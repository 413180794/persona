package persona.feature.files

import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue

/** Apply to the current draft, then focus the composer; this never sends a message. */
fun insertFilePath(value: TextFieldValue, path: String, focused: Boolean = true): TextFieldValue {
    if (path.isEmpty()) return value
    val text = value.text
    var start = if (focused) value.selection.min.coerceIn(0, text.length) else text.length
    var end = if (focused) value.selection.max.coerceIn(start, text.length) else text.length
    fun splitsSurrogate(offset: Int) = offset > 0 && offset < text.length && text[offset - 1].isHighSurrogate() && text[offset].isLowSurrogate()
    if (splitsSurrogate(start)) start--
    if (splitsSurrogate(end)) end++
    val before = text.substring(0, start)
    val after = text.substring(end)
    val quoted = if (path.any { it.isWhitespace() || it == '\'' || it == '"' }) "'" + path.replace("'", "'\\''") + "'" else path
    val insertion = (if (before.lastOrNull()?.isWhitespace() == false) " " else "") + quoted +
        (if (after.firstOrNull()?.isWhitespace() != true) " " else "")
    return TextFieldValue(before + insertion + after, TextRange(before.length + insertion.length))
}
