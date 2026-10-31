package jarvis.android.ui

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.HelpOutline
import androidx.compose.material.icons.automirrored.filled.ListAlt
import androidx.compose.material.icons.automirrored.filled.NoteAdd
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Build
import androidx.compose.material.icons.filled.Checklist
import androidx.compose.material.icons.filled.Description
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Extension
import androidx.compose.material.icons.filled.FolderOpen
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.Language
import androidx.compose.material.icons.filled.Mail
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Smartphone
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.ui.graphics.vector.ImageVector
import jarvis.android.data.Item
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import java.time.Instant

// The chat's row model (iOS `ChatStore.buildRows` + `chatLines`): tool calls joined with their results, runs of
// calls with the narration between them, and each turn's work folded behind one line under the user's message.

/** A tool call with its result and screenshots, joined by tool_use_id. */
data class ToolCall(val use: Item, val result: Item?, val images: List<Item>) {
    val name: String get() = use.toolName.orEmpty()
    val isError: Boolean get() = result?.isError == true
    val isRunning: Boolean get() = result == null
}

/** One step inside a run of tool calls: a call, or narration (thinking / notice) between calls. */
sealed interface ToolStep {
    val id: String

    data class Call(val call: ToolCall) : ToolStep { override val id get() = call.use.id }
    data class Thought(val item: Item) : ToolStep { override val id get() = item.id }
}

/** What the chat list draws, oldest first. Keys are stable across live updates. */
sealed interface ChatRow {
    val key: String

    data class User(val item: Item) : ChatRow { override val key get() = "u:${item.id}" }
    data class Assistant(val item: Item) : ChatRow { override val key get() = "a:${item.id}" }
    data class Thinking(val item: Item) : ChatRow { override val key get() = "k:${item.id}" }
    data class Tools(val steps: List<ToolStep>) : ChatRow { override val key get() = "t:${steps.first().id}" }
    data class Notice(val item: Item) : ChatRow { override val key get() = "n:${item.id}" }
    /** Claude Code's "while you were away" recap. */
    data class Recap(val item: Item) : ChatRow { override val key get() = "r:${item.id}" }
    data class Image(val item: Item) : ChatRow { override val key get() = "i:${item.id}" }
}

/** Joins tool results, folds consecutive calls into runs, keeps narration in between (iOS `ChatStore.buildRows`). */
fun buildRows(items: List<Item>): List<ChatRow> {
    val results = HashMap<String, Item>()
    val images = HashMap<String, MutableList<Item>>()
    for (it in items) {
        val tid = it.toolUseId ?: continue
        if (it.kind == "tool_result") results[tid] = it
        if (it.kind == "image" && it.image != null) images.getOrPut(tid) { ArrayList() }.add(it)
    }
    val rows = ArrayList<ChatRow>()
    val run = ArrayList<ToolStep>()
    val thoughts = ArrayList<Item>()

    fun flushThoughts(asRows: Boolean) {
        if (asRows) thoughts.forEach { rows += ChatRow.Thinking(it) } else thoughts.forEach { run += ToolStep.Thought(it) }
        thoughts.clear()
    }
    fun flushRun() {
        if (run.isNotEmpty()) { rows += ChatRow.Tools(run.toList()); run.clear() }
    }
    fun closeRun() = if (run.isEmpty()) flushThoughts(true) else { flushThoughts(false); flushRun() }

    for (it in items) {
        when (it.kind) {
            "tool_use" -> {
                flushThoughts(asRows = run.isEmpty())
                val tid = it.toolUseId
                run += ToolStep.Call(ToolCall(it, tid?.let { t -> results[t] }, tid?.let { t -> images[t] }.orEmpty()))
            }
            "thinking" -> if (!it.text.isNullOrBlank()) thoughts += it
            "tool_result" -> Unit
            "image" -> if (it.toolUseId == null) { closeRun(); rows += ChatRow.Image(it) }
            "text", "notice", "recap" -> {
                if (it.text.isNullOrBlank()) continue
                if (it.kind == "text" && it.role == "user" && userText(it).let { u -> u.text.isEmpty() && u.refs.isEmpty() && u.images.isEmpty() }) continue
                closeRun()
                rows += when (it.kind) {
                    "text" -> if (it.role == "user") ChatRow.User(it) else ChatRow.Assistant(it)
                    "notice" -> ChatRow.Notice(it)
                    else -> ChatRow.Recap(it)
                }
            }
        }
    }
    closeRun()
    return rows
}

