package persona.core.domain.answer

enum class ScreenOptionKind { Plain, TypeSomething, Chat, Unknown }
data class ParsedScreenOption(val key: String, val label: String, val detail: String?, val kind: ScreenOptionKind)
data class ScreenStep(val label: String, val done: Boolean)
data class ParsedScreenDialog(
    val header: String?,
    val question: String,
    val detail: String?,
    val options: List<ParsedScreenOption>,
    val steps: List<ScreenStep>,
)

/** Legacy screen fallback. A hub-provided dialog remains authoritative when available. */
object ScreenAnswers {
    private val numbered = Regex("^( *)(?:[❯>] *)?([1-9])\\. +(.+)$")
    private const val marks = "☐□☒✔◻■✓"
    private fun rule(line: String): Boolean = line.trim().let { it.length > 20 && it.all { c -> c in "─━-" } }

    fun parse(screen: String): ParsedScreenDialog? {
        data class Row(val index: Int, val number: Int, val label: String, val indent: Int)
        val lines = screen.lines()
        val rows = lines.mapIndexedNotNull { index, line ->
            numbered.matchEntire(line)?.let { match ->
                Row(index, match.groupValues[2].toInt(), match.groupValues[3].trim(), match.groups[3]!!.range.first)
                    .takeIf { it.label.isNotEmpty() }
            }
        }
        val start = rows.indexOfLast { it.number == 1 }
        if (start < 0) return null
        val block = mutableListOf(rows[start])
        rows.drop(start + 1).forEach { if (it.number == block.last().number + 1) block += it }
        if (block.size < 2) return null
        val options = block.mapIndexed { index, row ->
            val end = block.getOrNull(index + 1)?.index ?: minOf(lines.size, row.index + 6)
            val detail = mutableListOf<String>()
            for (line in lines.subList(row.index + 1, end)) {
                val text = line.trim()
                if (text.isEmpty() || rule(text) || line.takeWhile { it == ' ' }.length < row.indent - 1) {
                    if (detail.isNotEmpty()) break else continue
                }
                detail += text
            }
            val kind = when {
                row.label.startsWith("type something", ignoreCase = true) -> ScreenOptionKind.TypeSomething
                row.label.startsWith("chat about this", ignoreCase = true) -> ScreenOptionKind.Chat
                else -> ScreenOptionKind.Plain
            }
            ParsedScreenOption(row.number.toString(), row.label, detail.takeIf { it.isNotEmpty() }?.let(::joinWrapped), kind)
        }
        val first = block.first().index
        val top = (first - 1 downTo 0).firstOrNull { rule(lines[it]) }?.plus(1) ?: 0
        var header: String? = null
        val quoted = mutableListOf<String>()
        val plain = mutableListOf<String>()
        for (line in lines.subList(top, first)) {
            val text = line.trim()
            when {
                text.isEmpty() -> Unit
                text.first() in "☐□☒✔◻■" -> header = text.drop(1).trim()
                text.first() in "│|" -> quoted += text.drop(1).trim()
                else -> plain += text
            }
        }
        val question: String
        val details: List<String>
        when {
            quoted.isNotEmpty() -> { question = joinWrapped(quoted); details = plain }
            plain.isNotEmpty() -> { question = plain.last(); details = plain.dropLast(1) }
            else -> { question = header.orEmpty(); header = null; details = emptyList() }
        }
        val steps = mutableListOf<ScreenStep>()
        val remaining = mutableListOf<String>()
        for (line in details) {
            val count = line.count { it in marks }
            if (count < 2 && !(count >= 1 && (line.contains('←') || line.contains('→')))) {
                remaining += line
                continue
            }
            var mark: Char? = null
            val label = StringBuilder()
            fun flush() {
                val text = label.toString().trim()
                if (mark != null && text.isNotEmpty() && !text.equals("submit", ignoreCase = true)) {
                    steps += ScreenStep(text, mark?.let { it in "☒✔■✓" } == true)
                }
                label.clear()
            }
            for (char in line) when (char) {
                '←', '→' -> Unit
                in marks -> { flush(); mark = char }
                else -> label.append(char)
            }
            flush()
        }
        return ParsedScreenDialog(header, question, remaining.takeIf { it.isNotEmpty() }?.joinToString("\n"), options, steps)
    }

    private fun joinWrapped(lines: List<String>): String = buildString {
        fun cjk(c: Char) = c in '\u2e80'..'\u9fff' || c in '\uac00'..'\ud7af' || c in '\uf900'..'\ufaff' || c in '\uff00'..'\uffef'
        for (line in lines) {
            if (isNotEmpty() && line.isNotEmpty() && !cjk(last()) && !cjk(line.first())) append(' ')
            append(line)
        }
    }
}
