package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle

/**
 * Syntax colours for code blocks that name their language (web `rehype-highlight`, iOS `PersonaCodeHighlighter`): a
 * light tokenizer that knows comments, strings, numbers and the common keywords of the languages agents write, not a
 * grammar per language. Plain text stays the block's ink.
 */
object CodeSyntax {
    enum class Kind { Plain, Comment, String, Number, Keyword }

    /** One run of [Kind] at [start] until [end] (exclusive) in the code. */
    data class Span(val start: Int, val end: Int, val kind: Kind)

    private val keywords = setOf(
        "func", "function", "def", "class", "struct", "enum", "interface", "type", "return", "if", "else", "elif",
        "for", "while", "switch", "case", "default", "break", "continue", "var", "let", "const", "import", "from", "package",
        "public", "private", "protected", "internal", "static", "async", "await", "try", "catch", "finally", "throw", "throws",
        "new", "nil", "null", "None", "true", "false", "True", "False", "self", "this", "in", "go", "defer", "select", "chan",
        "map", "range", "fn", "impl", "pub", "use", "mod", "match", "export", "extends", "implements", "guard", "where", "with",
        "as", "lambda", "yield", "echo", "then", "fi", "do", "done", "val", "fun", "object", "when", "is", "not", "and", "or",
        "override", "open", "data", "sealed", "suspend", "mut", "trait", "loop", "until", "unless", "end", "local", "export",
    )

    /** Languages whose `#` starts a comment (shells, Python, Ruby, YAML, TOML, Makefiles…); elsewhere it is code (C's #include, CSS ids). */
    private val hashComments = setOf(
        "sh", "bash", "zsh", "shell", "fish", "console", "python", "py", "ruby", "rb", "yaml", "yml", "toml", "make",
        "makefile", "dockerfile", "perl", "pl", "r", "conf", "ini", "nginx", "cmake", "elixir", "ex", "powershell", "ps1",
    )

    /** Languages whose `--` starts a comment (SQL, Lua, Haskell). */
    private val dashComments = setOf("sql", "lua", "haskell", "hs", "elm", "ada")

    /** Languages whose `//` and `/* */` start a comment (the C family); elsewhere they are code (`a // b` in Python, URLs in shells and YAML). */
    private val slashComments = setOf(
        "c", "cpp", "c++", "h", "hpp", "java", "kotlin", "kt", "kts", "swift", "go", "rust", "rs", "js", "jsx", "ts", "tsx",
        "javascript", "typescript", "scala", "dart", "php", "css", "scss", "less", "json5", "jsonc", "proto", "groovy",
        "gradle", "cs", "csharp", "objc", "m", "mm", "zig", "sol", "solidity",
    )

    /** Info strings that are not a programming language: left uncoloured like web `rehype-highlight` leaves unknown ones. */
    private val plainLanguages = setOf(
        "text", "txt", "plaintext", "plain", "log", "logs", "output", "console-output", "diff", "patch", "markdown", "md",
        "csv", "tsv", "nohighlight",
    )

    private fun normalized(language: String) = language.trim().lowercase().substringBefore(' ')

    /** The coloured runs of [code] written in [language] (a fence's info string; case does not matter). */
    fun spans(code: String, language: String): List<Span> {
        val lang = normalized(language)
        if (lang.isEmpty() || lang in plainLanguages) return emptyList()
        val slash = lang in slashComments
        val hash = lang in hashComments
        val dash = lang in dashComments
        val out = ArrayList<Span>()
        var i = 0
        var lineStart = true
        while (i < code.length) {
            val c = code[i]
            if (c == '\n') { i++; lineStart = true; continue }
            val comment = (slash && code.startsWith("//", i)) || (hash && c == '#' && (lineStart || code[i - 1].isWhitespace())) ||
                (dash && code.startsWith("--", i))
            if (comment) {
                val end = code.indexOf('\n', i).let { if (it < 0) code.length else it }
                out += Span(i, end, Kind.Comment); i = end; continue
            }
            if (slash && code.startsWith("/*", i)) {
                val end = code.indexOf("*/", i + 2).let { if (it < 0) code.length else it + 2 }
                out += Span(i, end, Kind.Comment); i = end; continue
            }
            lineStart = false
            if (c == '"' || c == '\'' || c == '`') {
                var j = i + 1
                while (j < code.length && code[j] != c && code[j] != '\n') { if (code[j] == '\\') j++; j++ }
                val end = minOf(if (j < code.length && code[j] == c) j + 1 else j, code.length)
                out += Span(i, end, Kind.String); i = end; continue
            }
            if (c.isLetter() || c == '_') {
                var j = i
                while (j < code.length && (code[j].isLetterOrDigit() || code[j] == '_')) j++
                if (code.substring(i, j) in keywords) out += Span(i, j, Kind.Keyword)
                i = j; continue
            }
            if (c.isDigit()) {
                var j = i
                while (j < code.length && (code[j].isLetterOrDigit() || code[j] == '.' || code[j] == '_')) j++
                out += Span(i, j, Kind.Number); i = j; continue
            }
            if (!c.isWhitespace()) lineStart = false
            i++
        }
        return out
    }

    /** The colour a [Kind] is drawn in, from the palette's roles (no colours of its own: they follow the style and theme). */
    fun color(kind: Kind, palette: PersonaPalette): Color = when (kind) {
        Kind.Keyword -> palette.accent
        Kind.String -> palette.diffAdd
        Kind.Number -> palette.cautionText
        Kind.Comment -> palette.ink3
        Kind.Plain -> palette.ink
    }

    /** [text] (the code, maybe with link annotations already on it) with [language]'s syntax coloured in. */
    fun highlight(text: AnnotatedString, language: String, palette: PersonaPalette): AnnotatedString {
        if (language.isBlank()) return text
        val spans = spans(text.text, language)
        if (spans.isEmpty()) return text
        return AnnotatedString.Builder(text).apply {
            spans.forEach { addStyle(SpanStyle(color = color(it.kind, palette)), it.start, it.end) }
        }.toAnnotatedString()
    }
}
