package persona.core.terminal

sealed interface TermColor {
    data object Standard : TermColor
    data class Indexed(val index: Int) : TermColor
    data class Rgb(val red: Int, val green: Int, val blue: Int) : TermColor
}

data class TermStyle(
    val fg: TermColor = TermColor.Standard,
    val bg: TermColor = TermColor.Standard,
    val bold: Boolean = false,
    val dim: Boolean = false,
    val italic: Boolean = false,
    val underline: Boolean = false,
    val inverse: Boolean = false,
    val strike: Boolean = false,
    val hidden: Boolean = false,
    val link: String? = null,
)

data class TermCell(val text: String, val width: Int = 1, val style: TermStyle = TermStyle()) {
    val isBlank: Boolean get() = text == " " && style.bg == TermColor.Standard &&
        !style.inverse && !style.underline && !style.strike
    val isRule: Boolean get() = text.length == 1 && text[0].code in ruleScalars
    val isFiller: Boolean get() = text == " " || isRule

    companion object {
        val Blank = TermCell(" ")
        private val ruleScalars = setOf(0x2500, 0x2501, 0x2504, 0x2505, 0x2508, 0x2509, 0x254C, 0x254D, 0x2550)
    }
}

typealias TermRow = List<TermCell>

/** A VT subset for herdr's already emulated grid; printing never wraps at the right edge. */
class TerminalGrid private constructor(cols: Int, rows: Int, val growing: Boolean) {
    constructor(cols: Int, rows: Int) : this(cols, rows, false)

    var cols = cols.coerceAtLeast(1)
        private set
    var rows = rows.coerceAtLeast(1)
        private set
    private var cells = MutableList(this.rows) { mutableListOf<TermCell>() }
    // Return a stable snapshot: subsequent frames must not mutate the previous screen.
    val lines: List<TermRow> get() = cells.map { it.toList() }
    var cursorRow = 0
        private set
    var cursorCol = 0
        private set
    var cursorVisible = true
        private set
    private var style = TermStyle()
    private var parser = Parser()

    fun reset(cols: Int, rows: Int) {
        this.cols = cols.coerceAtLeast(1)
        this.rows = rows.coerceAtLeast(1)
        cells = MutableList(this.rows) { mutableListOf() }
        cursorRow = 0
        cursorCol = 0
        cursorVisible = true
        style = TermStyle()
        parser = Parser()
    }

    fun feed(text: String) { graphemes(text).forEach { parser.step(it) } }
    // Each frame decodes independently, matching Swift's replacement of incomplete UTF-8.
    fun feed(bytes: ByteArray) = feed(bytes.toString(Charsets.UTF_8))

    private fun printChar(ch: String) {
        val w = width(ch)
        val row = cells[cursorRow]
        if (w == 0) {
            if (cursorCol > 0 && cursorCol - 1 < row.size) {
                val prev = row[cursorCol - 1]
                row[cursorCol - 1] = prev.copy(text = prev.text + ch)
            }
            return
        }
        if (!growing && cursorCol.toLong() + w > cols) return
        put(TermCell(ch, w, style), cursorCol)
        if (w == 2) putRightHalf(cursorCol + 1)
        cursorCol += w
    }

    private fun extend(row: MutableList<TermCell>, col: Int) {
        while (row.size <= col) row.add(TermCell.Blank)
    }

    private fun put(cell: TermCell, col: Int) {
        val row = cells[cursorRow]
        extend(row, col)
        if (row[col].width == 0 && col > 0 && row[col - 1].width == 2) row[col - 1] = TermCell.Blank
        if (row[col].width == 2 && col + 1 < row.size && cell.width != 2) row[col + 1] = TermCell.Blank
        row[col] = cell
    }

    private fun putRightHalf(col: Int) {
        val row = cells[cursorRow]
        extend(row, col)
        // Do not clear the left half just written when redrawing a wide character in place.
        if (row[col].width == 2 && col + 1 < row.size) row[col + 1] = TermCell.Blank
        row[col] = TermCell("", 0, style)
    }

    private fun control(ch: String) {
        when (ch) {
            "\r" -> cursorCol = 0
            "\n" -> lineFeed()
            "\r\n" -> { cursorCol = 0; lineFeed() }
            "\b" -> cursorCol = (cursorCol - 1).coerceAtLeast(0)
            "\t" -> cursorCol = minOf(if (growing) cursorCol + 8 else cols - 1, (cursorCol / 8 + 1) * 8)
        }
    }

    private fun lineFeed() {
        if (growing) {
            cursorRow++
            if (cursorRow >= cells.size) cells.add(mutableListOf())
            cursorCol = 0
        } else if (cursorRow + 1 < rows) cursorRow++
        else { cells.removeAt(0); cells.add(mutableListOf()) }
    }

