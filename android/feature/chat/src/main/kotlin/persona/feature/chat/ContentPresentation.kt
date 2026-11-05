package persona.feature.chat

import persona.core.contracts.Item
import persona.core.domain.lines.ChatLine
import persona.core.domain.MessageContext
import persona.core.domain.decodeXmlEntities
import persona.core.domain.lines.WorkItem
import persona.core.domain.messageText
import persona.core.domain.parseContexts
import java.time.Instant
import kotlinx.serialization.json.*

/** Presentation options can be supplied by either chat host without changing wire items. */
data class ChatRenderStyle(
    val collapseThoughts: Boolean = false,
    /** The work line stays one sentence (Persona's own chat): no live steps under it, the plan as a plain step. */
    val compactToolSummary: Boolean = false,
    val hideThoughts: Boolean = false,
) {
    companion object {
        val Agent = ChatRenderStyle()
        val Assistant = ChatRenderStyle(collapseThoughts = true, compactToolSummary = true, hideThoughts = true)
    }
}

/** The event owner supplies complete text snapshots under the same item ID. */
data class ChatTextUpdate(val text: String, val streaming: Boolean = false)

internal sealed interface MessagePart {
    data class Text(val text: String) : MessagePart
    data class Source(val name: String, val text: String) : MessagePart
}

private val messageTag = Regex("<cross-session-message\\b([^>]*)>([\\s\\S]*?)</cross-session-message>")
private val attribute = Regex("([\\w-]+)\\s*=\\s*(?:\"([^\"]*)\"|'([^']*)')")

/**
 * A message as a row draws it: its [parts] in order, the photos it carries (`<attached_images>` and the older notes,
 * thumbnails above the text; never 「[图片]」, R3), the file paths in its text (chips under it, at most six), and its
 * `<jarvis-context>` blocks ([contexts]), which are not what the user wrote: one folded row beside the bubble (#876).
 */
internal data class MessageModel(
    val parts: List<MessagePart>, val images: List<String>, val files: List<String>,
    val contexts: List<MessageContext> = emptyList(),
) {
    /** What it says without its contexts: what the long-press menu copies, shares and quotes. */
    val plain: String get() = parts.mapNotNull {
        when (it) {
            is MessagePart.Text -> it.text
            is MessagePart.Source -> it.text.takeIf(String::isNotBlank)
        }
    }.joinToString("\n\n")
}

internal fun messageModel(raw: String, from: String? = null): MessageModel {
    val (contexts, text) = parseContexts(raw)
    val images = mutableListOf<String>()
    val files = mutableListOf<String>()
    val parts = buildList {
        var offset = 0
        fun plain(value: String) {
            val shown = messageText(value)
            images += shown.images
            files += shown.files
            shown.text.takeIf { it.isNotBlank() }?.let { add(MessagePart.Text(it)) }
        }
        messageTag.findAll(text).forEach { match ->
            plain(text.substring(offset, match.range.first))
            val attributes = attribute.findAll(match.groupValues[1]).associate {
                it.groupValues[1] to decodeXmlEntities(it.groupValues[2].ifEmpty { it.groupValues[3] })
            }
            add(MessagePart.Source(attributes["from-name"].orEmpty().ifBlank { "其他会话" }, decodeXmlEntities(match.groupValues[2]).trim()))
            offset = match.range.last + 1
        }
        plain(text.substring(offset))
    }.let { parts ->
        // A message the hub knows came from another session: all of it is that session's words, one 「来自」 card.
        if (!from.isNullOrBlank() && parts.none { it is MessagePart.Source })
            listOf(MessagePart.Source(from, parts.joinToString("\n\n") { (it as MessagePart.Text).text })) else parts
    }
    val photos = images.distinct()
    return MessageModel(parts, photos, files.distinct().filterNot { it in photos }.take(6), contexts)
}

internal fun messageParts(text: String, from: String? = null): List<MessagePart> = messageModel(text, from).parts

/**
 * Another session's words as Markdown with each single line break kept (Markdown joins a paragraph's lines; two spaces
 * before the newline keep it a break), as in the message it was. Web `keepLineBreaks`, iOS `markdownSoftBreakMode`.
 */
internal fun keepLineBreaks(text: String): String = softBreak.replace(text, "$1  \n")

private val softBreak = Regex("([^\n])\n(?=[^\n])")

/** A reply's Markdown to parse ahead: the cache key and the text [MessageContent] draws under it. */
internal data class MarkdownText(val key: String, val text: String)

/**
 * The Markdown of a conversation's replies (at most [limit] of them), with the keys and the text the rows draw (a cut
 * text without the hub's mark): first the latest turn's, which an unread conversation opens on, then the earlier ones,
 * newest first.
 */
