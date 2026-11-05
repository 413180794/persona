package persona.core.terminal

data class TerminalCursor(val row: Int, val col: Int)
data class DisplayLine(val row: Int, val from: Int, val to: Int, val indent: Int = 0)

object TerminalWrap {
    fun lines(rows: List<TermRow>, cursor: TerminalCursor? = null, perLine: Int, wrap: Boolean): List<DisplayLine> {
        require(perLine > 0) { "Display width must be positive" }
        val out = mutableListOf<DisplayLine>()
        rows.forEachIndexed { r, row ->
            var end = row.size
            while (end > 0 && row[end - 1].isBlank) end--
            if (cursor?.row == r) end = maxOf(end, cursor.col + 1)
            if (!wrap || end <= perLine) {
                out.add(DisplayLine(r, 0, end))
            } else {
                val cursorCol = cursor?.takeIf { it.row == r }?.col ?: -1
                val hang = hangingIndent(row, end, perLine)
                var from = 0
                while (from < end) {
                    val indent = if (from == 0) 0 else hang
                    var to = minOf(from + perLine - indent, end)
                    if (to < end) {
                        if (to < row.size && row[to].width == 0 && to - 1 > from) to--
                        to = breakPoint(row, from, to) ?: to
                    }
                    out.add(DisplayLine(r, from, to, indent))
                    from = to
                    if (from < end && row.drop(from).take(end - from).all { it.isFiller } && cursorCol < from) break
                    var skip = from
                    while (skip < minOf(end, row.size) && row[skip].isBlank && cursorCol != skip) skip++
                    if (skip < end) from = skip
                }
            }
        }
        return out
    }

    private fun breakPoint(row: TermRow, from: Int, to: Int): Int? {
        if (canBreak(row, to)) return null
        var k = to - 1
        while (k > from + (to - from) / 2) {
            if (canBreak(row, k)) return k
            k--
        }
        return null
    }

    private fun canBreak(row: TermRow, k: Int): Boolean {
        if (k <= 0 || k >= row.size) return true
        val next = row[k]
        val prev = row[k - 1]
        if (next.text == " " || prev.text == " ") return true
        if (next.text in closing || prev.text in opening) return false
        return next.width == 2 || prev.width == 0
    }

    private fun hangingIndent(row: TermRow, end: Int, perLine: Int): Int {
        var i = 0
        // A cursor can extend beyond the stored row; those cells are implicit blanks.
        fun cell(at: Int) = row.getOrElse(at) { TermCell.Blank }
        fun spaces() { while (i < end && (cell(i).text == " " || cell(i).text in borders)) i++ }
        spaces()
        val start = i
        if (i < end && cell(i).text in markers) i += cell(i).width
        else {
            while (i < end && isNumber(cell(i).text)) i++
            if (i > start && i < end && cell(i).text in setOf(".", ")")) i++ else i = start
        }
        if (i > start) {
            if (i < end && cell(i).text == " ") spaces() else i = start
        }
        return if (i >= end) 0 else minOf(i, perLine / 2)
    }

    private fun isNumber(text: String): Boolean = text.isNotEmpty() && graphemes(text).count() == 1 &&
        Character.getType(text.codePointAt(0)) in setOf(
            Character.DECIMAL_DIGIT_NUMBER.toInt(), Character.LETTER_NUMBER.toInt(), Character.OTHER_NUMBER.toInt(),
        )

    private val closing = "，。、：；！？）】」』》”’,.:;!?)]}".map { it.toString() }.toSet()  // i18n-ignore: line-break punctuation matched against terminal cells, not shown
    private val opening = "（【「『《“‘([{".map { it.toString() }.toSet()  // i18n-ignore: line-break punctuation matched against terminal cells, not shown
    private val borders = setOf("│", "┃", "║", "▏", "▎")
    private val markers = setOf("⏺", "⎿", "❯", "›", ">", "•", "·", "-", "*", "+", "✻", "✶", "✳", "◦", "▸", "▪", "☐", "☒", "✔", "✘")
}
