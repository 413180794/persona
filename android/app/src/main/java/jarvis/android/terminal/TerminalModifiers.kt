package jarvis.android.terminal

/** A modifier key's state on the terminal's key bar (iOS ModifierState). */
enum class ModifierState { Off, Once, Held, Locked }

/**
 * Ctrl / Alt / Shift on the key bar, as the iOS app's TerminalModifiers: a tap arms one for the next key, a quick
 * second tap locks it until tapped again, holding keeps it down while held (letting go without using it arms it).
 */
data class TerminalModifiers(
    val ctrl: ModifierState = ModifierState.Off,
    val alt: ModifierState = ModifierState.Off,
    val shift: ModifierState = ModifierState.Off,
    /** A key went out while one was held: letting go then releases it instead of arming it. */
    private val usedWhileHeld: Boolean = false,
) {
    enum class Key { Ctrl, Alt, Shift }

    operator fun get(k: Key) = when (k) { Key.Ctrl -> ctrl; Key.Alt -> alt; Key.Shift -> shift }
    private fun with(k: Key, s: ModifierState) = when (k) { Key.Ctrl -> copy(ctrl = s); Key.Alt -> copy(alt = s); Key.Shift -> copy(shift = s) }

    val active: Boolean get() = Key.entries.any { get(it) != ModifierState.Off }

    /** The key name herdr takes with the modifiers that are down: "ctrl+alt+x", "shift+tab". esc goes out bare. */
    fun apply(key: String): String {
        if (key == "esc") return key
        val names = listOf(Key.Ctrl to "ctrl", Key.Alt to "alt", Key.Shift to "shift")
        return (names.filter { get(it.first) != ModifierState.Off }.map { it.second } + key).joinToString("+")
    }

    /** A key went out: armed-once modifiers release, held and locked ones stay. */
    fun pressed(): TerminalModifiers {
        var m = this
        for (k in Key.entries) {
            if (m[k] == ModifierState.Once) m = m.with(k, ModifierState.Off)
            if (m[k] == ModifierState.Held) m = m.copy(usedWhileHeld = true)
        }
        return m
    }

    /** A tap; [quick] when it follows the previous tap on the same key closely (a double tap). */
    fun tap(k: Key, quick: Boolean): TerminalModifiers = with(k, when (get(k)) {
        ModifierState.Off -> ModifierState.Once
        ModifierState.Once -> if (quick) ModifierState.Locked else ModifierState.Off
        ModifierState.Held, ModifierState.Locked -> ModifierState.Off
    })

    /** Held long enough to count as holding it down. */
    fun hold(k: Key): TerminalModifiers = with(k, ModifierState.Held).copy(usedWhileHeld = false)

    /** Let go after holding. */
    fun release(k: Key): TerminalModifiers {
        if (get(k) != ModifierState.Held) return this
        return with(k, if (usedWhileHeld) ModifierState.Off else ModifierState.Once)
    }

    /**
     * What one character typed in the text field sends while modifiers are down: a key for herdr ("ctrl+c"), or — shift
     * alone, or a character no key name covers — the text itself (shift capitalized).
     */
    fun typed(ch: Char): Pair<String?, String?> {
        val ctrlOrAlt = ctrl != ModifierState.Off || alt != ModifierState.Off
        if (!ctrlOrAlt) return null to if (shift != ModifierState.Off) ch.uppercase() else ch.toString()
        if (ch.code > 127 || ch == '\n' || ch == '\r') return null to ch.toString()
        val name = when (ch) { ' ' -> "space"; '\t' -> "tab"; else -> ch.lowercase() }
        return apply(name) to null
    }
}
