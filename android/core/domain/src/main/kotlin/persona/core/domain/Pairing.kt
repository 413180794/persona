package persona.core.domain

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
        /**
         * URL schemes accepted for `pair` / `open` links. `persona` is the new name; links the hub generates
         * stay `jarvis://` until old apps no longer need them (docs/rename.md).
         */
        val schemes = listOf("persona", "jarvis")

        /**
         * Whether [text] is a full app link (`persona://` or `jarvis://`) rather than a plain hub address.
         * The `//` is required so a bare host named after a scheme (`persona:18660`) stays an address.
         */
        fun isLink(text: String): Boolean = text.trim().let { value -> schemes.any { value.startsWith("$it://") } }

        /**
         * `persona://pair?url=…&token=…` or the generated `jarvis://pair?url=…&token=…`,
         * with optional public-route metadata.
         */
        fun parse(text: String): Pairing? {
            val link = text.trim().substringBefore('#')
            val prefix = schemes.map { "$it://" }.firstOrNull(link::startsWith) ?: return null
            val authority = link.removePrefix(prefix).substringBefore('/').substringBefore('?')
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
