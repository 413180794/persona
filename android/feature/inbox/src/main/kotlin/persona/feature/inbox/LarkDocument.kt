package persona.feature.inbox

import kotlinx.serialization.json.*
import persona.core.contracts.LarkFeatures
import java.net.URI

internal fun larkWebUrl(value: String): String? = runCatching {
    val uri = URI(value.trim())
    value.trim().takeIf { uri.scheme?.lowercase() in setOf("http", "https") && !uri.host.isNullOrBlank() && uri.rawUserInfo == null }
}.getOrNull()

fun isLarkDocumentUrl(value: String): Boolean = runCatching {
    val uri = URI(larkWebUrl(value) ?: return false)
    val host = uri.host.lowercase()
    listOf("feishu.cn", "larksuite.com", "larkoffice.com").any { host == it || host.endsWith(".$it") } &&
        Regex("^/(docx|docs|doc|wiki|sheets|base|mindnotes|slides)/[^/]+").containsMatchIn(uri.path)
}.getOrDefault(false)

/** A Feishu link that opens in a native view rather than as a web page. */
sealed interface LarkNativeLink {
    /** A Feishu document: the native reader (`/api/v1/lark/raw/documents/…`). */
    data class Document(val url: String) : LarkNativeLink
    /** A company-feed (公司圈) share link: the post and its comments (`/api/v1/lark/moments/…`). */
    data class MomentsPost(val postId: String) : LarkNativeLink
}

/**
 * The native view [url] opens in while the hub has what it reads ([lark], the lark-connector extension's features, #1530);
 * null: an ordinary web page, as every Feishu link is on a hub without the extension.
 */
fun larkNativeLink(url: String, lark: LarkFeatures): LarkNativeLink? = when {
    lark.chats && isLarkDocumentUrl(url) -> LarkNativeLink.Document(url)
    lark.moments -> LarkMoments.postId(url)?.let(LarkNativeLink::MomentsPost)
    else -> null
}

data class LarkDocument(
    val title: String, val blocks: List<LarkDocumentBlock>, val partial: Boolean, val copyAllowed: Boolean,
)

data class LarkDocumentBlock(
    val kind: String, val text: String = "", val level: Int = 0, val marker: String = "",
    val image: String = "", val language: String = "",
)

fun parseLarkDocument(data: JsonObject): LarkDocument {
    require(data["object"] !is JsonObject && data["base"] !is JsonObject) { "Unsupported document" }
    val root = requireNotNull(data["content"] as? JsonObject) { "Missing document content" }
    val out = mutableListOf<LarkDocumentBlock>()
    var remaining = 10000
    fun children(n: JsonObject) = (n["content"] as? JsonArray).orEmpty().mapNotNull { it as? JsonObject }
    fun plain(n: JsonObject, depth: Int = 0): String {
        require(depth <= 40) { "Document tree too deep" }
        if (n.text("type") == "hardBreak") return "\n"
        val attrs = n["attrs"] as? JsonObject
        return n.cursor("text") ?: attrs?.cursor("label") ?: children(n).joinToString("") { plain(it, depth + 1) }
    }
    fun visit(n: JsonObject, level: Int = 0, marker: String = "") {
        require(level <= 40 && --remaining >= 0) { "Document tree too large" }
        val attrs = n["attrs"] as? JsonObject ?: JsonObject(emptyMap())
        when (val type = n.text("type")) {
            "paragraph", "heading", "codeBlock" -> out += LarkDocumentBlock(type, plain(n),
                if (type == "heading") attrs.number("level")?.toInt()?.coerceIn(1, 6) ?: 1 else level,
                marker, language = attrs.text("language"))
            "image" -> {
                val src = attrs.text("src")
                val uri = runCatching { URI(src) }.getOrNull()
                val path = uri?.rawPath.orEmpty()
                val ref = when {
                    path.startsWith("/api/documents/") -> "/api/v1/lark/raw/" + path.removePrefix("/api/") + (uri?.rawQuery?.let { "?$it" } ?: "")
                    src.startsWith("/api/v1/lark/") -> src
                    else -> ""
                }
                out += LarkDocumentBlock("image", attrs.text("caption"), image = ref)
            }
            "bulletList", "orderedList", "taskList" -> children(n).forEachIndexed { index, item ->
                val start = attrs.number("start") ?: 1
                visit(item, level + 1, when (type) { "orderedList" -> "${start + index}."; "taskList" -> "☐"; else -> "•" })
            }
            "doc", "listItem", "taskItem", "blockquote", "column", "columns" -> children(n).forEachIndexed { index, child ->
                visit(child, level + 1, if (index == 0) marker else "")
            }
            "horizontalRule" -> out += LarkDocumentBlock("divider")
            else -> out += LarkDocumentBlock("unsupported", plain(n))
        }
    }
    visit(root)
    val authority = data["authority"] as? JsonObject
    return LarkDocument(data.text("title"), out, data.flag("partial"),
        (authority?.get("copy") as? JsonPrimitive)?.booleanOrNull != false)
}
