package jarvis.android.terminal

// The terminal's screen model (docs/terminal.md), ported from the iOS app's TerminalGrid.swift. What arrives is not a
// program's raw output but herdr redrawing a pane's grid it has already emulated — cursor moves, SGR, text, erases,
// OSC 8 links — so a small VT subset is enough; anything else is skipped without harm. The same parser reads the
// colored scrollback pages (`/screen?format=ansi`), which are just SGR and text, one row per line.

/** A cell's color: the theme's default, one of the 256 indexed colors, or true color. */
sealed interface TermColor {
    data object Standard : TermColor
    data class Indexed(val i: Int) : TermColor
    data class Rgb(val r: Int, val g: Int, val b: Int) : TermColor
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
    /** Where an OSC 8 link goes. */
    val link: String? = null,
) {
    companion object { val Plain = TermStyle() }
}

data class TermCell(
    /** One grapheme; "" for the right half of a wide character. */
    val text: String,
    /** 1, or 2 for a wide character (its right half is a 0-width cell after it). */
    val width: Int,
    val style: TermStyle,
) {
    val isBlank: Boolean get() = text == " " && style.bg == TermColor.Standard && !style.inverse && !style.underline && !style.strike
    /** Only pads a row: a space (whatever its background) or a rule across the cell. */
    val isFiller: Boolean get() = text == " " || text in RULES

    companion object {
        val Blank = TermCell(" ", 1, TermStyle.Plain)
        private val RULES = setOf("─", "━", "┄", "┅", "┈", "┉", "╌", "╍", "═")
    }
}

typealias TermRow = List<TermCell>

/** A grid of cols × rows fed VT bytes, or — `growing` — rows that grow as text comes (the scrollback pages). */
class TerminalGrid private constructor(cols: Int, rows: Int, val growing: Boolean) {
    constructor(cols: Int, rows: Int) : this(maxOf(cols, 1), maxOf(rows, 1), false)

    var cols = cols; private set
    var rows = rows; private set
    private val grid: MutableList<MutableList<TermCell>> = MutableList(if (growing) 1 else this.rows) { mutableListOf() }
    val lines: List<TermRow> get() = grid.map { it.toList() }
    var cursorRow = 0; private set
    var cursorCol = 0; private set
    var cursorVisible = true; private set

    private var style = TermStyle()
    private val parser = Parser()

    /** A fresh screen of this size (a whole frame follows). */
    fun reset(cols: Int, rows: Int) {
        this.cols = maxOf(cols, 1); this.rows = maxOf(rows, 1)
        grid.clear(); repeat(this.rows) { grid.add(mutableListOf()) }
        cursorRow = 0; cursorCol = 0; cursorVisible = true
        style = TermStyle(); parser.reset()
    }

    fun feed(text: String) {
        var i = 0
        while (i < text.length) {
            val cp = text.codePointAt(i)
            parser.step(cp, this)
            i += Character.charCount(cp)
        }
    }

    fun feed(data: ByteArray) = feed(data.toString(Charsets.UTF_8))

    // Actions the parser takes

    /** Code points that join the grapheme before them (Swift iterates graphemes; this is the part herdr sends). */
    private var joinNext = false

    internal fun print(cp: Int) {
        if (joinNext || joins(cp)) {
            joinNext = cp == 0x200D
            appendToPrevious(cp)
            return
        }
        val w = width(cp)
        if (w == 0) { appendToPrevious(cp); return }
        if (!growing && cursorCol + w > cols) return // herdr places every row itself; nothing it sends runs past the edge
        put(TermCell(String(Character.toChars(cp)), w, style), cursorCol)
        if (w == 2) putRightHalf(cursorCol + 1)
        cursorCol += w
    }

    /** A combining mark, variation selector, skin tone or ZWJ part: onto the cell before. U+FE0F makes it emoji-wide. */
    private fun appendToPrevious(cp: Int) {
        val row = grid[cursorRow]
        var c = cursorCol - 1
        if (c >= 0 && c < row.size && row[c].width == 0) c -= 1
        if (c < 0 || c >= row.size) return
        val cell = row[c]
        if (cp == 0xFE0F && cell.width == 1 && (growing || c + 2 <= cols)) {
            row[c] = cell.copy(text = cell.text + String(Character.toChars(cp)), width = 2)
            if (c + 1 == cursorCol) { putRightHalf(cursorCol); cursorCol += 1 }
            return
        }
        row[c] = cell.copy(text = cell.text + String(Character.toChars(cp)))
    }

    /** In place: a whole screen is thousands of these. */
    private fun put(cell: TermCell, col: Int) {
        val r = grid[cursorRow]
        while (r.size <= col) r.add(TermCell.Blank)
        // Overwriting half of a wide character blanks its other half.
        if (r[col].width == 0 && col > 0 && r[col - 1].width == 2) r[col - 1] = TermCell.Blank
        if (r[col].width == 2 && col + 1 < r.size && cell.width != 2) r[col + 1] = TermCell.Blank
        r[col] = cell
    }

