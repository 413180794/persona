package persona.core.designsystem

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextOverflow
import java.util.Locale
import kotlin.math.abs

// A value that is drawn again with a new text (an extension's metric, stat or row value, #2811, as the web's #2757):
// when only its numbers changed they count to the new ones, written the way the value writes them; any other change
// fades in. Nothing moves the first time it is drawn or with reduced motion, and at rest the text is exactly the value.

/** The numbers in a value and the text around them: two values with the same [shape] count from one to the other. */
data class NumberParts(val shape: String, val text: List<String>, val numbers: List<Double>, val formats: List<NumberFormat>) {
    /** How a number is written: its decimals, grouped by thousands, the digits before the point when it starts with 0. */
    data class NumberFormat(val decimals: Int, val grouped: Boolean, val pad: Int)

    /** The value with its numbers at [values] (a frame of the count), each written as the value writes it. */
    fun format(values: List<Double>): String = buildString {
        append(text[0])
        values.forEachIndexed { i, n ->
            val f = formats[i]
            var s = String.format(Locale.ROOT, "%.${f.decimals}f", abs(n))
            if (f.pad > 0) s = s.padStart(f.pad + if (f.decimals > 0) f.decimals + 1 else 0, '0')
            if (f.grouped) {
                val int = s.substringBefore('.')
                val frac = s.substringAfter('.', "")
                s = int.reversed().chunked(3).joinToString(",").reversed() + if (s.contains('.')) ".$frac" else ""
            }
            append(s).append(text[i + 1])
        }
    }

    companion object {
        // 1,234.5 or 12 or 0.75; a sign, a currency, a unit and the rest stay as they are
        private val number = Regex("""\d{1,3}(?:,\d{3})+(?:\.\d+)?|\d+(?:\.\d+)?""")

        fun of(value: String): NumberParts {
            val text = mutableListOf<String>()
            val numbers = mutableListOf<Double>()
            val formats = mutableListOf<NumberFormat>()
            var at = 0
            number.findAll(value).forEach { m ->
                text += value.substring(at, m.range.first)
                at = m.range.last + 1
                val plain = m.value.replace(",", "")
                val int = plain.substringBefore('.')
                numbers += plain.toDouble()
                formats += NumberFormat(plain.substringAfter('.', "").length, m.value.contains(','), if (int.length > 1 && int[0] == '0') int.length else 0)
            }
            text += value.substring(at)
            return NumberParts(text.joinToString("#"), text, numbers, formats)
        }
    }
}

/**
 * [value] as it should be drawn this frame: while its numbers count from the last value's (same shape, other numbers)
 * the frame's text, else the value itself. A change while counting goes on from where the count is.
 */
@Composable
fun rollingText(value: String): String {
    val reduce = PersonaTheme.reduceMotion
    val duration = PersonaTheme.motion.count
    val parts = remember(value) { NumberParts.of(value) }
    var from by remember { mutableStateOf(parts) }
    var shown by remember { mutableStateOf(parts.numbers) }
    val progress = remember { Animatable(1f) }
    LaunchedEffect(parts) {
        if (reduce || from.shape != parts.shape || from.numbers == parts.numbers || parts.numbers.isEmpty()) {
            from = parts
            shown = parts.numbers
            progress.snapTo(1f)
            return@LaunchedEffect
        }
        val start = if (shown.size == parts.numbers.size) shown else from.numbers
        from = parts
        progress.snapTo(0f)
        progress.animateTo(1f, tween(duration, easing = FastOutSlowInEasing)) {
            shown = start.indices.map { i -> start[i] + (parts.numbers[i] - start[i]) * this.value }
        }
        shown = parts.numbers
    }
    // the frame the value changed in still shows the old numbers, the effect starts the count from them
    return if (!reduce && from.shape == parts.shape && shown.size == parts.numbers.size && shown != parts.numbers) parts.format(shown) else value
}

/**
 * A value's text that counts its numbers to a new value ([rollingText]) and fades in any other change (a new shape:
 * "Working" → "Waiting for you"). The first draw and reduced motion show it at once.
 */
@Composable
fun JChangingText(value: String, style: TextStyle, color: Color, modifier: Modifier = Modifier, maxLines: Int = 1,
                  overflow: TextOverflow = TextOverflow.Ellipsis) {
    val reduce = PersonaTheme.reduceMotion
    val duration = PersonaTheme.motion.quick
    val shape = remember(value) { NumberParts.of(value).shape }
    var drawn by remember { mutableStateOf(false) }
    key(shape) {
        val alpha = remember { Animatable(if (drawn && !reduce) 0f else 1f) }
        LaunchedEffect(Unit) {
            drawn = true
            if (alpha.value < 1f) alpha.animateTo(1f, tween(duration, easing = FastOutSlowInEasing))
        }
        Text(rollingText(value), modifier.graphicsLayer { this.alpha = alpha.value }, style = style, color = color, maxLines = maxLines, overflow = overflow)
    }
}