// --- Tool presentation (iOS `ToolPresentation`) -------------------------------------------------------------------------

object ToolText {
    fun baseName(name: String): String = if (name.startsWith("mcp__")) name.substringAfterLast("__") else name

    fun icon(name: String): ImageVector {
        val n = baseName(name)
        if (n.startsWith("phone_")) return Icons.Filled.Smartphone
        if (n.startsWith("mail_")) return Icons.Filled.Mail
        return when (n) {
            "Bash", "BashOutput", "KillShell", "KillBash" -> Icons.Filled.Terminal
            "Read" -> Icons.Filled.Description
            "Edit", "MultiEdit", "NotebookEdit" -> Icons.Filled.Edit
            "Write" -> Icons.AutoMirrored.Filled.NoteAdd
            "Grep", "WebSearch", "ToolSearch" -> Icons.Filled.Search
            "Glob", "LS" -> Icons.Filled.FolderOpen
            "WebFetch" -> Icons.Filled.Language
            "Task", "Agent" -> Icons.Filled.Groups
            "TodoWrite", "TaskCreate", "TaskUpdate" -> Icons.Filled.Checklist
            "AskUserQuestion" -> Icons.AutoMirrored.Filled.HelpOutline
            "ExitPlanMode", "EnterPlanMode" -> Icons.AutoMirrored.Filled.ListAlt
            "Skill" -> Icons.Filled.AutoAwesome
            else -> if (name.startsWith("mcp__")) Icons.Filled.Extension else Icons.Filled.Build
        }
    }

    fun verb(name: String?): String {
        val n = baseName(name ?: return "工具")
        if (n.startsWith("phone_")) return "手机"
        if (n.startsWith("mail_")) return "邮件"
        return when (n) {
            "Bash" -> "运行"
            "Read" -> "读取"
            "Edit", "MultiEdit" -> "编辑"
            "Write" -> "写入"
            "Grep" -> "搜索"
            "Glob", "LS" -> "查找"
            "WebFetch" -> "打开网页"
            "WebSearch" -> "网页搜索"
            "Task", "Agent" -> "子任务"
            "TodoWrite" -> "更新待办"
            "AskUserQuestion" -> "提问"
            "ExitPlanMode" -> "提交计划"
            "EnterPlanMode" -> "进入计划模式"
            "ToolSearch" -> "加载工具"
            "Skill" -> "使用技能"
            else -> n
        }
    }

    /** One line of what the call is about: the command, the file, the pattern… */
    fun subject(use: Item): String {
        fun s(k: String) = use.inputString(k)?.takeIf { it.isNotEmpty() }
        val input = use.toolInput as? JsonObject
        val n = baseName(use.toolName.orEmpty())
        return when (n) {
            "Bash" -> s("description") ?: firstLine(s("command").orEmpty())
            "Read", "Edit", "MultiEdit", "Write", "NotebookEdit" -> (s("file_path") ?: s("notebook_path")).orEmpty().substringAfterLast('/')
            "Grep", "Glob" -> s("pattern").orEmpty()
            "WebFetch" -> s("url")?.let { runCatching { java.net.URI(it).host }.getOrNull() ?: it }.orEmpty()
            "WebSearch", "ToolSearch" -> s("query").orEmpty()
            "Task", "Agent" -> s("description").orEmpty()
            "Skill" -> s("skill") ?: s("command").orEmpty()
            "AskUserQuestion" -> (((input?.get("questions") as? JsonArray)?.firstOrNull() as? JsonObject)?.get("question") as? JsonPrimitive)
                ?.contentOrNull.orEmpty()
            "ExitPlanMode" -> s("plan")?.lineSequence()?.map { it.trim().trimStart('#').trim() }?.firstOrNull { it.isNotEmpty() }.orEmpty()
            "TodoWrite" -> {
                val todos = input?.get("todos") as? JsonArray
                if (todos == null) "" else {
                    val done = todos.count { ((it as? JsonObject)?.get("status") as? JsonPrimitive)?.contentOrNull == "completed" }
                    "$done/${todos.size}"
                }
            }
            else -> if (n.startsWith("phone_") || n.startsWith("mail_")) n.split('_').drop(1).joinToString(" ")
            else input?.values?.firstNotNullOfOrNull { (it as? JsonPrimitive)?.takeIf { p -> p.isString }?.content }?.let { firstLine(it) }.orEmpty()
        }
    }