    /** The right half of the wide character just put at col - 1 (not `put`: see TerminalGrid.swift). */
    private fun putRightHalf(col: Int) {
        val r = grid[cursorRow]
        while (r.size <= col) r.add(TermCell.Blank)
        if (r[col].width == 2 && col + 1 < r.size) r[col + 1] = TermCell.Blank
        r[col] = TermCell("", 0, style)
    }

    internal fun control(cp: Int) {
        when (cp) {
            '\r'.code -> cursorCol = 0
            '\n'.code -> lineFeed()
            0x08 -> cursorCol = maxOf(0, cursorCol - 1)
            '\t'.code -> cursorCol = minOf(if (growing) cursorCol + 8 else cols - 1, (cursorCol / 8 + 1) * 8)
        }
        joinNext = false
    }

    private fun lineFeed() {
        if (growing) {
            cursorRow += 1
            if (cursorRow >= grid.size) grid.add(mutableListOf())
            cursorCol = 0
            return
        }
        if (cursorRow + 1 < rows) cursorRow += 1 else { grid.removeAt(0); grid.add(mutableListOf()) }
    }

    internal fun csi(final: Char, params: List<Int>, privateMarker: Char?) {
        joinNext = false
        fun p(i: Int, d: Int = 1) = if (i < params.size && params[i] > 0) params[i] else d
        val lastCol = if (growing) Int.MAX_VALUE / 2 else cols - 1
        if (privateMarker != null) {
            if (privateMarker == '?' && (final == 'h' || final == 'l') && 25 in params) cursorVisible = final == 'h'
            return // 2026 (synchronized output), mouse modes, …: nothing to keep
        }
        when (final) {
            'H', 'f' -> moveTo(p(0) - 1, p(1) - 1)
            'A' -> moveTo(cursorRow - p(0), cursorCol)
            'B', 'e' -> moveTo(cursorRow + p(0), cursorCol)
            'C', 'a' -> moveTo(cursorRow, cursorCol + p(0))
            'D' -> moveTo(cursorRow, cursorCol - p(0))
            'E' -> moveTo(cursorRow + p(0), 0)
            'F' -> moveTo(cursorRow - p(0), 0)
            'G', '`' -> moveTo(cursorRow, p(0) - 1)
            'd' -> moveTo(p(0) - 1, cursorCol)
            'J' -> eraseDisplay(params.firstOrNull() ?: 0)
            'K' -> eraseLine(params.firstOrNull() ?: 0)
            'X' -> blank(cursorRow, cursorCol, minOf(cursorCol + p(0), lastCol + 1))
            'P' -> deleteChars(p(0))
            '@' -> insertChars(p(0))
            'L' -> repeat(minOf(p(0), rows)) { grid.add(cursorRow, mutableListOf()); grid.removeAt(grid.size - 1) }
            'M' -> repeat(minOf(p(0), rows)) { grid.removeAt(cursorRow); grid.add(mutableListOf()) }
            'S' -> repeat(minOf(p(0), rows)) { grid.removeAt(0); grid.add(mutableListOf()) }
            'T' -> repeat(minOf(p(0), rows)) { grid.removeAt(grid.size - 1); grid.add(0, mutableListOf()) }
            'm' -> sgr(params)
        }
    }

    private fun moveTo(row: Int, col: Int) {
        if (growing) {
            cursorRow = maxOf(0, row)
            while (cursorRow >= grid.size) grid.add(mutableListOf())
            cursorCol = maxOf(0, col)
        } else {
            cursorRow = row.coerceIn(0, rows - 1)
            cursorCol = col.coerceIn(0, cols - 1)
        }
    }

    private fun eraseDisplay(mode: Int) {
        when (mode) {
            0 -> { eraseLine(0); for (r in cursorRow + 1 until grid.size) grid[r] = mutableListOf() }
            1 -> { for (r in 0 until cursorRow) grid[r] = mutableListOf(); eraseLine(1) }
            else -> for (r in grid.indices) grid[r] = mutableListOf()
        }
    }

    private fun eraseLine(mode: Int) {
        val n = grid[cursorRow].size
        when (mode) {
            0 -> blank(cursorRow, cursorCol, n)
            1 -> blank(cursorRow, 0, minOf(cursorCol + 1, n))
            else -> grid[cursorRow] = mutableListOf()
        }
    }

