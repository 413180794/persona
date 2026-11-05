package persona.feature.terminal

import persona.core.contracts.TerminalEvent
import persona.core.terminal.*
import java.util.Base64

internal class TerminalFrames {
    private val screen = ScrollbackGrid()
    var epoch: String? = null
        private set
    var seq = 0
        private set

    /** What the page draws: rows that scrolled off while the phone drove the terminal, then the live screen. */
    data class View(val lines: List<TermRow>, val screenStart: Int, val cols: Int, val rows: Int, val cursor: TerminalCursor?)

    @Synchronized fun view(): View {
        val kept = screen.scrollback
        return View(kept + screen.screen, kept.size, screen.cols, screen.rows,
            screen.cursor?.let { TerminalCursor(it.row + kept.size, it.col) })
    }

    enum class Result { Applied, Duplicate, Gap }
    /** [keep]: the phone drives the terminal, so rows moving up off the screen are kept ([ScrollbackGrid]). */
    @Synchronized fun apply(frame: TerminalEvent, keep: Boolean = false): Result {
        val frameEpoch = frame.epoch.orEmpty()
        val frameSeq = frame.seq?.takeIf { it in 0..Int.MAX_VALUE }?.toInt() ?: -1
        if (frameEpoch.isEmpty() || frameSeq < 0 || frame.cols !in 1..1000 || frame.rows !in 1..1000) return gap()
        if (!frame.full) {
            if (frameEpoch == epoch && frameSeq <= seq) return Result.Duplicate
            if (frameEpoch != epoch || frameSeq != seq + 1 || frame.cols != screen.cols || frame.rows != screen.rows) return gap()
        }
        val bytes = try { Base64.getDecoder().decode(frame.data.orEmpty()) } catch (_: IllegalArgumentException) { return gap() }
        screen.apply(frame.full, frame.cols, frame.rows, bytes, keep)
        epoch = frameEpoch; seq = frameSeq
        return Result.Applied
    }
    @Synchronized fun forget() { epoch = null; seq = 0 }
    /** Back to watching the pane at the computer's size: the rows kept while driving it go. */
    @Synchronized fun forgetScrollback() { screen.forgetScrollback() }
    private fun gap(): Result { forget(); return Result.Gap }
}

/** Composition stays local until committed; finish without a following commit still sends exactly once. */
internal class TerminalComposition(private val send: (String) -> Unit, private val changed: (String) -> Unit, private val key: (String) -> Unit) {
    var marked = ""
        private set
    fun compose(text: String) { marked = text; changed(text) }
    fun commit(text: String) { marked = ""; changed(""); emit(text) }
    fun finish() { val text = marked; commit(text) }
    fun delete(count: Int): Boolean {
        if (marked.isEmpty()) return false
        marked = marked.dropLast(count.coerceAtMost(marked.length)); changed(marked); return true
    }
    fun clear() { marked = ""; changed("") }
    /** Enter and tab from the soft keyboard go out as keys (as on iOS); text would arrive as a bracketed paste. */
    private fun emit(text: String) {
        var start = 0; var i = 0
        while (i < text.length) {
            val name = when (text[i]) { '\n', '\r' -> "enter"; '\t' -> "tab"; else -> null }
            if (name == null) { i++; continue }
            if (i > start) send(text.substring(start, i))
            key(name)
            i += if (text.startsWith("\r\n", i)) 2 else 1
            start = i
        }
        if (start < text.length) send(text.substring(start))
    }
}

/** One wheel message per cell crossed, always anchored at the initial touch cell. */
internal class TerminalWheel(private val lineHeight: Float, val at: persona.core.contracts.api.TerminalCellPosition) {
    private var remainder = 0f
    fun move(delta: Float, send: (Int, persona.core.contracts.api.TerminalCellPosition) -> Unit) {
        remainder += delta
        val lines = (remainder / lineHeight).toInt()
        remainder -= lines * lineHeight
        // One message for the lines of a move: the node turns them into wheel events next to herdr. A message a line
        // crossed a slow link (a computer behind a proxy) once per line.
        if (lines != 0) send(lines, at)
    }
}
