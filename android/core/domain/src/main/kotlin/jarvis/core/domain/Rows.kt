package jarvis.core.domain

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import jarvis.core.contracts.Item

/** What the chat shows: user / assistant text, a tool call folded with its result, or a notice. */
sealed interface Row {
    val id: String
    data class User(override val id: String, val text: String, val queued: Boolean, val from: String?) : Row
    data class Assistant(override val id: String, val text: String) : Row
    data class Thinking(override val id: String, val text: String) : Row
    data class Tool(override val id: String, val use: Item, val result: Item?) : Row
    data class Image(override val id: String, val item: Item) : Row
    data class Notice(override val id: String, val text: String) : Row
}

/** Tool uses and results share tool_use_id: merge them into one row (as the iOS chat does). */
fun buildRows(items: List<Item>): List<Row> {
    val results = items.filter { it.kind == "tool_result" && it.toolUseId != null }.associateBy { it.toolUseId!! }
    val rows = mutableListOf<Row>()
    for (it in items) {
        when (it.kind) {
            "text" -> {
                val t = cleanText(it.text.orEmpty())
                if (t.isBlank()) continue
                rows += if (it.role == "user") Row.User(it.id, t, it.queued == true, it.from) else Row.Assistant(it.id, t)
            }
            "thinking" -> if (!it.text.isNullOrBlank()) rows += Row.Thinking(it.id, it.text.orEmpty())
            "tool_use" -> rows += Row.Tool(it.id, it, it.toolUseId?.let { id -> results[id] })
            "tool_result" -> Unit // shown with its tool_use
            "image" -> if (it.toolUseId == null) rows += Row.Image(it.id, it)
            "notice" -> if (!it.text.isNullOrBlank()) rows += Row.Notice(it.id, it.text.orEmpty())
        }
    }
    return rows
}

/** Strips what the hub / Claude Code wrap around typed text (docs/protocol.md「消息里的标签」). */
fun cleanText(s: String): String = s
    .replace(Regex("<attached_images>[\\s\\S]*?</attached_images>"), "[图片]")
    .replace(Regex("</?pasted_content[^>]*>"), "")
    .replace(Regex("<jarvis-context[^>]*>([\\s\\S]*?)</jarvis-context>"), "「$1」\n")
    .replace(Regex("<cross-session-message[^>]*>([\\s\\S]*?)</cross-session-message>"), "$1")
    .trim()

fun toolVerb(name: String?): String = when (name) {
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
    null -> "工具"
    else -> if (name.startsWith("mcp__")) name.substringAfterLast("__") else name
}

/** One line of what the call is about: the command, the file, the pattern… */
fun toolSubject(it: Item): String {
    val i = it.toolInput as? JsonObject ?: return ""
    fun s(k: String) = (i[k] as? JsonPrimitive)?.content
    return when (it.toolName) {
        "Bash" -> s("description") ?: s("command")?.lineSequence()?.firstOrNull().orEmpty()
        "Read", "Edit", "MultiEdit", "Write", "NotebookEdit" -> (s("file_path") ?: s("notebook_path")).orEmpty().substringAfterLast('/')
        "Grep", "Glob" -> s("pattern").orEmpty()
        "WebFetch" -> s("url").orEmpty().removePrefix("https://").removePrefix("http://").substringBefore('/')
        "WebSearch", "ToolSearch" -> s("query").orEmpty()
        "Task", "Agent" -> s("description").orEmpty()
        "Skill" -> s("skill") ?: s("command").orEmpty()
        "AskUserQuestion" -> runCatching { i["questions"]!!.jsonArray[0].jsonObject["question"]!!.jsonPrimitive.content }.getOrDefault("")
        "ExitPlanMode" -> s("plan")?.lineSequence()?.firstOrNull { l -> l.isNotBlank() }?.trimStart('#', ' ').orEmpty()
        else -> ""
    }
}

/** The details shown when a tool row is expanded. */
fun toolDetail(it: Item): String {
    val i = it.toolInput as? JsonObject ?: return ""
    fun s(k: String) = (i[k] as? JsonPrimitive)?.content
    return when (it.toolName) {
        "Bash" -> "$ " + s("command").orEmpty()
        "Edit" -> "--- " + s("file_path").orEmpty() + "\n" +
            s("old_string").orEmpty().lines().joinToString("\n") { "- $it" } + "\n" +
            s("new_string").orEmpty().lines().joinToString("\n") { "+ $it" }
        "Write" -> s("file_path").orEmpty() + "\n" + s("content").orEmpty().take(4000)
        "ExitPlanMode" -> s("plan").orEmpty()
        else -> i.entries.joinToString("\n") { (k, v) -> "$k: " + ((v as? JsonPrimitive)?.content ?: v.toString()) }
    }
}
