package jarvis.android.terminal

/**
 * The terminal's colors: the iOS app's default theme 墨黑 (near black whatever the app's appearance, soft bright
 * colors); 16–255 are the standard xterm cube and grays.
 */
object TerminalPalette {
    const val BACKGROUND = 0xFF0C0C0E.toInt()
    const val FOREGROUND = 0xFFDCDFE4.toInt()
    const val CURSOR = 0xFF7AA2F7.toInt()
    private val ansi = intArrayOf(
        0x2A2C33, 0xF7768E, 0x73DACA, 0xE0AF68, 0x7AA2F7, 0xBB9AF7, 0x7DCFFF, 0xC0C4CE,
        0x5A5F6E, 0xFF8FA3, 0x9AF0DC, 0xF2C88A, 0x9DBBFF, 0xD0B6FF, 0xA4E0FF, 0xF4F5F7,
    )

    /** ARGB of a cell color; `fg` picks the default the theme gives. */
    fun argb(c: TermColor, fg: Boolean): Int = when (c) {
        TermColor.Standard -> if (fg) FOREGROUND else BACKGROUND
        is TermColor.Indexed -> 0xFF000000.toInt() or indexed(c.i)
        is TermColor.Rgb -> 0xFF000000.toInt() or (c.r shl 16) or (c.g shl 8) or c.b
    }

    /** xterm's 256 colors: 16 from the theme, a 6×6×6 cube, 24 grays (RGB). */
    fun indexed(i: Int): Int {
        if (i < 16) return ansi[i.coerceAtLeast(0)]
        if (i < 232) {
            val n = i - 16
            fun level(v: Int) = if (v == 0) 0 else 55 + v * 40
            return (level(n / 36) shl 16) or (level(n / 6 % 6) shl 8) or level(n % 6)
        }
        val g = 8 + (i - 232) * 10
        return (g shl 16) or (g shl 8) or g
    }
}
