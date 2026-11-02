package jarvis.feature.terminal

import jarvis.core.contracts.TerminalEvent
import jarvis.core.terminal.*
import java.util.Base64

internal class TerminalFrames {
    private val grid = TerminalGrid(1, 1)
    var epoch: String? = null
        private set
    var seq = 0
        private set
    val lines get() = grid.lines
    val cols get() = grid.cols
    val rows get() = grid.rows
    val cursor get() = if (grid.cursorVisible) TerminalCursor(grid.cursorRow, grid.cursorCol) else null

    enum class Result { Applied, Duplicate, Gap }
    @Synchronized fun apply(frame: TerminalEvent): Result {
        val frameEpoch = frame.epoch.orEmpty()
        val frameSeq = frame.seq?.takeIf { it in 0..Int.MAX_VALUE }?.toInt() ?: -1
        if (frameEpoch.isEmpty() || frameSeq < 0 || frame.cols !in 1..1000 || frame.rows !in 1..1000) return gap()
        if (!frame.full) {
            if (frameEpoch == epoch && frameSeq <= seq) return Result.Duplicate
            if (frameEpoch != epoch || frameSeq != seq + 1 || frame.cols != cols || frame.rows != rows) return gap()
        }
        val bytes = try { Base64.getDecoder().decode(frame.data.orEmpty()) } catch (_: IllegalArgumentException) { return gap() }
        if (frame.full) grid.reset(frame.cols, frame.rows)
        grid.feed(bytes)
        epoch = frameEpoch; seq = frameSeq
        return Result.Applied
    }
    @Synchronized fun forget() { epoch = null; seq = 0 }
    private fun gap(): Result { forget(); return Result.Gap }
}

/** Composition stays local until committed; finish without a following commit still sends exactly once. */
internal class TerminalComposition(private val send: (String) -> Unit, private val changed: (String) -> Unit) {
    var marked = ""
        private set
    fun compose(text: String) { marked = text; changed(text) }
    fun commit(text: String) { marked = ""; changed(""); if (text.isNotEmpty()) send(text) }
    fun finish() { val text = marked; commit(text) }
    fun delete(count: Int): Boolean {
        if (marked.isEmpty()) return false
        marked = marked.dropLast(count.coerceAtMost(marked.length)); changed(marked); return true
    }
    fun clear() { marked = ""; changed("") }
}

/** One wheel message per cell crossed, always anchored at the initial touch cell. */
internal class TerminalWheel(private val lineHeight: Float, val at: jarvis.core.contracts.api.TerminalCellPosition) {
    private var remainder = 0f
    fun move(delta: Float, send: (Int, jarvis.core.contracts.api.TerminalCellPosition) -> Unit) {
        remainder += delta
        val lines = (remainder / lineHeight).toInt()
        remainder -= lines * lineHeight
        // One message for the lines of a move: the node turns them into wheel events next to herdr. A message a line
        // crossed a slow link (a computer behind a proxy) once per line.
        if (lines != 0) send(lines, at)
    }
}
