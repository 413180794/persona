package persona.core.domain.views

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

// The cards in an assistant's reply (docs/assistant/assistant-views.md "Card identity"): the same fence rules as the hub's
// `sdk/go/wire/assistantblocks.go`, checked against `schema/extension/examples/assistant-text/fences.json`.

/**
 * A ```persona-view or ```persona-card block of a reply: its place in the text ([start] to [end]), what is inside, and
 * whether its closing fence came. [index] is its number among the text's persona-view blocks (from 0, a broken one
 * counts too); a persona-card has none (-1).
 */
data class AssistantFence(val index: Int, val start: Int, val end: Int, val language: String, val raw: String, val complete: Boolean) {
    val isView: Boolean get() = language == AssistantFences.View

    /** The key the hub registers the block under: `<item id>/<index>`; none for a persona-card. */
    fun key(itemId: String): String? = if (isView) AssistantFences.key(itemId, index) else null
}

object AssistantFences {
    const val View = "persona-view"
    const val Card = "persona-card"

    /** At most this many persona-view blocks in one text item; the hub checks the same. */
    const val MostPerText = 3

    fun key(itemId: String, index: Int) = "$itemId/$index"

    /** Cheap check before scanning: the text names a card at all. */
    fun mentions(text: String): Boolean = text.contains(View, ignoreCase = true) || text.contains(Card, ignoreCase = true)

    /**
     * The standalone fences of [text] that hold a card, in order: backticks or tildes, a longer outer fence, LF / CRLF /
     * CR line ends, the language in any case. A fence inside another code block is an example, not a card; one that
     * never closes is the last block, not complete.
     */
    fun blocks(text: String): List<AssistantFence> {
        val out = ArrayList<AssistantFence>()
        var char = 0.toChar()
        var length = 0
        var start = 0
        var content = 0
        var index = 0
        var language = ""
        var position = 0
        while (position < text.length) {
            var end = text.length
            val n = text.indexOfAny(charArrayOf('\r', '\n'), position)
            if (n >= 0) {
                end = n + 1
                if (text[end - 1] == '\r' && end < text.length && text[end] == '\n') end++
            }
            val line = text.substring(position, end)
            val fence = fence(line)
            if (char == 0.toChar()) {
                if (fence != null) {
                    char = fence.char; length = fence.length; language = fence.info; start = position; content = position + line.length
                }
            } else if (fence != null && fence.char == char && fence.length >= length && fence.info.isEmpty()) {
                if (language == View || language == Card) {
                    val ordinal = if (language == View) index++ else -1
                    out += AssistantFence(ordinal, start, position + line.length, language, text.substring(content, position), true)
                }
                char = 0.toChar()
            }
            position += line.length
        }
        if (char != 0.toChar() && (language == View || language == Card)) {
            out += AssistantFence(if (language == View) index else -1, start, text.length, language, text.substring(content), false)
        }
        return out
    }

    private class Fence(val char: Char, val length: Int, val info: String)

    private fun fence(raw: String): Fence? {
        val line = raw.removeSuffix("\n").removeSuffix("\r")
        val spaces = line.length - line.trimStart(' ').length
        if (spaces > 3) return null
        val rest = line.substring(spaces)
        if (rest.length < 3 || (rest[0] != '`' && rest[0] != '~')) return null
        val char = rest[0]
        var n = 0
        while (n < rest.length && rest[n] == char) n++
        if (n < 3) return null
        var info = rest.substring(n).trim()
        if (char == '`' && info.contains('`')) return null
        info = info.split(Regex("\\s+")).firstOrNull().orEmpty()
        return Fence(char, n, info.lowercase())
    }

    /**
     * The card's plain text: its top-level `fallback` (at most 2000 characters), never its HTML or JSON. A block cut
     * off may already hold its fallback; null when there is none to read (the app shows its own placeholder).
     */
    fun fallback(raw: String): String? {
        val text = if (raw.length > AssistantCards.MostBytes) raw.take(AssistantCards.MostBytes) else raw
        val whole = runCatching { Json.parseToJsonElement(text) }.getOrNull()
        if (whole != null) return ((whole as? JsonObject)?.get("fallback") as? JsonPrimitive)?.let(::fallbackText)
        return partialFallback(text)
    }

    private fun fallbackText(value: JsonPrimitive): String? =
        value.takeIf { it.isString }?.contentOrNull?.takeIf { it.isNotBlank() }?.let { s ->
            if (s.codePointCount(0, s.length) > 2000) s.substring(0, s.offsetByCodePoints(0, 2000)) else s
        }

    /**
     * A partial object's `fallback`, read member by member at the top level as the hub's streaming decoder does: only a
     * member written in full counts.
     */
    private fun partialFallback(text: String): String? {
        var i = skip(text, 0)
        if (i >= text.length || text[i] != '{') return null
        i++
        while (true) {
            i = skip(text, i)
            if (i >= text.length || text[i] != '"') return null
            val nameEnd = stringEnd(text, i) ?: return null
            val name = runCatching { Json.parseToJsonElement(text.substring(i, nameEnd)) as JsonPrimitive }.getOrNull()?.contentOrNull ?: return null
            i = skip(text, nameEnd)
            if (i >= text.length || text[i] != ':') return null
            i = skip(text, i + 1)
            val valueEnd = valueEnd(text, i) ?: return null
            if (name == "fallback") {
                val value = runCatching { Json.parseToJsonElement(text.substring(i, valueEnd)) }.getOrNull() as? JsonPrimitive ?: return null
                return fallbackText(value)
            }
            i = skip(text, valueEnd)
            if (i >= text.length || text[i] != ',') return null
            i++
        }
    }

    private fun skip(text: String, from: Int): Int {
        var i = from
        while (i < text.length && text[i].isWhitespace()) i++
        return i
    }

    /** The index after a JSON string starting at [from] (a quote), or null when it does not end. */
    private fun stringEnd(text: String, from: Int): Int? {
        var i = from + 1
        while (i < text.length) {
            when (text[i]) {
                '\\' -> i += 2
                '"' -> return i + 1
                else -> i++
            }
        }
        return null
    }

    /** The index after one whole JSON value starting at [from], or null when it is cut off. */
    private fun valueEnd(text: String, from: Int): Int? {
        if (from >= text.length) return null
        if (text[from] == '"') return stringEnd(text, from)
        if (text[from] == '{' || text[from] == '[') {
            var depth = 0
            var i = from
            while (i < text.length) {
                when (text[i]) {
                    '"' -> { i = stringEnd(text, i) ?: return null; continue }
                    '{', '[' -> depth++
                    '}', ']' -> if (--depth == 0) return i + 1
                }
                i++
            }
            return null
        }
        var i = from
        while (i < text.length && text[i] != ',' && text[i] != '}' && !text[i].isWhitespace()) i++
        return if (i < text.length) i else null
    }
}