internal fun markdownPrewarm(items: List<Item>, limit: Int = 80): Pair<List<MarkdownText>, List<MarkdownText>> {
    val replies = items.filter { it.kind == "text" && it.role == "assistant" }.takeLast(limit)
    val lastUser = items.lastOrNull { it.role == "user" && it.kind == "text" }
    fun texts(item: Item) = messageParts(item.text.orEmpty().let { if (item.fullSize != null) cutText(it) else it }, item.from)
        .mapIndexedNotNull { index, part -> (part as? MessagePart.Text)?.let { MarkdownText("${item.id}/$index", it.text) } }
    val (latest, earlier) = replies.partition { lastUser == null || chatItemOrder.compare(it, lastUser) > 0 }
    return latest.flatMap(::texts) to earlier.asReversed().flatMap(::texts)
}

/**
 * What 编辑 puts back in the composer for a message: its words, and its photos as `<attached_images>` again, which the
 * composer turns back into (already uploaded) attachments.
 */
internal fun MessageModel.editText(): String =
    if (images.isEmpty()) plain else plain + "\n\n<attached_images>\n" + images.joinToString("\n") + "\n</attached_images>"

internal data class DiffLine(val text: String, val sign: Char, val old: Int?, val new: Int?)
internal data class ToolDiff(val lines: List<DiffLine>) {
    val added get() = lines.count { it.sign == '+' }
    val removed get() = lines.count { it.sign == '-' }
}

private fun JsonObject.string(key: String) = (get(key) as? JsonPrimitive)?.contentOrNull
private fun contentLines(text: String): List<String> =
    if (text.isEmpty()) emptyList() else text.removeSuffix("\n").split('\n')

/** The file an edit or a write changed (for the header above its diff). */
internal fun toolPath(item: Item): String? = (item.toolInput as? JsonObject)?.let { input ->
    (input.string("file_path") ?: input.string("notebook_path"))?.takeIf { it.isNotBlank() }
}

internal fun toolDiff(item: Item): ToolDiff? {
    val input = item.toolInput as? JsonObject ?: return null
    // Codex's file changes come as an Edit with the patch (docs/codex.md): no old / new strings.
    if (item.toolName == "Edit") input.string("diff")?.let { return patchDiff(it, input.string("change")) }
    val edits = when (item.toolName) {
        "Edit" -> listOf(input)
        "MultiEdit" -> (input["edits"] as? JsonArray)?.mapNotNull { it as? JsonObject }.orEmpty()
        "Write" -> listOf(buildJsonObject {
            put("old_string", "")
            put("new_string", input.string("content") ?: return null)
        })
        else -> return null
    }
    val lines = buildList {
        edits.forEach { edit ->
            val old = contentLines(edit.string("old_string") ?: return null)
            val new = contentLines(edit.string("new_string") ?: return null)
            val prefix = old.zip(new).takeWhile { it.first == it.second }.size
            var suffix = 0
            while (suffix < minOf(old.size, new.size) - prefix && old[old.lastIndex - suffix] == new[new.lastIndex - suffix]) suffix++
            // Edit inputs contain snippets, so no absolute file line numbers are inferred.
            old.take(prefix).forEach { add(DiffLine(it, ' ', null, null)) }
            old.subList(prefix, old.size - suffix).forEach { add(DiffLine(it, '-', null, null)) }
            new.subList(prefix, new.size - suffix).forEachIndexed { index, line ->
                add(DiffLine(line, '+', null, if (item.toolName == "Write") index + 1 else null))
            }
            old.takeLast(suffix).forEach { add(DiffLine(it, ' ', null, null)) }
        }
    }
    return ToolDiff(lines)
}

private val hunkHeader = Regex("^@@ -(\\d+)(?:,\\d+)? \\+(\\d+)(?:,\\d+)? @@.*")

/**
 * A Codex file change (R2): a unified diff, numbered with the old and new lines; a new or deleted file the hub sends as
 * its whole content (no hunks) is all added, or all removed when [change] is `delete`.
 */
internal fun patchDiff(patch: String, change: String?): ToolDiff {
    if (patch.lineSequence().any { it.startsWith("@@") }) return ToolDiff(unifiedDiffLines(patch))
    val lines = contentLines(patch)
    return if (change == "delete") ToolDiff(lines.mapIndexed { index, line -> DiffLine(line, '-', index + 1, null) })
    else ToolDiff(lines.mapIndexed { index, line -> DiffLine(line, '+', null, index + 1) })
}