    /** Blanks cols [from, to) of a row in the current background (an erase keeps the background color). */
    private fun blank(r: Int, from: Int, to: Int) {
        val row = grid[r]
        val bg = TermCell(" ", 1, TermStyle(bg = style.bg))
        if (style.bg != TermColor.Standard && row.size < to && !growing) {
            while (row.size < minOf(to, cols)) row.add(TermCell.Blank)
        }
        if (from >= row.size) return
        for (c in from until minOf(to, row.size)) row[c] = bg
        if (style.bg == TermColor.Standard) while (row.lastOrNull()?.isBlank == true) row.removeAt(row.size - 1)
    }

    private fun deleteChars(n: Int) {
        val row = grid[cursorRow]
        if (cursorCol >= row.size) return
        repeat(minOf(row.size, cursorCol + n) - cursorCol) { row.removeAt(cursorCol) }
    }

    private fun insertChars(n: Int) {
        val row = grid[cursorRow]
        if (cursorCol >= row.size) return
        row.addAll(cursorCol, List(n) { TermCell.Blank })
        if (!growing) while (row.size > cols) row.removeAt(row.size - 1)
    }

    private fun sgr(params: List<Int>) {
        val ps = params.ifEmpty { listOf(0) }
        var i = 0
        fun color(): TermColor? {
            if (i + 1 >= ps.size) return null
            if (ps[i + 1] == 5 && i + 2 < ps.size) { i += 2; return TermColor.Indexed(ps[i].coerceIn(0, 255)) }
            if (ps[i + 1] == 2 && i + 4 < ps.size) {
                i += 4
                return TermColor.Rgb(ps[i - 2].coerceIn(0, 255), ps[i - 1].coerceIn(0, 255), ps[i].coerceIn(0, 255))
            }
            return null
        }
        var s = style
        while (i < ps.size) {
            val p = ps[i]
            s = when (p) {
                0 -> TermStyle(link = s.link)
                1 -> s.copy(bold = true)
                2 -> s.copy(dim = true)
                3 -> s.copy(italic = true)
                4 -> s.copy(underline = true)
                7 -> s.copy(inverse = true)
                8 -> s.copy(hidden = true)
                9 -> s.copy(strike = true)
                21, 22 -> s.copy(bold = false, dim = false)
                23 -> s.copy(italic = false)
                24 -> s.copy(underline = false)
                27 -> s.copy(inverse = false)
                28 -> s.copy(hidden = false)
                29 -> s.copy(strike = false)
                in 30..37 -> s.copy(fg = TermColor.Indexed(p - 30))
                38 -> color()?.let { s.copy(fg = it) } ?: s
                39 -> s.copy(fg = TermColor.Standard)
                in 40..47 -> s.copy(bg = TermColor.Indexed(p - 40))
                48 -> color()?.let { s.copy(bg = it) } ?: s
                49 -> s.copy(bg = TermColor.Standard)
                in 90..97 -> s.copy(fg = TermColor.Indexed(p - 90 + 8))
                in 100..107 -> s.copy(bg = TermColor.Indexed(p - 100 + 8))
                else -> s
            }
            i += 1
        }
        style = s
    }

    internal fun osc(body: String) {
        // OSC 8 ; params ; uri — a link from here on, an empty uri ends it.
        if (!body.startsWith("8;")) return
        val parts = body.split(";", limit = 3)
        val uri = parts.getOrNull(2).orEmpty()
        style = style.copy(link = uri.ifEmpty { null })
    }

    /** ESC / CSI / OSC recognizer, one code point at a time (a sequence may span feeds). */
    private class Parser {
        private enum class State { Ground, Escape, Csi, Osc, OscEscape, SkipOne }
        private var state = State.Ground
        private val params = ArrayList<Int>()
        private var current: Int? = null
        private var privateMarker: Char? = null
        private val oscBody = StringBuilder()

        fun reset() { state = State.Ground; params.clear(); current = null; privateMarker = null; oscBody.clear() }

        fun step(cp: Int, g: TerminalGrid) {
            when (state) {
                State.Ground -> when {
                    cp == 0x1B -> state = State.Escape
                    cp < 0x20 || cp == 0x7F -> g.control(cp)
                    else -> g.print(cp)
                }
                State.Escape -> state = when (cp) {
                    '['.code -> { params.clear(); current = null; privateMarker = null; State.Csi }
                    ']'.code -> { oscBody.clear(); State.Osc }
                    '('.code, ')'.code, '*'.code, '+'.code, '#'.code, ' '.code, '%'.code -> State.SkipOne // charset: one more
                    else -> State.Ground // ESC 7 / 8 / = / > / M …: nothing kept
                }
                State.SkipOne -> state = State.Ground
                State.Csi -> when (cp) {
                    in 0x30..0x39 -> current = (current ?: 0) * 10 + (cp - 0x30)
                    0x3B, 0x3A -> { params.add(current ?: 0); current = null } // ';' and ':' (sub-parameters, flattened)
                    in 0x3C..0x3F -> privateMarker = cp.toChar()
                    in 0x20..0x2F -> {} // intermediates
                    in 0x40..0x7E -> {
                        current?.let { params.add(it) }
                        g.csi(cp.toChar(), params, privateMarker)
                        state = State.Ground
                    }
                    else -> state = State.Ground
                }
                State.Osc -> when {
                    cp == 0x07 -> { g.osc(oscBody.toString()); state = State.Ground }
                    cp == 0x1B -> state = State.OscEscape
                    oscBody.length < 4096 -> oscBody.appendCodePoint(cp)
                }
                State.OscEscape -> { // ESC \ ends it
                    g.osc(oscBody.toString())
                    state = State.Ground
                    if (cp != '\\'.code) step(cp, g)
                }
            }
        }
    }

