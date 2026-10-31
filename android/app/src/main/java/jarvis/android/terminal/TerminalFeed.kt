package jarvis.android.terminal

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** What the terminal page shows: rows of cells and the cursor (null: hidden). */
data class TerminalContent(
    val rows: List<TermRow> = emptyList(),
    val cols: Int = 0,
    val cursor: Pair<Int, Int>? = null,
    /** Where the live screen starts in rows (the phone's own scrollback is above it). */
    val screenStart: Int = 0,
)

/** The grid the phone asks for when it drives a terminal: what fits its view at the font size. */
data class GridSize(val cols: Int, val rows: Int)

/**
 * The live screen of one pane (docs/terminal.md), as the iOS app's TerminalFeed: the hub's frames applied in order to
 * a grid — a whole frame always, a diff only right after the frame before it — and whether the phone drives the
 * terminal (terminal_control). The page wires it to the hub; this keeps only the state, so it can be tested alone.
 */
class TerminalFeed {
    enum class Result { Applied, Duplicate, Gap }

    /** Driving the terminal at the phone's size: asked for, granted, another program holds it (ask the user), or not. */
    enum class Control { Off, Starting, Active, Busy, Declined }

    private val grid = TerminalGrid(1, 1)
    private var epoch: String? = null
    private var seq = 0
    /** Driving: the rows that scrolled off the top (herdr keeps no scrollback at the phone's size and redraws in place). */
    private val scrollback = ArrayList<TermRow>()

    private val _content = MutableStateFlow(TerminalContent())
    val content: StateFlow<TerminalContent> = _content.asStateFlow()
    private val _control = MutableStateFlow(Control.Off)
    val control: StateFlow<Control> = _control.asStateFlow()
    private val _note = MutableStateFlow<String?>(null)
    /** Why the phone only watches (taken back, an old node), shown with 接管. */
    val note: StateFlow<String?> = _note.asStateFlow()

    val epochToResume: String? get() = epoch
    val lastSeq: Int get() = seq

    fun apply(epoch: String, seq: Int, full: Boolean, cols: Int, rows: Int, data: ByteArray): Result {
        val driving = _control.value == Control.Active || _control.value == Control.Starting
        val before = if (driving && cols == grid.cols) grid.lines else null
        if (full) {
            grid.reset(cols, rows)
        } else if (epoch == this.epoch && seq <= this.seq) {
            return Result.Duplicate // a replay overlapping what arrived live
        } else if (epoch != this.epoch || seq != this.seq + 1) {
            this.epoch = null
            return Result.Gap
        }
        grid.feed(data)
        if (before != null) {
            val k = TerminalGrid.scrolled(before, grid.lines)
            if (k > 0) {
                scrollback.addAll(before.take(k))
                if (scrollback.size > SCROLLBACK_LIMIT) scrollback.subList(0, scrollback.size - SCROLLBACK_LIMIT).clear()
            }
        }
        this.epoch = epoch
        this.seq = seq
        publish()
        return Result.Applied
    }

    /** The stream ended (or a new subscription starts from a whole frame). */
    fun forgetStream() { epoch = null }

    /** The colored screen read once (no stream): the rows as they are, no cursor. */
    fun showPolled(rows: List<TermRow>) {
        _content.value = TerminalContent(rows, 0, null)
    }

    fun setControl(c: Control, note: String? = null) {
        _control.value = c
        _note.value = note
        if (c != Control.Active && c != Control.Starting) scrollback.clear() // back to the pane's size: what was kept goes
    }

    /** What the hub said about driving it (terminal_control). */
    fun controlChanged(state: String, why: String?) = when (state) {
        "active" -> setControl(Control.Active)
        "busy" -> setControl(Control.Busy)
        "unsupported" -> setControl(Control.Declined, why ?: "这台电脑只能看，不能接管")
        else -> setControl(Control.Declined, why ?: "终端已回到电脑上的尺寸") // taken, lost
    }

    private fun publish() {
        val lines = grid.lines
        val cursor = if (grid.cursorVisible) grid.cursorRow to grid.cursorCol else null
        // Down to the last row with text or the cursor: a shell after `clear` has forty empty rows under its prompt.
        var end = lines.size
        while (end > 0 && end - 1 > (cursor?.first ?: -1) && lines[end - 1].all { it.isBlank }) end--
        val top = scrollback.toList()
        _content.value = TerminalContent(top + lines.subList(0, end), grid.cols, cursor?.let { (r, c) -> (top.size + r) to c }, top.size)
    }

    companion object {
        const val SCROLLBACK_LIMIT = 3000
    }
}