    /** Summary of a run, like 「运行 3 · 读取 2」. */
    fun runSummary(calls: List<ToolCall>): String {
        val counts = LinkedHashMap<String, Int>()
        calls.forEach { counts[verb(it.name)] = (counts[verb(it.name)] ?: 0) + 1 }
        return counts.entries.take(3).joinToString(" · ") { "${it.key} ${it.value}" }
    }

    fun firstLine(s: String): String = s.lineSequence().firstOrNull { it.isNotBlank() }?.trim() ?: s
}

/** Kept for callers of the old row model. */
fun toolVerb(name: String?): String = ToolText.verb(name)
fun toolSubject(it: Item): String = ToolText.subject(it)

/** Indented JSON for display. */
fun pretty(e: JsonElement, indent: String = ""): String = when (e) {
    is JsonObject -> if (e.isEmpty()) "{}" else e.entries.joinToString(",\n", "{\n", "\n$indent}") { (k, v) ->
        "$indent  \"$k\": ${pretty(v, "$indent  ")}"
    }
    is JsonArray -> if (e.isEmpty()) "[]" else e.joinToString(",\n", "[\n", "\n$indent]") { "$indent  ${pretty(it, "$indent  ")}" }
    is JsonPrimitive -> if (e.isString) {
        val c = e.content
        if (c.length > 2000) "\"${c.take(2000)}…\"" else "\"$c\""
    } else e.content
}

/**
 * An excerpt that rides along with a message (CLAUDE.md「链接在 App 内打开」, the long-press 「作为上下文」): [kind]
 * "context" is reference material, "edit" asks the agent to change it. Sent as a `<jarvis-context>` tag that the
 * clients parse back into a card above the bubble.
 */
typealias ContextRef = jarvis.android.ui.web.ContextRef // one type for the chat's menu and the link sheet's picks

// --- User message tags (docs/protocol.md「消息里的标签」, iOS `UserRowText`) ----------------------------------------------

/** A user message split into what its row shows. */
data class UserText(
    /** What was typed, without context tags, image notes and Claude's `[Image #N]` placeholders. */
    val text: String,
    /** `<jarvis-context>` excerpts it carries (cards above the bubble). */
    val refs: List<ContextRef> = emptyList(),
    /** Photo paths from `<attached_images>`: thumbnails, read from the session's machine. */
    val images: List<String> = emptyList(),
    /** File-looking paths in the text (chips under the bubble). */
    val files: List<String> = emptyList(),
    /** `<cross-session-message from-name>`: another Claude session sent it. */
    val from: String? = null,
)

private val crossRe = Regex("<cross-session-message([^>]*)>([\\s\\S]*?)</cross-session-message>")
private val pastedRe = Regex("</?pasted_content[^>]*>")
private val imagePlaceholderRe = Regex("\\[Image #\\d+]\\s*")
private val contextRe = Regex(
    "<jarvis-context\\s+kind=\"([^\"]*)\"\\s+title=\"([^\"]*)\"(?:\\s+url=\"([^\"]*)\")?\\s*>\\s*([\\s\\S]*?)\\s*</jarvis-context>\\s*",
)

/** `<attached_images>` and the plain-text notes older builds appended (iOS `UserBubble.notePatterns`). */
private val notePatterns = listOf(
    Regex("\\s*<attached_images>[\\s\\S]*?</attached_images>\\s*"),
    Regex("(?:\\A|\\n)\\s*相关图片：[^\\n]*\\z"),
    Regex("\\s*（我从手机发来了图片，请用 Read 查看：[^）]*）\\s*\\z"),
)

private fun attr(attrs: String, name: String): String? = Regex("$name=\"([^\"]*)\"").find(attrs)?.groupValues?.get(1)
private fun unesc(s: String) = s.replace("&quot;", "\"").replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")