/**
 * The lines of a unified diff with their old / new line numbers (the old App's `DiffText.parse`). Hunk headers and
 * notes (`\ No newline at end of file`, `Binary files …`) are kept as quiet lines, signed `@`; file headers are dropped.
 */
internal fun unifiedDiffLines(diff: String): List<DiffLine> = buildList {
    var old = 0
    var new = 0
    for (raw in diff.split('\n')) {
        val line = raw.removeSuffix("\r")
        when {
            line.startsWith("diff --git") || line.startsWith("index ") || line.startsWith("--- ") || line.startsWith("+++ ") -> Unit
            line.startsWith("@@") -> {
                hunkHeader.matchEntire(line)?.let {
                    old = it.groupValues[1].toInt()
                    new = it.groupValues[2].toInt()
                }
                add(DiffLine(line, '@', null, null))
            }
            line.startsWith("+") -> add(DiffLine(line.drop(1), '+', null, new++))
            line.startsWith("-") -> add(DiffLine(line.drop(1), '-', old++, null))
            line.startsWith(" ") -> add(DiffLine(line.drop(1), ' ', old++, new++))
            line.startsWith("\\") || line.startsWith("Binary") || line.startsWith("new file") || line.startsWith("deleted file") ||
                line.startsWith("similarity") || line.startsWith("rename ") || line.startsWith("old mode") || line.startsWith("new mode") ->
                add(DiffLine(line, '@', null, null))
        }
    }
}

internal data class TodoEntry(val content: String, val status: String, val activeForm: String?)

internal fun todoEntries(item: Item): List<TodoEntry>? {
    if (item.toolName != "TodoWrite") return null
    val todos = (item.toolInput as? JsonObject)?.get("todos") as? JsonArray ?: return null
    return todos.mapNotNull { value ->
        val todo = value as? JsonObject ?: return@mapNotNull null
        // An item with no content reads as its activeForm (web `planTodos`); one with neither is dropped.
        val content = todo.string("content")?.takeIf { it.isNotBlank() }
            ?: todo.string("activeForm")?.takeIf { it.isNotBlank() } ?: return@mapNotNull null
        TodoEntry(content, todo.string("status").orEmpty(), todo.string("activeForm"))
    }
}

/** How far a plan is (web `planProgress`): the step it is on (in progress, else the next pending, else the last), 1-based. */
internal data class PlanProgress(val done: Int, val total: Int, val current: String, val step: Int) {
    val allDone get() = total > 0 && done >= total
}

internal fun planProgress(todos: List<TodoEntry>): PlanProgress {
    val done = todos.count { it.status == "completed" }
    val active = todos.indexOfFirst { it.status == "in_progress" }
    val pending = todos.indexOfFirst { it.status != "completed" && it.status != "in_progress" }
    val at = if (active >= 0) active else if (pending >= 0) pending else maxOf(0, todos.lastIndex)
    val todo = todos.getOrNull(at)
    val current = (if (todo?.status == "in_progress") todo.activeForm?.takeIf { it.isNotBlank() } else null) ?: todo?.content.orEmpty()
    return PlanProgress(done, todos.size, current, if (todos.isNotEmpty() && done >= todos.size) todos.size else at + 1)
}

/**
 * The plan a work line draws (web `planSteps`): its latest TodoWrite of two or more items, with them. Every other
 * TodoWrite of the line (older updates, an empty list that clears it) is not drawn.
 */
internal data class LinePlan(val id: String, val todos: List<TodoEntry>, val failed: Boolean)

internal fun latestPlan(steps: List<WorkItem>): LinePlan? = steps.asReversed().firstNotNullOfOrNull { step ->
    (step as? WorkItem.Tool)?.call?.let { call ->
        todoEntries(call.use.item)?.takeIf { it.size >= 2 }?.let { LinePlan(call.id, it, call.failed) }
    }
}

/** What a running work line shows under its header (web #1990 `previewSteps`): the latest plan, and the newest steps. */
internal data class WorkPeek(val plan: LinePlan?, val steps: List<WorkItem>)

/**
 * The newest [limit] tool calls and thoughts of a running line, oldest first; the agent's words, notices, folds and
 * subagent calls (Task / Agent) are not steps here. Reading the same file again within one run of reads shows once. The plan is not one of the steps: the
 * latest TodoWrite of two or more items is pinned above them.
 */
