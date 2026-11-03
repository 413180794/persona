package persona.feature.chat

import persona.core.contracts.Item
import persona.core.domain.cleanText
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.WorkItem
import kotlinx.serialization.json.*

/** Presentation options can be supplied by either chat host without changing wire items. */
data class ChatRenderStyle(
    val collapseThoughts: Boolean = false,
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
    data class Reference(val title: String, val kind: String, val url: String?, val quote: String) : MessagePart
    data class Source(val name: String, val text: String) : MessagePart
}

private val messageTag = Regex("<(jarvis-context|cross-session-message)\\b([^>]*)>([\\s\\S]*?)</\\1>")
private val attribute = Regex("([\\w-]+)\\s*=\\s*(?:\"([^\"]*)\"|'([^']*)')")

internal fun messageParts(text: String, from: String? = null): List<MessagePart> = buildList {
    var offset = 0
    fun plain(value: String) {
        cleanText(value).takeIf { it.isNotBlank() }?.let { add(MessagePart.Text(it)) }
    }
    messageTag.findAll(text).forEach { match ->
        plain(text.substring(offset, match.range.first))
        val attributes = attribute.findAll(match.groupValues[2]).associate {
            it.groupValues[1] to decodeEntities(it.groupValues[2].ifEmpty { it.groupValues[3] })
        }
        val body = decodeEntities(match.groupValues[3]).trim()
        if (match.groupValues[1] == "jarvis-context") {
            add(MessagePart.Reference(attributes["title"].orEmpty().ifBlank { "引用内容" },
                attributes["kind"].orEmpty(), attributes["url"]?.takeIf { it.isNotBlank() }, body))
        } else add(MessagePart.Source(attributes["from-name"].orEmpty().ifBlank { "其他会话" }, body))
        offset = match.range.last + 1
    }
    plain(text.substring(offset))
}.let { parts ->
    if (!from.isNullOrBlank() && parts.none { it is MessagePart.Source })
        listOf(MessagePart.Source(from, "")) + parts else parts
}

private fun decodeEntities(value: String): String = value
    .replace("&quot;", "\"").replace("&apos;", "'")
    .replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")

internal data class DiffLine(val text: String, val sign: Char, val old: Int?, val new: Int?)
internal data class ToolDiff(val lines: List<DiffLine>) {
    val added get() = lines.count { it.sign == '+' }
    val removed get() = lines.count { it.sign == '-' }
}

private fun JsonObject.string(key: String) = (get(key) as? JsonPrimitive)?.contentOrNull
private fun contentLines(text: String): List<String> =
    if (text.isEmpty()) emptyList() else text.removeSuffix("\n").split('\n')

internal fun toolDiff(item: Item): ToolDiff? {
    val input = item.toolInput as? JsonObject ?: return null
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

internal data class TodoEntry(val content: String, val status: String, val activeForm: String?)

internal fun todoEntries(item: Item): List<TodoEntry>? {
    if (item.toolName != "TodoWrite") return null
    val todos = (item.toolInput as? JsonObject)?.get("todos") as? JsonArray ?: return null
    return todos.mapNotNull { value ->
        val todo = value as? JsonObject ?: return@mapNotNull null
        val content = todo.string("content")?.takeIf { it.isNotBlank() } ?: return@mapNotNull null
        TodoEntry(content, todo.string("status").orEmpty(), todo.string("activeForm"))
    }
}

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

internal fun workSummary(line: ChatLine.Work, recordedDuration: Long?): String {
    val count = line.steps.sumOf { if (it is WorkItem.Folded) it.item.fold?.items ?: 1 else 1 }
    if (line.live) {
        val current = line.steps.lastOrNull()
        val action = when (current) {
            is WorkItem.Tool -> if (current.call.running)
                toolPresentation(current.call.use.item).let { "正在${it.verb} ${it.title}" } else "正在工作"
            is WorkItem.Thought -> if (current.item.kind == "thinking") "正在思考" else "正在工作"
            else -> "正在工作"
        }
        return if (count > 0) "$action，第 $count 步" else action
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
