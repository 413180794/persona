package jarvis.feature.chat

import androidx.compose.runtime.saveable.listSaver
import java.net.URI
import java.util.UUID

internal data class ChatLinkMatch(val url: String, val start: Int, val end: Int)

internal object ChatLinks {
    private val pattern = Regex("https?://[\\x21-\\x7e]+", RegexOption.IGNORE_CASE)
    private val protectedMarkdown = Regex("```[\\s\\S]*?```|~~~[\\s\\S]*?~~~|`[^`]*`|!?\\[[^\\]]*]\\([^\\n]*?\\)|<https?://[^>]*>")

    fun webUrl(value: String): String? = runCatching {
        val uri = URI(value.trim())
        value.trim().takeIf { uri.scheme?.lowercase() in setOf("http", "https") && !uri.host.isNullOrEmpty() && uri.rawUserInfo == null }
    }.getOrNull()

    fun matches(text: String): List<ChatLinkMatch> = pattern.findAll(text).mapNotNull { match ->
        var url = match.value.substringBefore('<').substringBefore('>').substringBefore('"')
        do {
            val before = url
            url = url.trimEnd('.', ',', ';', ':', '!', '?')
            for ((close, open) in listOf(')' to '(', ']' to '[')) {
                while (url.endsWith(close) && url.count { it == close } > url.count { it == open }) url = url.dropLast(1)
            }
        } while (url != before)
        webUrl(url)?.let { ChatLinkMatch(it, match.range.first, match.range.first + it.length) }
    }.toList()

    fun markdown(text: String): String {
        val protected = protectedMarkdown.findAll(text).map { it.range }.toList()
        val links = matches(text).filter { link -> protected.none { link.start in it } }
        return buildString {
            var cursor = 0
            for (link in links) { append(text.substring(cursor, link.start)); append('<'); append(link.url); append('>'); cursor = link.end }
            append(text.substring(cursor))
        }
    }
}

/** Wire-compatible with the existing jarvis-context message format. */
data class ChatLinkContext(
    val kind: String = "context", val title: String, val url: String, val text: String,
    val id: String = UUID.randomUUID().toString(),
) {
    fun xml(): String = "<jarvis-context kind=\"${if (kind == "edit") "edit" else "context"}\" title=\"${escape(title)}\" url=\"${escape(url)}\">\n${escape(text)}\n</jarvis-context>"

    companion object {
        private val tags = Regex("<jarvis-context\\b([^>]*)>([\\s\\S]*?)</jarvis-context>\\s*")
        private val attributes = Regex("([a-z]+)=\"([^\"]*)\"")
        private fun escape(s: String) = s.replace("&", "&amp;").replace("\"", "&quot;").replace("<", "&lt;").replace(">", "&gt;")
        private fun unescape(s: String) = s.replace("&quot;", "\"").replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")
        internal fun parse(text: String): Pair<List<ChatLinkContext>, String> {
            val refs = tags.findAll(text).map { tag ->
                val attrs = attributes.findAll(tag.groupValues[1]).associate { it.groupValues[1] to unescape(it.groupValues[2]) }
                ChatLinkContext(attrs["kind"] ?: "context", attrs["title"].orEmpty(), attrs["url"].orEmpty(), unescape(tag.groupValues[2].trim()))
            }.toList()
            return refs to tags.replace(text, "").trim()
        }
    }
}

internal fun linkMessage(draft: String, contexts: List<ChatLinkContext>): String =
    (contexts.map { it.xml() } + draft).filter { it.isNotBlank() }.joinToString("\n\n")

internal val ChatLinkContextsSaver = listSaver<List<ChatLinkContext>, String>(
    save = { refs -> refs.flatMap { listOf(it.kind, it.title, it.url, it.text, it.id) } },
    restore = { values -> values.chunked(5).map { ChatLinkContext(it[0], it[1], it[2], it[3], it[4]) } },
)