/** The `<jarvis-context>` tags in a message and the message without them. */
fun parseContexts(text: String): Pair<List<ContextRef>, String> {
    val refs = contextRe.findAll(text).map { m ->
        ContextRef(unesc(m.groupValues[1]), unesc(m.groupValues[2]), unesc(m.groupValues[3]), unesc(m.groupValues[4]))
    }.toList()
    return refs to contextRe.replace(text, "").trim()
}

/** A message carrying [refs] before [text] (what the composer sends). */
fun composeWithContexts(text: String, refs: List<ContextRef>): String =
    if (refs.isEmpty()) text else refs.joinToString("\n") { it.xml } + "\n\n" + text

private val userMemo = java.util.Collections.synchronizedMap(object : LinkedHashMap<String, UserText>(256, 0.75f, true) {
    override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, UserText>?) = size > 4000
})

/** [parseUserText] memoized by item id (rows redraw constantly; an id never changes its text). */
fun userText(item: Item): UserText = userMemo[item.id] ?: parseUserText(item.cutText).also { userMemo[item.id] = it }

fun parseUserText(raw: String): UserText {
    var s = raw
    var from: String? = null
    crossRe.find(s)?.let { m ->
        from = attr(m.groupValues[1], "from-name") ?: attr(m.groupValues[1], "from")
        s = s.replace(m.value, m.groupValues[2])
    }
    s = pastedRe.replace(s, "")
    val (refs, rest) = parseContexts(s)
    val images = imagePaths(rest)
    var shown = rest
    for (p in notePatterns) shown = p.replace(shown, "\n")
    shown = imagePlaceholderRe.replace(shown, "").trim()
    val files = FilePaths.find(rest).filter { it !in images }
    return UserText(shown, refs, images, files, from)
}

/** Strips what the hub / Claude Code wrap around typed text (one line of text, for previews). */
fun cleanText(s: String): String {
    val u = parseUserText(s)
    return if (u.images.isNotEmpty()) ("[图片]\n" + u.text).trim() else u.text
}

/** The photo paths listed in an image note. */
fun imagePaths(text: String): List<String> {
    for (p in notePatterns) {
        val m = p.find(text) ?: continue
        var note = m.value
        for (token in listOf("<attached_images>", "</attached_images>", "相关图片：", "（我从手机发来了图片，请用 Read 查看：", "）")) {
            note = note.replace(token, "\n")
        }
        return note.split('\n', '、').map { it.trim() }.filter { it.startsWith("/") || it.startsWith("~") }
    }
    return emptyList()
}

/** File-looking paths in a message (iOS `FilePaths`): ~/x, /x/y, or relative with a folder and an extension. */
object FilePaths {
    private val re = Regex(
        "(?<![\\w/:.\\-~])(~(?:/[\\w.@+\\-]+)+/?|/[\\w.@+\\-]+(?:/[\\w.@+\\-]+)+/?|[\\w.@\\-]+(?:/[\\w.@+\\-]+)+\\.[A-Za-z][A-Za-z0-9]{0,7})(?![\\w/\\-])",
    )
    private val memo = java.util.Collections.synchronizedMap(object : LinkedHashMap<String, List<String>>(256, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, List<String>>?) = size > 2000
    })

    /** Paths in [text], in order, without duplicates or URL fragments (memoized). */
    fun find(text: String): List<String> = memo[text] ?: scan(text, 6).also { memo[text] = it }

    private fun scan(text: String, limit: Int): List<String> {
        val out = ArrayList<String>()
        for (m in re.findAll(text)) {
            var p = m.groupValues[1]
            while (p.isNotEmpty() && p.last() in ".,;:)") p = p.dropLast(1)
            val start = m.range.first
            val before = if (start >= 3) text.substring(start - 3, start) else ""
            if (before.endsWith("://") || p.startsWith("//") || p.length < 4 || p in out) continue
            if (p.startsWith("/") && p.split('/').count { it.isNotEmpty() } < 2) continue
            out += p
            if (out.size >= limit) break
        }
        return out
    }
}