    companion object {
        /** Rows of colored text, one per line (a scrollback page from `/screen?format=ansi`; plain text works too). */
        fun rowsFromAnsi(text: String): List<TermRow> {
            if (text.isEmpty()) return emptyList()
            val g = TerminalGrid(Int.MAX_VALUE, 1, true)
            val unix = text.replace("\r\n", "\n")
            g.feed(unix)
            val out = g.lines.toMutableList()
            if (out.lastOrNull()?.isEmpty() == true && unix.endsWith("\n")) out.removeAt(out.size - 1)
            return out
        }

        /** The row as plain text, trailing blanks trimmed. */
        fun plain(row: TermRow): String = buildString { row.forEach { if (it.width > 0) append(it.text) } }.trimEnd(' ')

        private fun joins(cp: Int) = cp == 0x200D || cp == 0xFE0E || cp == 0xFE0F || cp in 0x1F3FB..0x1F3FF || cp in 0xE0020..0xE007F

        /** Cells a character takes: 2 for East Asian wide / fullwidth and emoji shown as emoji, 0 for a lone combining mark. */
        fun width(cp: Int): Int {
            if (cp < 0x300) return 1
            when (Character.getType(cp).toByte()) {
                Character.NON_SPACING_MARK, Character.ENCLOSING_MARK -> return 0
            }
            return if (wide(cp)) 2 else 1
        }

        private fun wide(v: Int): Boolean = when (v) {
            in 0x1100..0x115F, in 0x231A..0x231B, in 0x2329..0x232A, in 0x23E9..0x23EC, 0x23F0, 0x23F3, in 0x25FD..0x25FE,
            in 0x2614..0x2615, in 0x2648..0x2653, 0x267F, 0x2693, 0x26A1, in 0x26AA..0x26AB, in 0x26BD..0x26BE,
            in 0x26C4..0x26C5, 0x26CE, 0x26D4, 0x26EA, in 0x26F2..0x26F3, 0x26F5, 0x26FA, 0x26FD, 0x2705, in 0x270A..0x270B,
            0x2728, 0x274C, 0x274E, in 0x2753..0x2755, 0x2757, in 0x2795..0x2797, 0x27B0, 0x27BF, in 0x2B1B..0x2B1C, 0x2B50, 0x2B55,
            in 0x2E80..0x303E, in 0x3041..0x33FF, in 0x3400..0x4DBF, in 0x4E00..0x9FFF, in 0xA000..0xA4CF, in 0xA960..0xA97F,
            in 0xAC00..0xD7A3, in 0xF900..0xFAFF, in 0xFE10..0xFE19, in 0xFE30..0xFE6F, in 0xFF00..0xFF60, in 0xFFE0..0xFFE6,
            0x1F004, 0x1F0CF, 0x1F18E, in 0x1F191..0x1F19A, in 0x1F200..0x1F251, in 0x1F300..0x1F64F, in 0x1F680..0x1F6FF,
            in 0x1F7E0..0x1F7EB, in 0x1F900..0x1F9FF, in 0x1FA70..0x1FAFF, in 0x20000..0x2FFFD, in 0x30000..0x3FFFD -> true
            else -> false
        }

        /**
         * How many rows the screen moved up from `old` to `new` (0: it did not). Driving the terminal, herdr keeps no
         * scrollback, so the rows that scroll off the top are kept by the phone (see TerminalGrid.swift `scrolled`).
         */
        fun scrolled(old: List<TermRow>, new: List<TermRow>): Int {
            if (old.size < 4 || new.size < 4 || old[0] == new[0]) return 0
            for (k in 1 until old.size - 2) {
                var run = 0; var telling = 0
                while (run < new.size && k + run < old.size && new[run] == old[k + run]) {
                    if (!new[run].all { it.isFiller }) telling += 1
                    run += 1
                }
                if (run >= maxOf(3, (old.size - k) / 3) && telling >= 2) return k
            }
            return 0
        }
    }
}
