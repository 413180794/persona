package persona.core.domain

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import persona.core.contracts.Item

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
fun buildRows(items: List<Item>, photo: String): List<Row> {
    val results = items.filter { it.kind == "tool_result" && it.toolUseId != null }.associateBy { it.toolUseId!! }
    val rows = mutableListOf<Row>()
    for (it in items) {
        when (it.kind) {
            "text" -> {
                val t = cleanText(it.text.orEmpty(), photo)
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

/**
 * A message as one plain text for previews (docs/protocol.md「消息里的标签」): what the hub / Claude Code wrap around
 * typed text unwrapped, photos as 「[图片]」. `<jarvis-context>` blocks are not what the user wrote and are left out
 * ([parseContexts]); a message of nothing but blocks previews as empty. A conversation row draws photos as thumbnails
 * instead ([messageText]) and folds the blocks into one row.
 */
fun cleanText(s: String, photo: String): String {
    val shown = messageText(parseContexts(s).second
        .replace(Regex("<cross-session-message[^>]*>([\\s\\S]*?)</cross-session-message>"), "$1"), files = false)
    return if (shown.images.isEmpty()) shown.text else ("$photo\n" + shown.text).trim()
}

fun toolVerb(name: String?): CoreText = when (name) {
    "Bash" -> CoreText.of(Key.ToolRun)
    "Read" -> CoreText.of(Key.ToolRead)
    "Edit", "MultiEdit" -> CoreText.of(Key.ToolEdit)
    "Write" -> CoreText.of(Key.ToolWrite)
    "Grep" -> CoreText.of(Key.ToolSearch)
    "Glob", "LS" -> CoreText.of(Key.ToolFind)
    "WebFetch" -> CoreText.of(Key.ToolWebFetch)
    "WebSearch" -> CoreText.of(Key.ToolWebSearch)
    "Task", "Agent" -> CoreText.of(Key.Subtask)
    "TodoWrite" -> CoreText.of(Key.ToolTodos)
    "AskUserQuestion" -> CoreText.of(Key.ToolAsk)
    "ExitPlanMode" -> CoreText.of(Key.ToolSubmitPlan)
    "EnterPlanMode" -> CoreText.of(Key.ToolEnterPlan)
    "ToolSearch" -> CoreText.of(Key.ToolLoadTools)
    "Skill" -> CoreText.of(Key.ToolSkill)
    null -> CoreText.of(Key.ToolGeneric)
    else -> CoreText.plain(if (name.startsWith("mcp__")) name.substringAfterLast("__") else name)
}

/** The MCP server's name as shown: Persona's own server keeps its old id `jarvis` (allow lists and hooks match it). */
fun mcpServerLabel(server: String): String = server.removePrefix("plugin_").let { if (it == "jarvis") "Persona" else it }

/**
 * A tool's name for a sentence (a permission card): an MCP tool as 「Persona · mail send」, others as written; null
 * without a name (the card says 工具, [CoreText.Key.ToolGeneric]).
 */
fun toolDisplayName(name: String?): String? {
    if (name.isNullOrEmpty()) return null
    if (!name.startsWith("mcp__")) return name
    val rest = name.removePrefix("mcp__")
    val i = rest.lastIndexOf("__")
    if (i <= 0) return if (rest.isEmpty()) name else mcpServerLabel(rest)
    val tool = rest.substring(i + 2).replace('_', ' ')
    return if (tool.isEmpty()) mcpServerLabel(rest.substring(0, i)) else "${mcpServerLabel(rest.substring(0, i))} · $tool"
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