    private fun moveTo(row: Long, col: Long) {
        if (growing) {
            cursorRow = row.coerceIn(0, Int.MAX_VALUE.toLong()).toInt()
            while (cursorRow >= cells.size) cells.add(mutableListOf())
            cursorCol = col.coerceIn(0, Int.MAX_VALUE.toLong()).toInt()
        } else {
            cursorRow = row.coerceIn(0, (rows - 1).toLong()).toInt()
            cursorCol = col.coerceIn(0, (cols - 1).toLong()).toInt()
        }
    }

    private fun csi(final: String, params: List<Int>, marker: String?) {
        fun p(i: Int) = params.getOrNull(i)?.takeIf { it > 0 } ?: 1
        val r = cursorRow.toLong()
        val c = cursorCol.toLong()
        if (marker != null) {
            if (marker == "?" && final in listOf("h", "l") && 25 in params) cursorVisible = final == "h"
            return
        }
        when (final) {
            "H", "f" -> moveTo(p(0).toLong() - 1, p(1).toLong() - 1)
            "A" -> moveTo(r - p(0), c)
            "B", "e" -> moveTo(r + p(0), c)
            "C", "a" -> moveTo(r, c + p(0))
            "D" -> moveTo(r, c - p(0))
            "E" -> moveTo(r + p(0), 0)
            "F" -> moveTo(r - p(0), 0)
            "G", "`" -> moveTo(r, p(0).toLong() - 1)
            "d" -> moveTo(p(0).toLong() - 1, c)
            "J" -> eraseDisplay(params.firstOrNull() ?: 0)
            "K" -> eraseLine(params.firstOrNull() ?: 0)
            "X" -> blank(cursorCol, minOf(c + p(0), if (growing) Int.MAX_VALUE / 2L + 1 else cols.toLong()).toInt())
            "P" -> {
                val row = cells[cursorRow]
                if (cursorCol < row.size) row.subList(cursorCol, minOf(row.size.toLong(), c + p(0)).toInt()).clear()
            }
            "@" -> {
                val row = cells[cursorRow]
                if (cursorCol < row.size) {
                    row.addAll(cursorCol, List(if (growing) p(0) else minOf(p(0), cols - cursorCol)) { TermCell.Blank })
                    if (!growing && row.size > cols) row.subList(cols, row.size).clear()
                }
            }
            "L" -> repeat(minOf(p(0), rows)) { cells.add(cursorRow, mutableListOf()); cells.removeAt(cells.lastIndex) }
            "M" -> repeat(minOf(p(0), rows)) { cells.removeAt(cursorRow); cells.add(mutableListOf()) }
            "S" -> repeat(minOf(p(0), rows)) { cells.removeAt(0); cells.add(mutableListOf()) }
            "T" -> repeat(minOf(p(0), rows)) { cells.removeAt(cells.lastIndex); cells.add(0, mutableListOf()) }
            "m" -> sgr(params)
        }
    }

    private fun eraseDisplay(mode: Int) {
        when (mode) {
            0 -> { eraseLine(0); for (r in cursorRow + 1 until cells.size) cells[r].clear() }
            1 -> { for (r in 0 until cursorRow) cells[r].clear(); eraseLine(1) }
            else -> cells.forEach { it.clear() }
        }
    }

    private fun eraseLine(mode: Int) {
        val row = cells[cursorRow]
        when (mode) {
            0 -> blank(cursorCol, row.size)
            1 -> blank(0, minOf(cursorCol + 1, row.size))
            else -> row.clear()
        }
    }

    private fun blank(from: Int, to: Int) {
        val row = cells[cursorRow]
        if (style.bg != TermColor.Standard && row.size < to && !growing) extend(row, minOf(to, cols) - 1)
        if (from >= row.size) return
        for (c in from until minOf(to, row.size)) row[c] = TermCell(" ", style = TermStyle(bg = style.bg))
        if (style.bg == TermColor.Standard) while (row.lastOrNull()?.isBlank == true) row.removeAt(row.lastIndex)
    }