/** A background task's report (`wake` notice): `Background command "Release" completed (exit code 0)` → 「后台命令完成 · Release」. */
fun wakeSummary(s: String): Pair<String, Boolean> {
    val m = Regex("^(.*?)\\s*\"(.+)\"\\s*(.*)$", RegexOption.DOT_MATCHES_ALL).find(s) ?: return s to false
    val kind = when (m.groupValues[1]) {
        "Background command" -> "后台命令"
        "Agent" -> "子 Agent "
        "Monitor" -> "监控"
        else -> "后台任务"
    }
    val rest = m.groupValues[3].lowercase()
    val code = Regex("exit code (\\d+)").find(rest)?.groupValues?.get(1)?.toIntOrNull()
    val failed = "fail" in rest || "error" in rest || (code ?: 0) != 0
    val state = when {
        failed -> "失败"
        "kill" in rest || "stop" in rest -> "已停止"
        "complet" in rest -> "完成"
        else -> "有更新"
    }
    return "$kind$state · ${m.groupValues[2]}" to failed
}

// --- Work lines (iOS `chatLines` + `WorkLine` + `WorkItem`, docs/ios.md「展示逻辑」) --------------------------------------

/** One step of an opened work line, a line of the chat list of its own (a turn can have hundreds). */
sealed interface WorkItem {
    val id: String

    data class Call(val call: ToolCall) : WorkItem { override val id get() = call.use.id }
    /** Thinking, or a system notice (a background task done…). */
    data class Thought(val item: Item) : WorkItem { override val id get() = item.id }
    /** Something the agent said before its last words. */
    data class Text(val item: Item) : WorkItem { override val id get() = "t-${item.id}" }
    /** A picture the agent showed along the way. */
    data class Image(val item: Item) : WorkItem { override val id get() = "i-${item.id}" }
}

private fun ChatRow.workItems(): List<WorkItem> = when (this) {
    is ChatRow.Tools -> steps.map { s -> if (s is ToolStep.Call) WorkItem.Call(s.call) else WorkItem.Thought((s as ToolStep.Thought).item) }
    is ChatRow.Thinking -> listOf(WorkItem.Thought(item))
    is ChatRow.Notice -> listOf(WorkItem.Thought(item))
    is ChatRow.Assistant -> listOf(WorkItem.Text(item))
    is ChatRow.Image -> listOf(WorkItem.Image(item))
    is ChatRow.User, is ChatRow.Recap -> emptyList()
}

/** What one segment's work line counts (iOS `WorkSummary`); wake notices count as tools. */
data class WorkStats(
    val tools: Int,
    val thoughts: Int,
    val replies: Int,
    val start: Instant?,
    val end: Instant?,
    val notices: Int = 0,
) {
    companion object {
        fun of(items: List<WorkItem>, start: Instant?, end: Instant?): WorkStats {
            var calls = 0
            var thoughts = 0
            var notices = 0
            var texts = 0
            for (w in items) when (w) {
                is WorkItem.Call -> calls++
                is WorkItem.Thought -> if (w.item.kind == "notice") { if (w.item.wake == true) calls++ else notices++ } else thoughts++
                is WorkItem.Text -> texts++
                is WorkItem.Image -> Unit
            }
            return WorkStats(calls, thoughts, texts, start, end, notices)
        }
    }
}

/** A chat row as drawn, after folding each segment's work behind one line. */
sealed interface FoldedRow {
    val key: String

    data class Plain(val row: ChatRow) : FoldedRow { override val key get() = row.key }
    /** The fold: [items] are hidden unless [expanded]; [running] while the agent still works on this segment. */
    data class Work(val id: String, val items: List<WorkItem>, val stats: WorkStats, val running: Boolean, val expanded: Boolean) : FoldedRow {
        override val key get() = "w:$id"
    }
    /** An opened work line's step; [first] draws the card's rounded top. */
    data class Step(val workId: String, val item: WorkItem, val first: Boolean) : FoldedRow {
        override val key get() = "ws:$workId/${item.id}"
    }
    data class StepsEnd(val workId: String, val count: Int) : FoldedRow { override val key get() = "we:$workId" }
}

