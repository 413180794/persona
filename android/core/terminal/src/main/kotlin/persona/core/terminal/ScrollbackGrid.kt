package persona.core.terminal

/**
 * A pane's grid fed frame by frame (whole frames reset it, diffs run on), keeping what scrolls off its top while the
 * phone drives the terminal. herdr keeps no scrollback at the phone's size and redraws the grid in place, so what
 * scrolled off is only what the phone noticed moving up between two frames ([TerminalGrid.scrolled]; docs/integrations/terminal.md
 * section 11 "Scrollback on the phone"). At most [limit] rows are kept, the oldest go first.
 */
class ScrollbackGrid(private val limit: Int = LIMIT) {
    private val grid = TerminalGrid(1, 1)
    private val kept = ArrayList<TermRow>()
    private var snapshot: List<TermRow> = grid.lines

    val cols: Int get() = grid.cols
    val rows: Int get() = grid.rows
    /** The live screen, row by row (a stable snapshot). */
    val screen: List<TermRow> get() = snapshot
    /** What scrolled off the top while it was kept, oldest first. */
    val scrollback: List<TermRow> get() = kept.toList()
    val scrollbackSize: Int get() = kept.size
    /** On the live screen; null while hidden. */
    val cursor: TerminalCursor? get() = if (grid.cursorVisible) TerminalCursor(grid.cursorRow, grid.cursorCol) else null

    /**
     * One frame of [bytes] at [cols] × [rows]. With [keep], rows that moved up off the screen are kept: only between
     * screens of the same width (a new width is a new layout, not a scroll).
     */
    fun apply(full: Boolean, cols: Int, rows: Int, bytes: ByteArray, keep: Boolean) {
        val before = if (keep && cols == grid.cols) snapshot else null
        // Taller at the same width (the keyboard went down): the program draws more of what it printed at the top,
        // rows the phone may have kept when the screen got shorter.
        val grew = before != null && full && rows > grid.rows
        if (full) grid.reset(cols, rows)
        grid.feed(bytes)
        snapshot = grid.lines
        if (before != null) {
            val k = TerminalGrid.scrolled(before, snapshot)
            if (k > 0) {
                kept.addAll(before.take(k))
                if (kept.size > limit) kept.subList(0, kept.size - limit).clear()
            }
        }
        if (grew) {
            val again = overlap(kept, snapshot)
            if (again > 0) kept.subList(kept.size - again, kept.size).clear()
        }
    }

    /**
     * How many of the last kept rows the screen starts with again, at most: those are on screen now, not history.
     * At least one of them must say something (blank rows and rules line up anywhere).
     */
    private fun overlap(kept: List<TermRow>, screen: List<TermRow>): Int {
        for (m in minOf(kept.size, screen.size) downTo 1) {
            val tail = kept.subList(kept.size - m, kept.size)
            if (tail.indices.all { tail[it] == screen[it] } && tail.any { row -> !row.all { it.isFiller } }) return m
        }
        return 0
    }

    /** Back to the pane's own size (or a new subscription): what was kept goes. */
    fun forgetScrollback() { kept.clear() }

    companion object {
        const val LIMIT = 3000
    }
}

/** The terminal as text: what "Select text" shows and what is drawn of a mirrored screen. */
object TerminalText {
    /**
     * How many of [rows] to show: down to the last one with something on it, or the cursor's. A shell after `clear`
     * has forty empty rows under its prompt.
     */
    fun shownRows(rows: List<TermRow>, cursorRow: Int? = null): Int {
        var end = rows.size
        while (end > 0 && end - 1 > (cursorRow ?: -1) && rows[end - 1].all { it.isBlank }) end--
        return end
    }

    /** A line of plain text per row, without the blank lines at the end: columns line up in a monospaced font. */
    fun plain(rows: List<TermRow>): String = rows.joinToString("\n") { TerminalGrid.plain(it) }.trimEnd()

    /**
     * [text] for showing in the monospaced face: Claude Code's ⏺ ⏹ ⏸ ⏵ ⏴ become ● ■ ∥ ▶ ◀, which the bundled
     * JetBrains Mono has. It has none of the former, so Android drew ⏺ as a coloured emoji tile and ⏵ as an empty box
     * (the canvas draws them as shapes, [BoxDrawing.symbol]). One cell each either way, so columns still line up; what
     * is copied stays the terminal's own text.
     */
    fun readable(text: String): String {
        if (text.none { it in shown }) return text
        return buildString(text.length) { text.forEach { append(shown[it] ?: it) } }
    }

    private val shown = mapOf('⏺' to '●', '⏹' to '■', '⏸' to '∥', '⏵' to '▶', '⏴' to '◀')
}
