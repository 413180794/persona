package jarvis.core.domain

import java.net.URLDecoder
import java.nio.charset.StandardCharsets

/** The old URL and token remain the only values used by the app until route selection is introduced. */
data class Pairing(
    val url: String,
    val token: String,
    val hub: String? = null,
    val fp: String? = null,
    val tls: Int? = null,
    val alt: List<String> = emptyList(),
) {
    companion object {
        /** `jarvis://pair?url=…&token=…`, with optional public-route metadata. */
        fun parse(text: String): Pairing? {
            val link = text.trim().substringBefore('#')
            if (!link.startsWith("jarvis://")) return null
            val authority = link.removePrefix("jarvis://").substringBefore('/').substringBefore('?')
            if (decode(authority.substringAfterLast('@').substringBefore(':')) != "pair") return null
            val query = link.substringAfter('?', "").split('&').map {
                it.substringBefore('=') to decode(it.substringAfter('=', ""))
            }
            fun first(key: String) = query.firstOrNull { it.first == key }?.second
            val url = first("url")?.trim().orEmpty()
            val token = first("token")?.trim().orEmpty()
            if (!url.startsWith("http") || token.isEmpty()) return null
            return Pairing(
                url, token,
                hub = first("hub"),
                fp = first("fp"),
                tls = first("tls")?.toIntOrNull(),
                alt = query.filter { it.first == "alt" }.map { it.second },
            )
        }

        private fun decode(value: String): String =
            runCatching { URLDecoder.decode(value, StandardCharsets.UTF_8.name()) }.getOrDefault(value)
    }
}