private fun ChatRow.times(): List<Instant> = when (this) {
    is ChatRow.User -> listOfNotNull(item.time)
    is ChatRow.Assistant -> listOfNotNull(item.time)
    is ChatRow.Thinking -> listOfNotNull(item.time)
    is ChatRow.Notice -> listOfNotNull(item.time)
    is ChatRow.Recap -> listOfNotNull(item.time)
    is ChatRow.Image -> listOfNotNull(item.time)
    is ChatRow.Tools -> steps.flatMap {
        when (it) {
            is ToolStep.Call -> listOfNotNull(it.call.use.time, it.call.result?.time)
            is ToolStep.Thought -> listOfNotNull(it.item.time)
        }
    }
}

/** Rows that stay visible inside a segment: the user's own pictures and away recaps. */
private fun ChatRow.staysOut(): Boolean = this is ChatRow.Recap || (this is ChatRow.Image && item.role == "user")

/**
 * Cuts [rows] into segments at each user message: message → one work line (everything but the last thing the agent
 * said before the next message) → that last reply. While [working], the newest segment's line runs, and a reply
 * followed by more work is still work. An [expanded] line lists its steps right under it, one list line each.
 */
fun foldWork(rows: List<ChatRow>, working: Boolean, expanded: Set<String>): List<FoldedRow> {
    val out = ArrayList<FoldedRow>(rows.size)
    var i = 0
    while (i < rows.size) {
        if (rows[i] is ChatRow.User) { out += FoldedRow.Plain(rows[i]); i++; continue }
        var j = i
        while (j < rows.size && rows[j] !is ChatRow.User) j++
        val body = rows.subList(i, j)
        val running = j == rows.size && working
        var answerAt = body.indexOfLast { it is ChatRow.Assistant }
        if (running && answerAt >= 0 && body.drop(answerAt + 1).any { !it.staysOut() }) answerAt = -1
        val steps = body.filterIndexed { k, r -> k != answerAt && !r.staysOut() }
        body.filter { it.staysOut() }.forEach { out += FoldedRow.Plain(it) }
        if (steps.isNotEmpty()) {
            val id = steps.first().key
            val prev = rows.getOrNull(i - 1) as? ChatRow.User
            val times = body.flatMap { it.times() } + listOfNotNull(prev?.item?.time)
            val items = steps.flatMap { it.workItems() }
            val open = id in expanded
            out += FoldedRow.Work(id, items, WorkStats.of(items, times.minOrNull(), times.maxOrNull()), running, open)
            if (open && items.isNotEmpty()) {
                items.forEachIndexed { k, w -> out += FoldedRow.Step(id, w, first = k == 0) }
                out += FoldedRow.StepsEnd(id, items.size)
            }
        }
        if (answerAt >= 0) out += FoldedRow.Plain(body[answerAt])
        i = j
    }
    return out
}

/** 「39 秒」「4 分 17 秒」「5 分」「1 小时 5 分」 (iOS `WorkLine.duration`). */
fun workDuration(seconds: Long): String {
    val s = seconds.coerceAtLeast(0)
    return when {
        s < 60 -> "$s 秒"
        s < 3600 -> if (s % 60 == 0L) "${s / 60} 分" else "${s / 60} 分 ${s % 60} 秒"
        else -> "${s / 3600} 小时 ${s % 3600 / 60} 分"
    }
}

/** The words on a work line; [now] for the running one's timer; [short] keeps only the tools and the time. */
fun workLabel(stats: WorkStats, running: Boolean, now: Long, short: Boolean = false): String {
    val parts = ArrayList<String>()
    if (stats.tools > 0) parts += "工具 ${stats.tools}"
    if (!short) {
        if (stats.thoughts > 0) parts += "思考 ${stats.thoughts}"
        if (stats.replies > 0) parts += "回复 ${stats.replies}"
    }
    if (parts.isEmpty() && stats.notices > 0) parts += "后台通知 ${stats.notices}"
    val start = stats.start
    if (running) {
        val took = start?.let { workDuration((now - it.toEpochMilli()) / 1000) }.orEmpty()
        parts.add(0, "正在工作 $took".trim())
    } else if (start != null && stats.end != null) {
        val secs = java.time.Duration.between(start, stats.end).seconds
        if (secs > 0) parts += workDuration(secs)
    }
    return if (parts.isEmpty()) "已完成" else parts.joinToString(" · ")
}
