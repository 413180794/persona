package jarvis.android.terminal

/**
 * What a key is as bytes on the terminal, for the phone driving it directly (herdr's key names: "enter", "ctrl+c",
 * "shift+tab", "alt+left"), as the iOS app's TerminalKeyBytes: CSI for arrows (with `1;m` for modifiers), the control
 * character for ctrl+letter, ESC before the key for alt. shift+enter / alt+enter are ESC CR, a new line in the prompt
 * for Claude Code and Codex.
 */
object TerminalKeys {
    private const val ESC = "\u001B"
    private val csiFinal = mapOf("up" to "A", "down" to "B", "right" to "C", "left" to "D", "home" to "H", "end" to "F")
    private val tilde = mapOf(
        "insert" to 2, "delete" to 3, "pageup" to 5, "pagedown" to 6, "f5" to 15, "f6" to 17, "f7" to 18, "f8" to 19,
        "f9" to 20, "f10" to 21, "f11" to 23, "f12" to 24,
    )
    private val ss3 = mapOf("f1" to "P", "f2" to "Q", "f3" to "R", "f4" to "S")

    fun encode(name: String): String? {
        val parts = name.lowercase().split("+").toMutableList()
        // "ctrl++": the key is "+"
        if (parts.size > 1 && parts.last() == "" && parts[parts.size - 2] == "") { parts.removeAt(parts.size - 1); parts.removeAt(parts.size - 1); parts.add("+") }
        val base = parts.removeLastOrNull()?.takeIf { it.isNotEmpty() } ?: return null
        val ctrl = "ctrl" in parts || "control" in parts
        val alt = "alt" in parts || "meta" in parts || "option" in parts
        val shift = "shift" in parts
        val mod = 1 + (if (shift) 1 else 0) + (if (alt) 2 else 0) + (if (ctrl) 4 else 0)
        csiFinal[base]?.let { return if (mod == 1) "$ESC[$it" else "$ESC[1;$mod$it" }
        tilde[base]?.let { return if (mod == 1) "$ESC[$it~" else "$ESC[$it;$mod~" }
        ss3[base]?.let { return if (mod == 1) "${ESC}O$it" else "$ESC[1;$mod$it" }
        val prefix = if (alt) ESC else ""
        when (base) {
            "enter", "return" -> return if (shift || alt) "$ESC\r" else "\r"
            "tab" -> return if (shift) "$ESC[Z" else "$prefix\t"
            "esc", "escape" -> return ESC
            "backspace" -> return prefix + if (ctrl) "\u0008" else "\u007F"
            "space" -> return prefix + if (ctrl) "\u0000" else " "
        }
        if (base.codePointCount(0, base.length) != 1) return null
        if (ctrl) {
            val c = base[0]
            return when (c) {
                in 'a'..'z' -> prefix + (c.code - 0x60).toChar()
                '@', '2' -> prefix + "\u0000"
                '[', '3' -> ESC
                '\\', '4' -> prefix + "\u001C"
                ']', '5' -> prefix + "\u001D"
                '^', '6' -> prefix + "\u001E"
                '_', '-', '7', '/' -> prefix + "\u001F"
                '?', '8' -> prefix + "\u007F"
                else -> prefix + base
            }
        }
        return prefix + if (shift) base.uppercase() else base
    }

    /** Typed text: as it is, or — a paste with lines in it — as a bracketed paste, so the program takes it as one piece. */
    fun text(t: String): String {
        if (t.none { it == '\n' || it == '\r' }) return t
        return "$ESC[200~" + t.replace("\r\n", "\n").replace('\n', '\r') + "$ESC[201~"
    }
}
