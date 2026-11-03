package persona.feature.files

// Markdown files as the viewer renders them: README HTML turned into Markdown, bare links ending where the app's
// one rule says (whitespace or any non-ASCII character), and links and images that name files on the machine.

internal object FileMarkdownText {
    /**
     * READMEs use a little HTML (`<img>`, `<a>`, `<br>`, a centred `<p>`): lines that are HTML become Markdown, other
     * tags are dropped and their text kept (iOS `MarkdownHTML`). Fenced code is left alone.
     */
    fun simplifyHtml(markdown: String): String {
        if ('<' !in markdown) return markdown
        val out = ArrayList<String>()
        var fence: String? = null
        for (line in markdown.split('\n')) {
            val trimmed = line.trim()
            if (fence != null) {
                if (trimmed.startsWith(fence)) fence = null
                out += line
                continue
            }
            if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
                fence = trimmed.take(3)
                out += line
                continue
            }
            if (trimmed.startsWith("<") && !trimmed.startsWith("<http")) {
                inlineHtml(trimmed).takeIf { it.isNotEmpty() }?.let { out += it }
                continue
            }
            out += line
        }
        return out.joinToString("\n")
    }

    private val options = setOf(RegexOption.IGNORE_CASE, RegexOption.DOT_MATCHES_ALL)
    private val image = Regex("<img\\b[^>]*>", options)
    private val anchor = Regex("<a\\b[^>]*>(.*?)</a>", options)
    private val lineBreak = Regex("<br\\s*/?>", options)
    private val anyTag = Regex("<[^>]+>")
    private val spaces = Regex("\\s+")

    private fun attribute(name: String, tag: String) =
        Regex("\\b$name\\s*=\\s*[\"']([^\"']*)[\"']", options).find(tag)?.groupValues?.get(1)

    private fun inlineHtml(html: String): String {
        var s = image.replace(html) { m -> attribute("src", m.value)?.let { "![${attribute("alt", m.value).orEmpty()}]($it)" }.orEmpty() }
        s = anchor.replace(s) { m ->
            val text = anyTag.replace(m.groupValues[1], "")
            attribute("href", m.value)?.let { "[$text]($it)" } ?: text
        }
        s = lineBreak.replace(s, " ")
        s = anyTag.replace(s, "")
        for ((entity, char) in listOf("&nbsp;" to " ", "&lt;" to "<", "&gt;" to ">", "&quot;" to "\"", "&amp;" to "&")) s = s.replace(entity, char)
        return s.split(spaces).filter { it.isNotEmpty() }.joinToString(" ")
    }

    private val bareLink = Regex("https?://[\\x21-\\x7e]+", RegexOption.IGNORE_CASE)
    private val protectedMarkdown = Regex("```[\\s\\S]*?```|~~~[\\s\\S]*?~~~|`[^`]*`|!?\\[[^\\]]*]\\([^\\n]*?\\)|<https?://[^>]*>")

    /**
     * Bare web links written as `<url>`, so a link ends at whitespace or the first non-ASCII character, minus the
     * sentence's trailing punctuation and unpaired closing brackets (the chat's rule). The autolinking parser would
     * otherwise run a link on into the Chinese text after it.
     */
    fun links(text: String): String {
        val skip = protectedMarkdown.findAll(text).map { it.range }.toList()
        return buildString {
            var cursor = 0
            for (match in bareLink.findAll(text)) {
                if (skip.any { match.range.first in it }) continue
                var url = match.value.substringBefore('<').substringBefore('>').substringBefore('"')
                do {
                    val before = url
                    url = url.trimEnd('.', ',', ';', ':', '!', '?')
                    for ((close, open) in listOf(')' to '(', ']' to '[')) {
                        while (url.endsWith(close) && url.count { it == close } > url.count { it == open }) url = url.dropLast(1)
                    }
                } while (url != before)
                if (url.length <= "https://".length) continue
                append(text, cursor, match.range.first)
                append('<').append(url).append('>')
                cursor = match.range.first + url.length
            }
            append(text, cursor, text.length)
        }
    }

    /** What the viewer renders for a Markdown file's text. */
    fun prepare(text: String): String = links(simplifyHtml(text))

    private val scheme = Regex("^[a-zA-Z][a-zA-Z0-9+.-]*:")

    /**
     * The file a link or an image in a Markdown file names, resolved against the file's folder [base]; null for web
     * links, other schemes and in-page anchors. Queries, anchors and percent-escapes are dropped from the path.
     */
    fun target(link: String, base: String): String? {
        val raw = link.trim().removePrefix("<").removeSuffix(">")
        if (raw.isEmpty() || raw.startsWith("#") || raw.startsWith("//") || scheme.containsMatchIn(raw)) return null
        val path = raw.substringBefore('#').substringBefore('?').let { decode(it) }
        if (path.isEmpty()) return null
        return FilesPaths.resolve(path, base)
    }

    private fun decode(value: String): String =
        if ('%' !in value) value else runCatching { java.net.URLDecoder.decode(value.replace("+", "%2B"), "UTF-8") }.getOrDefault(value)
}
