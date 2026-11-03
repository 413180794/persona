package persona.core.terminal

import java.util.Locale

sealed interface TerminalInputEvent {
    data class Text(val text: String) : TerminalInputEvent
    data class Key(val key: String) : TerminalInputEvent
}

data class TerminalKeyRequest(val keys: List<String>, val text: String)

/** Preserves the legacy API's text-then-keys order and erases unsent graphemes locally. */
class KeyBatcher {
    private val pending = mutableListOf<TerminalInputEvent>()
    val isEmpty: Boolean get() = pending.isEmpty()

    fun push(event: TerminalInputEvent) {
        val last = pending.lastOrNull()
        when {
            event is TerminalInputEvent.Text -> {
                if (event.text.isEmpty()) return
                if (last is TerminalInputEvent.Text) pending[pending.lastIndex] = TerminalInputEvent.Text(last.text + event.text)
                else pending.add(event)
            }
            event == TerminalInputEvent.Key("backspace") && last is TerminalInputEvent.Text -> {
                val text = last.text.dropLast(graphemes(last.text).last().length)
                if (text.isEmpty()) pending.removeAt(pending.lastIndex)
                else pending[pending.lastIndex] = TerminalInputEvent.Text(text)
            }
            else -> pending.add(event)
        }
    }

    fun take(): TerminalKeyRequest? {
        if (pending.isEmpty()) return null
        var text = ""
        var i = 0
        val first = pending[0]
        if (first is TerminalInputEvent.Text) { text = first.text; i++ }
        val keys = mutableListOf<String>()
        while (i < pending.size) {
            val key = pending[i] as? TerminalInputEvent.Key ?: break
            keys.add(key.key)
            i++
        }
        pending.subList(0, i).clear()
        return TerminalKeyRequest(keys, text)
    }
}

object TerminalKeyBytes {
    private const val ESC = "\u001b"
    private val finals = mapOf("up" to "A", "down" to "B", "right" to "C", "left" to "D", "home" to "H", "end" to "F")
    private val tildes = mapOf("insert" to 2, "delete" to 3, "pageup" to 5, "pagedown" to 6,
        "f5" to 15, "f6" to 17, "f7" to 18, "f8" to 19, "f9" to 20, "f10" to 21, "f11" to 23, "f12" to 24)
    private val ss3 = mapOf("f1" to "P", "f2" to "Q", "f3" to "R", "f4" to "S")

    fun encode(name: String): String? {
        val parts = name.lowercase(Locale.ROOT).split('+').toMutableList()
        if (parts.size > 1 && parts.last().isEmpty() && parts[parts.lastIndex - 1].isEmpty()) {
            parts.removeAt(parts.lastIndex)
            parts[parts.lastIndex] = "+"
        }
        val base = parts.removeAt(parts.lastIndex)
        if (base.isEmpty()) return null
        val ctrl = parts.any { it == "ctrl" || it == "control" }
        val alt = parts.any { it == "alt" || it == "meta" || it == "option" }
        val shift = "shift" in parts
        val mod = 1 + (if (shift) 1 else 0) + (if (alt) 2 else 0) + (if (ctrl) 4 else 0)
        finals[base]?.let { return if (mod == 1) "$ESC[$it" else "$ESC[1;$mod$it" }
        tildes[base]?.let { return if (mod == 1) "$ESC[$it~" else "$ESC[$it;$mod~" }
        ss3[base]?.let { return if (mod == 1) "${ESC}O$it" else "$ESC[1;$mod$it" }
        val prefix = if (alt) ESC else ""
        when (base) {
            "enter", "return" -> return if (shift || alt) "$ESC\r" else "\r"
            "tab" -> return if (shift) "$ESC[Z" else "$prefix\t"
            "esc", "escape" -> return ESC
            "backspace" -> return prefix + if (ctrl) "\b" else "\u007f"
            "space" -> return prefix + if (ctrl) "\u0000" else " "
        }
        if (graphemes(base).count() != 1) return null
        if (ctrl) {
            return when (val ch = base.codePointAt(0)) {
                in 'a'.code..'z'.code -> prefix + (ch - 0x60).toChar()
                '@'.code, '2'.code -> prefix + "\u0000"
                '['.code, '3'.code -> ESC
                '\\'.code, '4'.code -> prefix + "\u001c"
                ']'.code, '5'.code -> prefix + "\u001d"
                '^'.code, '6'.code -> prefix + "\u001e"
                '_'.code, '-'.code, '7'.code, '/'.code -> prefix + "\u001f"
                '?'.code, '8'.code -> prefix + "\u007f"
                else -> prefix + base
            }
        }
        return prefix + if (shift) base.uppercase(Locale.ROOT) else base
    }

    fun encodeBytes(name: String): ByteArray? = encode(name)?.toByteArray(Charsets.UTF_8)

    fun text(text: String): String {
        if (!text.any(::isTerminalNewline)) return text
        return "$ESC[200~" + text.replace("\r\n", "\n").replace('\n', '\r') + "$ESC[201~"
    }

    fun textBytes(text: String): ByteArray = text(text).toByteArray(Charsets.UTF_8)
}

enum class ModifierState { OFF, ONCE, HELD, LOCKED }

class TerminalModifiers {
    enum class Key { CTRL, ALT, SHIFT }
    private val states = MutableList(Key.entries.size) { ModifierState.OFF }
    private var usedWhileHeld = false
    operator fun get(key: Key): ModifierState = states[key.ordinal]
    operator fun set(key: Key, state: ModifierState) { states[key.ordinal] = state }
    val active: Boolean get() = states.any { it != ModifierState.OFF }

    fun apply(key: String): String = if (key == "esc") key else
        (Key.entries.filter { this[it] != ModifierState.OFF }.map { it.name.lowercase(Locale.ROOT) } + key).joinToString("+")

    fun pressed() {
        Key.entries.forEach {
            if (this[it] == ModifierState.ONCE) this[it] = ModifierState.OFF
            if (this[it] == ModifierState.HELD) usedWhileHeld = true
        }
    }

    fun tap(key: Key, quick: Boolean = false) {
        this[key] = when (this[key]) {
            ModifierState.OFF -> ModifierState.ONCE
            ModifierState.ONCE -> if (quick) ModifierState.LOCKED else ModifierState.OFF
            ModifierState.HELD, ModifierState.LOCKED -> ModifierState.OFF
        }
    }

    fun hold(key: Key) { this[key] = ModifierState.HELD; usedWhileHeld = false }
    fun release(key: Key) {
        if (this[key] == ModifierState.HELD) this[key] = if (usedWhileHeld) ModifierState.OFF else ModifierState.ONCE
    }

    fun typed(character: String): TerminalInputEvent {
        val ctrlOrAlt = this[Key.CTRL] != ModifierState.OFF || this[Key.ALT] != ModifierState.OFF
        if (!ctrlOrAlt) return TerminalInputEvent.Text(
            if (this[Key.SHIFT] != ModifierState.OFF) character.uppercase(Locale.ROOT) else character,
        )
        if (character.length != 1 || character[0].code >= 128 || character.any(::isTerminalNewline)) return TerminalInputEvent.Text(character)
        val name = when (character) {
            " " -> "space"
            "\t" -> "tab"
            else -> character.lowercase(Locale.ROOT)
        }
        return TerminalInputEvent.Key(apply(name))
    }
}
