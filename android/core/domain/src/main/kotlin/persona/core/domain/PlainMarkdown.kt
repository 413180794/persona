package persona.core.domain

/** Markdown marks off a line so it reads as prose (iOS Fmt.plain): headings, quotes, bullets, emphasis, code, links. */
fun plainMarkdown(text: String): String = text.lineSequence()
    .map { it.trim().replace(MarkdownLead, "") }
    .filter(String::isNotEmpty)
    .joinToString(" ")
    .replace(MarkdownLink, "$1")
    .replace(MarkdownMarks, "")

private val MarkdownLead = Regex("^(#{1,6}\\s+|>\\s?|[-*+]\\s+|\\d+[.)]\\s+)+")
private val MarkdownLink = Regex("!?\\[([^\\]]*)]\\([^)]*\\)")
private val MarkdownMarks = Regex("\\*\\*|__|~~|`")