internal fun workPeek(steps: List<WorkItem>, limit: Int = 5): WorkPeek {
    val plan = latestPlan(steps)
    val out = mutableListOf<WorkItem>()
    val seen = mutableSetOf<String>()
    for (step in steps.asReversed()) {
        if (out.size >= limit) break
        when (step) {
            is WorkItem.Thought -> if (step.item.kind == "thinking" && !step.item.text.isNullOrBlank()) out += step
            is WorkItem.Tool -> {
                val item = step.call.use.item
                if (item.toolName == "TodoWrite" || item.toolName == "Task" || item.toolName == "Agent") continue
                val read = if (item.toolName == "Read") toolPath(item) else null
                if (item.toolName != "Read") seen.clear()
                else if (read != null && !seen.add(read)) continue
                out += step
            }
            else -> Unit
        }
    }
    return WorkPeek(plan, out.asReversed())
}

/** A thought as one line in a running line's peek (web ThinkingLine): its words with the line breaks folded. */
internal fun thoughtLine(text: String): String = text.trim().replace(Regex("\\s+"), " ").ifEmpty { "思考" }

internal data class ToolPresentation(val title: String, val verb: String, val detail: String? = null, val mono: Boolean = false)

internal fun toolPresentation(item: Item): ToolPresentation {
    val input = item.toolInput as? JsonObject
    val name = item.toolName.orEmpty()
    val verb = when (name) {
        "Read", "LS" -> "读取"
        "Edit", "MultiEdit", "NotebookEdit" -> "编辑"
        "Write" -> "新建"
        "Bash" -> "运行"
        "Grep", "Glob", "ToolSearch", "WebSearch" -> "搜索"
        "WebFetch" -> "读取"
        "TodoWrite", "TaskCreate", "TaskUpdate", "TaskList" -> "更新"
        "Task", "Agent" -> "委派"
        "EnterPlanMode", "ExitPlanMode" -> "规划"
        "AskUserQuestion" -> "询问"
        else -> "执行"
    }
    val path = input?.string("file_path") ?: input?.string("notebook_path")
    if (!path.isNullOrBlank()) {
        val normalized = path.replace('\\', '/')
        return ToolPresentation(normalized.substringAfterLast('/'), verb, normalized.substringBeforeLast('/', "").ifBlank { null })
    }
    if (name == "Bash") {
        val command = input?.string("command")?.lineSequence()?.firstOrNull().orEmpty()
        return ToolPresentation(command.ifBlank { "命令" }, verb, input?.string("description"), mono = true)
    }
    val subject = persona.core.domain.toolSubject(item).trim()
    return ToolPresentation(subject.ifBlank { when (name) {
        "TodoWrite" -> "任务清单"
        "Task", "Agent" -> "子 Agent"
        else -> verb
    } }, verb, mono = name == "Bash")
}

/**
 * The steps a fold stands for, counted the way its work line counts them once open (and the 收起 line and iOS do): each
 * tool call and each thought. `fold.items` also counts every tool result, so a fold of three calls read 「做了 6 步」.
 */
internal fun WorkItem.Folded.stepCount(): Int = item.fold?.let { it.tools + it.thoughts } ?: 1

/**
 * The words on a work line. A running one says what it is doing and, given [now], how long it has run (R1, ticking
 * every second like iOS's 「正在工作 39 秒」). With [peek] its latest steps show under it, so it only says
 * 「正在工作…」; the time is a leaf of its own beside it (web #1990).
 */
internal fun workSummary(line: ChatLine.Work, recordedDuration: Long?, now: Instant? = null, peek: Boolean = false): String {
    val count = line.steps.sumOf { if (it is WorkItem.Folded) it.stepCount() else 1 }
    if (line.live && peek) return "正在工作…"
    if (line.live) {
        val current = line.steps.lastOrNull()
        val action = when (current) {
            is WorkItem.Tool -> if (current.call.running)
                toolPresentation(current.call.use.item).let { "正在${it.verb} ${it.title}" } else "正在工作"
            is WorkItem.Thought -> if (current.item.kind == "thinking") "正在思考" else "正在工作"
            else -> "正在工作"
        }
        val running = if (count > 0) "$action，第 $count 步" else action
        val elapsed = now?.let { line.durationText(it) }.orEmpty()
        return if (elapsed.isEmpty()) running else "$running，$elapsed"
    }
    val seconds = recordedDuration?.takeIf { it >= 0 }
    val duration = seconds?.let { when {
        it < 60 -> "$it 秒"
        it < 3600 -> "${it / 60} 分钟"
        else -> "${it / 3600} 小时 ${it % 3600 / 60} 分钟"
    } }
    return "做了 $count 步" + (duration?.let { "，用时 $it" } ?: "") +
        (if (line.knownFailedTools > 0) "，${line.knownFailedTools} 项失败" else "") +
        (if (line.unresolvedTools > 0) "，${line.unresolvedTools} 项无结果记录" else "")
}
