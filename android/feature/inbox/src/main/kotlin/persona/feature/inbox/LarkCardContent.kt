package persona.feature.inbox

import persona.core.contracts.api.InboxAssistantTransport
import kotlinx.serialization.json.*
import org.jsoup.Jsoup

/** Requests stay bound to the pairing that supplied the message. */
class LarkContentSource(internal val transport: InboxAssistantTransport) {
    val scope: String get() = transport.scope
    suspend fun document(url: String): LarkDocument = parseLarkDocument(transport.readDocument(url))
    fun image(ref: String, document: Boolean = false): LarkPart.Image = LarkPart.Image(
        AuthenticatedLarkImage("${transport.scope}:$ref") {
            if (document && ref.startsWith("/api/v1/lark/raw/documents/")) transport.documentMedia(ref)
            else transport.media(ref)
        })
}

data class LarkCardContent(val title: String, val blocks: List<LarkCardBlock>, val summary: String)

sealed interface LarkCardBlock {
    data class Text(val text: String) : LarkCardBlock
    data class Button(
        val label: String,
        val actionId: String? = null,
        val actionMethod: Int = 1,
        val url: String? = null,
    ) : LarkCardBlock
    data class Image(val url: String, val alt: String, val width: Int = 1, val height: Int = 1) : LarkCardBlock
    data class Columns(val columns: List<List<LarkCardBlock>>, val weights: List<Float>) : LarkCardBlock
    data class Group(val children: List<LarkCardBlock>) : LarkCardBlock
    data object Divider : LarkCardBlock
}

internal fun readableLarkText(value: String): String = Jsoup.parse(
    value.replace(Regex("(?i)<br\\s*/?>"), "\n").replace("\n", "<br>"),
).body().wholeText().replace('\u00a0', ' ')

/** lark-connector's projected CardNode tree, with the raw card element shape as a fallback. */
fun parseLarkCard(meta: JsonObject, fallback: String): LarkCardContent {
    var remaining = 512
    fun plain(value: JsonElement?, depth: Int = 0): String {
        if (depth > 24) return ""
        return when (value) {
            is JsonPrimitive -> value.contentOrNull.orEmpty()
            is JsonArray -> value.joinToString("") { plain(it, depth + 1) }
            is JsonObject -> plain(value["text"] ?: value["content"] ?: value["children"], depth + 1)
            else -> ""
        }
    }
    fun objects(value: JsonElement?) = (value as? JsonArray).orEmpty().mapNotNull { it as? JsonObject }
    fun node(n: JsonObject, depth: Int = 0): List<LarkCardBlock> {
        if (depth > 24 || --remaining < 0) return listOf(LarkCardBlock.Text("部分卡片内容未显示"))
        val kids = objects(n["children"] ?: n["elements"] ?: n["actions"] ?: n["columns"])
        fun children() = kids.flatMap { node(it, depth + 1) }
        fun text() = readableLarkText(plain(n)).trim()
        return when (n.text("tag")) {
            "button" -> {
                val actionId = n.text("actionId").ifBlank { n.text("action_id") }.ifBlank { null }
                val actionMethod = n.number("actionMethod")?.toInt() ?: n.number("action_method")?.toInt() ?: 1
                val url = n.text("url").ifBlank { null }
                listOf(LarkCardBlock.Button(text().ifBlank { "操作" }, actionId, actionMethod, url))
            }
            "img", "image" -> {
                val image = n["image"] as? JsonObject
                val url = image?.text("url").orEmpty().ifBlank { n.text("url") }
                if (url.isBlank()) listOf(LarkCardBlock.Text(text().ifBlank { "图片暂不可用" }))
                else listOf(LarkCardBlock.Image(url, plain(n["alt"]).ifBlank { "卡片图片" },
                    image?.number("width")?.toInt()?.coerceAtLeast(1) ?: 1,
                    image?.number("height")?.toInt()?.coerceAtLeast(1) ?: 1))
            }
            "hr" -> listOf(LarkCardBlock.Divider)
            "column_set" -> {
                val cols = kids.filter { it.text("tag") == "column" }
                val weights = cols.map {
                    val style = it["style"] as? JsonObject
                    (style?.text("weight")?.toFloatOrNull() ?: it.text("weight").toFloatOrNull() ?: 1f)
                        .takeIf { w -> w.isFinite() && w > 0f }?.coerceIn(.1f, 10f) ?: 1f
                }
                listOf(LarkCardBlock.Columns(cols.map { node(it, depth + 1) }, weights))
            }
            "div" -> {
                val out = children().toMutableList()
                if (kids.isEmpty()) text().takeIf { it.isNotBlank() }?.let { out += LarkCardBlock.Text(it) }
                var shorts = mutableListOf<List<LarkCardBlock>>()
                fun flush() { if (shorts.isNotEmpty()) { out += LarkCardBlock.Columns(shorts.toList(), List(shorts.size) { 1f }); shorts = mutableListOf() } }
                for (field in objects(n["fields"])) {
                    val content = objects(field["children"]).flatMap { node(it, depth + 1) }
                        .ifEmpty { listOf(LarkCardBlock.Text(readableLarkText(plain(field)))) }
                    if (field.flag("short") || field.flag("is_short")) {
                        shorts += content
                        if (shorts.size == 2) flush()
                    } else { flush(); out += LarkCardBlock.Group(content) }
                }
                flush()
                out
            }
            "body", "column", "action", "note", "form", "interactive_container" -> children()
            "plain_text", "markdown", "lark_md", "link", "at", "text_tag", "code_span" ->
                text().takeIf { it.isNotBlank() }?.let { listOf(LarkCardBlock.Text(it)) }.orEmpty()
            else -> text().takeIf { it.isNotBlank() }?.let { listOf(LarkCardBlock.Text(it)) }
                ?: listOf(LarkCardBlock.Text("此内容暂不支持显示"))
        }
    }
    val title = readableLarkText(plain(meta["header"])).ifBlank { meta.text("title") }
    val body = meta["body"] as? JsonObject
    val blocks = if (body != null) node(body) else objects(meta["elements"]).flatMap { node(it) }
    val summary = (meta["fallback"] as? JsonArray).orEmpty().joinToString("\n") { plain(it) }
        .ifBlank { fallback }.ifBlank { "此卡片暂不支持显示" }
    return LarkCardContent(title, blocks, readableLarkText(summary))
}

/** Shared by the current data repository and the window-based inbox adapter. */
fun larkInteractivePart(metadata: JsonObject, fallback: String, transport: InboxAssistantTransport, messageId: String? = null): LarkPart.Card =
    LarkPart.Card(parseLarkCard(metadata, fallback), LarkContentSource(transport), messageId)