    private fun sgr(params: List<Int>) {
        val ps = params.ifEmpty { listOf(0) }
        var i = 0
        fun color(): TermColor? {
            if (ps.getOrNull(i + 1) == 5 && i + 2 < ps.size) {
                i += 2
                return TermColor.Indexed(ps[i].coerceIn(0, 255))
            }
            if (ps.getOrNull(i + 1) == 2 && i + 4 < ps.size) {
                i += 4
                return TermColor.Rgb(ps[i - 2].coerceIn(0, 255), ps[i - 1].coerceIn(0, 255), ps[i].coerceIn(0, 255))
            }
            return null
        }
        while (i < ps.size) {
            style = when (val p = ps[i]) {
                0 -> TermStyle(link = style.link)
                1 -> style.copy(bold = true)
                2 -> style.copy(dim = true)
                3 -> style.copy(italic = true)
                4 -> style.copy(underline = true)
                7 -> style.copy(inverse = true)
                8 -> style.copy(hidden = true)
                9 -> style.copy(strike = true)
                21, 22 -> style.copy(bold = false, dim = false)
                23 -> style.copy(italic = false)
                24 -> style.copy(underline = false)
                27 -> style.copy(inverse = false)
                28 -> style.copy(hidden = false)
                29 -> style.copy(strike = false)
                in 30..37 -> style.copy(fg = TermColor.Indexed(p - 30))
                38 -> style.copy(fg = color() ?: style.fg)
                39 -> style.copy(fg = TermColor.Standard)
                in 40..47 -> style.copy(bg = TermColor.Indexed(p - 40))
                48 -> style.copy(bg = color() ?: style.bg)
                49 -> style.copy(bg = TermColor.Standard)
                in 90..97 -> style.copy(fg = TermColor.Indexed(p - 90 + 8))
                in 100..107 -> style.copy(bg = TermColor.Indexed(p - 100 + 8))
                else -> style
            }
            i++
        }
    }

    private fun osc(body: String) {
        if (!body.startsWith("8;")) return
        style = style.copy(link = body.split(';', limit = 3).getOrNull(2)?.takeIf { it.isNotEmpty() })
    }

    private inner class Parser {
        var state = State.GROUND
        val params = mutableListOf<Int>()
        var current: Int? = null
        var marker: String? = null
        val body = StringBuilder()
        var bodyCount = 0
        fun step(ch: String) {
            val a = ch.singleOrNull()?.code?.takeIf { it < 128 }
            when (state) {
                State.GROUND -> when {
                    ch == "\u001b" -> state = State.ESCAPE
                    ch == "\r\n" || (a != null && (a < 32 || a == 127)) -> control(ch)
                    else -> printChar(ch)
                }
                State.ESCAPE -> when (ch) {
                    "[" -> { state = State.CSI; params.clear(); current = null; marker = null }
                    "]" -> { state = State.OSC; body.clear(); bodyCount = 0 }
                    "(", ")", "*", "+", "#", " ", "%" -> state = State.SKIP_ONE
                    else -> state = State.GROUND
                }
                State.SKIP_ONE -> state = State.GROUND
                State.CSI -> when (a) {
                    in 0x30..0x39 -> current = minOf(Int.MAX_VALUE.toLong(), (current ?: 0).toLong() * 10 + a!! - 0x30).toInt()
                    0x3B, 0x3A -> { params.add(current ?: 0); current = null }
                    in 0x3C..0x3F -> marker = ch
                    in 0x20..0x2F -> Unit
                    in 0x40..0x7E -> { current?.let { params.add(it) }; csi(ch, params, marker); state = State.GROUND }
                    else -> state = State.GROUND
                }
                State.OSC -> when (ch) {
                    "\u0007" -> { osc(body.toString()); state = State.GROUND }
                    "\u001b" -> state = State.OSC_ESCAPE
                    else -> if (bodyCount < 4096) { body.append(ch); bodyCount++ }
                }
                State.OSC_ESCAPE -> {
                    osc(body.toString()); state = State.GROUND
                    if (ch != "\\") step(ch)
                }
            }
        }
    }

    private enum class State { GROUND, ESCAPE, CSI, OSC, OSC_ESCAPE, SKIP_ONE }

    companion object {
        fun rows(fromANSI: String): List<TermRow> {
            if (fromANSI.isEmpty()) return emptyList()
            val grid = TerminalGrid(Int.MAX_VALUE, 1, true)
            val unix = fromANSI.replace("\r\n", "\n")
            grid.feed(unix)
            val out = grid.lines
            return if (out.lastOrNull()?.isEmpty() == true && unix.endsWith('\n')) out.dropLast(1) else out
        }

        fun plain(row: TermRow): String = row.filter { it.width > 0 }.joinToString("") { it.text }.trimEnd(' ')

        fun width(character: String): Int = TerminalUnicode.width(character)

        fun scrolled(old: List<TermRow>, new: List<TermRow>): Int {
            if (old.size < 4 || new.size < 4 || old[0] == new[0]) return 0
            for (k in 1 until old.size - 2) {
                var run = 0
                var telling = 0
                while (run < new.size && k + run < old.size && new[run] == old[k + run]) {
                    if (!new[run].all { it.isFiller }) telling++
                    run++
                }
                if (run >= maxOf(3, (old.size - k) / 3) && telling >= 2) return k
            }
            return 0
        }
    }
}
