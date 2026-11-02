package jarvis.android.lark

/** Just enough highlighting to read code at a glance: comments, strings, numbers, common keywords (iOS `LarkSyntax`). */
object LarkSyntax {
    enum class Kind { PLAIN, COMMENT, STRING, NUMBER, KEYWORD }

    data class Token(val text: String, val kind: Kind)

    val keywords = setOf("func", "function", "def", "class", "struct", "enum", "interface", "type", "return", "if", "else", "elif",
        "for", "while", "switch", "case", "default", "break", "continue", "var", "let", "const", "import", "from", "package", "public",
        "private", "static", "async", "await", "try", "catch", "throw", "throws", "new", "nil", "null", "None", "true", "false", "True",
        "False", "self", "this", "in", "go", "defer", "select", "chan", "map", "range", "fn", "impl", "pub", "use", "mod", "match",
        "export", "extends", "implements", "guard", "where", "with", "as", "lambda", "yield", "echo", "then", "fi", "do", "done", "val", "fun")

    fun tokens(code: String): List<Token> {
        val out = ArrayList<Token>()
        val plain = StringBuilder()
        fun flush() {
            if (plain.isNotEmpty()) { out += Token(plain.toString(), Kind.PLAIN); plain.clear() }
        }
        fun emit(s: String, k: Kind) { flush(); out += Token(s, k) }
        for ((li, line) in code.split('\n').withIndex()) {
            if (li > 0) plain.append('\n')
            var i = 0
            while (i < line.length) {
                val c = line[i]
                val rest = line.substring(i)
                if (rest.startsWith("//") || (c == '#' && (i == 0 || line[i - 1] == ' ')) || rest.startsWith("--")) {
                    emit(rest, Kind.COMMENT); break
                }
                if (c == '"' || c == '\'' || c == '`') {
                    var j = i + 1
                    while (j < line.length && line[j] != c) { if (line[j] == '\\') j++; j++ }
                    val end = minOf(if (j < line.length) j + 1 else j, line.length)
                    emit(line.substring(i, end), Kind.STRING); i = end; continue
                }
                if (c.isLetter() || c == '_') {
                    var j = i
                    while (j < line.length && (line[j].isLetterOrDigit() || line[j] == '_')) j++
                    val w = line.substring(i, j)
                    if (w in keywords) emit(w, Kind.KEYWORD) else plain.append(w)
                    i = j; continue
                }
                if (c.isDigit()) {
                    var j = i
                    while (j < line.length && (line[j].isLetterOrDigit() || line[j] == '.')) j++
                    emit(line.substring(i, j), Kind.NUMBER); i = j; continue
                }
                plain.append(c); i++
            }
        }
        flush()
        return out
    }
}
