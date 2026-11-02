package jarvis.android.mail

/**
 * Mail HTML for the in-app reader (iOS `HTMLView`). The WebView already runs with JavaScript off and every network
 * load refused (remote images only after 「显示图片」), so this is the second fence: scripts, frames, forms, embeds,
 * `<base>` / `<meta>` / `<link>`, event handlers and `javascript:` URLs are removed before the page is built.
 * Regex-based on purpose (no HTML parser dependency); it only has to make hostile markup inert, not pretty.
 */
object MailHtml {
    private val OPT = setOf(RegexOption.IGNORE_CASE, RegexOption.DOT_MATCHES_ALL)

    /** Elements dropped with everything inside them. */
    private val BLOCKS = Regex("<(script|iframe|object|applet|frameset|noscript|template|svg|math)\\b.*?(</\\1\\s*>|$)", OPT)
    /** Elements whose tags go but whose content stays (form fields become plain text). */
    private val TAGS = Regex("</?(form|input|button|select|textarea|base|meta|link|frame|iframe|embed|object|script)\\b[^>]*>", OPT)
    private val HANDLERS = Regex("\\s+on[a-z]+\\s*=\\s*(\"[^\"]*\"|'[^']*'|[^\\s>]+)", OPT)
    private val BAD_URLS = Regex("((?:href|src|action|formaction|background|poster|xlink:href)\\s*=\\s*[\"']?)\\s*(javascript|vbscript|data:text/html|livescript)\\s*:", OPT)
    private val CSS_EXPR = Regex("expression\\s*\\(|-moz-binding|behavior\\s*:", OPT)
    private val REMOTE_IMG = Regex("<img\\b[^>]*\\bsrc\\s*=\\s*[\"']?\\s*(https?:)?//", OPT)
    private val REMOTE_CSS = Regex("url\\(\\s*[\"']?\\s*(https?:)?//", OPT)
    private val REMOTE_BG = Regex("\\bbackground\\s*=\\s*[\"']?\\s*(https?:)?//", OPT)

    /** Makes [html] inert (see the class comment). */
    fun sanitize(html: String): String {
        var s = html
        s = BLOCKS.replace(s, "")
        s = TAGS.replace(s, "")
        s = HANDLERS.replace(s, "")
        s = BAD_URLS.replace(s) { it.groupValues[1] + "about:blank#" }
        s = CSS_EXPR.replace(s, "x-blocked(")
        return s
    }

    /** The mail pulls images from the internet (trackers included): the reader asks before loading them. */
    fun hasRemoteImages(html: String): Boolean =
        REMOTE_IMG.containsMatchIn(html) || REMOTE_CSS.containsMatchIn(html) || REMOTE_BG.containsMatchIn(html)

    /**
     * The page the WebView loads: [body] (already sanitized) in a shell that fits the phone (viewport, images and
     * tables no wider than the screen, long words wrap) and follows light / dark ([dark]) with the app's colors.
     */
    fun page(body: String, dark: Boolean, textColor: String, linkColor: String): String = """
        <!DOCTYPE html><html><head><meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <meta name="color-scheme" content="${if (dark) "dark" else "light"}">
        <style>
        html,body{margin:0;padding:0;background:transparent}
        body{font-family:sans-serif;font-size:16px;line-height:1.5;color:$textColor;word-wrap:break-word;overflow-wrap:anywhere;-webkit-text-size-adjust:100%}
        img{max-width:100%!important;height:auto!important}
        table{max-width:100%!important}
        pre{white-space:pre-wrap}
        a{color:$linkColor}
        blockquote{margin-left:0;padding-left:10px;border-left:3px solid rgba(128,128,128,.35)}
        </style></head><body>$body</body></html>
    """.trimIndent()

    private val DROP_FOR_TEXT = Regex("<(script|style|head|title|template|svg)\\b.*?</\\1\\s*>", OPT)
    private val BREAKS = Regex("<br\\s*/?>|</(p|div|tr|li|h[1-6]|blockquote|table|pre)\\s*>", OPT)
    private val ANY_TAG = Regex("<[^>]+>")
    private val ENTITY = Regex("&(#x[0-9a-fA-F]+|#[0-9]+|[a-zA-Z]+);")
    private val BLANK_LINES = Regex("\n{3,}")
    private val NAMED = mapOf("nbsp" to " ", "amp" to "&", "lt" to "<", "gt" to ">", "quot" to "\"", "apos" to "'",
        "hellip" to "…", "mdash" to "—", "ndash" to "–", "copy" to "©", "reg" to "®", "middot" to "·", "zwnj" to "", "zwj" to "")

    /** HTML reduced to readable text (quoting / forwarding an HTML-only mail). */
    fun toText(html: String): String {
        if (html.isBlank()) return ""
        var s = DROP_FOR_TEXT.replace(html, "")
        s = BREAKS.replace(s, "\n")
        s = ANY_TAG.replace(s, "")
        s = ENTITY.replace(s) { m ->
            val e = m.groupValues[1]
            when {
                e.startsWith("#x") || e.startsWith("#X") -> e.substring(2).toIntOrNull(16)?.let { cp -> runCatching { String(Character.toChars(cp)) }.getOrNull() } ?: m.value
                e.startsWith("#") -> e.substring(1).toIntOrNull()?.let { cp -> runCatching { String(Character.toChars(cp)) }.getOrNull() } ?: m.value
                else -> NAMED[e.lowercase()] ?: m.value
            }
        }
        s = s.lines().joinToString("\n") { it.trimEnd() }
        return BLANK_LINES.replace(s, "\n\n").trim()
    }
}
