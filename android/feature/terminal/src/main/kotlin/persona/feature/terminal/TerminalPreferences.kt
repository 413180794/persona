package persona.feature.terminal

import android.content.Context
import java.util.Locale

/**
 * What the phone remembers about its terminal: kept on this phone for every pairing, never sent to the hub, and kept
 * when the pairing changes.
 */
interface TerminalPreferences {
    /** "Take over" (the phone drives the pane at its own size) or "Read-only" (mirror it at the computer's size) when a terminal opens. */
    var drive: Boolean
    /** The terminal's font size in dp ([TerminalFontSize]): a new size is a new grid, so a driven pane lays itself out again. */
    var fontSize: Float
}

/** The terminal's font size: 7–20 in steps of 0.5 as on iOS, 15 unless the user changed it (iOS changed it to 15 too). */
object TerminalFontSize {
    const val DEFAULT = 15f
    const val MIN = 7f
    const val MAX = 20f
    const val STEP = 0.5f

    fun next(now: Float, up: Boolean): Float = valid(now + if (up) STEP else -STEP)

    /** A stored or computed size inside the range, on a step. */
    fun valid(size: Float): Float =
        if (size.isNaN()) DEFAULT else (Math.round(size / STEP) * STEP).coerceIn(MIN, MAX)

    /** "15", "12.5". */
    fun label(size: Float): String =
        if (size % 1f == 0f) String.format(Locale.ROOT, "%.0f", size) else String.format(Locale.ROOT, "%.1f", size)
}

/** For tests and previews: nothing is written anywhere. */
class MemoryTerminalPreferences(
    override var drive: Boolean = true,
    fontSize: Float = TerminalFontSize.DEFAULT,
) : TerminalPreferences {
    override var fontSize: Float = TerminalFontSize.valid(fontSize)
        set(value) { field = TerminalFontSize.valid(value) }
}

/** On the phone (SharedPreferences `persona-terminal`), shared by every pairing. */
internal class SharedTerminalPreferences(context: Context) : TerminalPreferences {
    private val stored = context.applicationContext.getSharedPreferences("persona-terminal", Context.MODE_PRIVATE)
    override var drive: Boolean
        get() = stored.getBoolean("drive", true)
        set(value) = stored.edit().putBoolean("drive", value).apply()
    override var fontSize: Float
        get() = TerminalFontSize.valid(stored.getFloat("font_size", TerminalFontSize.DEFAULT))
        set(value) = stored.edit().putFloat("font_size", TerminalFontSize.valid(value)).apply()
}
