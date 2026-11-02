package jarvis.android.ui.design

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.pow
import kotlin.math.sign
import kotlin.math.sin

/**
 * Jarvis' own mark, the app icon drawn small (scripts/make_icon.py): a night disc, a peach reactor ring in a coral
 * glow, the four-pointed spark and its small companion. The brand's coral lives only here (never on something to tap).
 */
@Composable
fun JarvisMark(size: Dp, modifier: Modifier = Modifier) {
    Canvas(modifier.size(size).semantics { contentDescription = "Jarvis" }) {
        val r = this.size.minDimension / 2
        val c = center
        drawCircle(Brush.verticalGradient(listOf(NightTop, NightBottom)), r, c)
        drawCircle(Brush.radialGradient(listOf(Coral.copy(alpha = 0.55f), Coral.copy(alpha = 0f)), c, r * 0.9f), r, c)
        drawCircle(Peach.copy(alpha = 0.9f), r * 0.67f, c, style = Stroke(r * 0.06f))
        drawPath(spark(c, r * 0.5f), Brush.verticalGradient(listOf(Peach, Coral, Ember), c.y - r * 0.5f, c.y + r * 0.5f))
        drawPath(spark(Offset(c.x + r * 0.33f, c.y - r * 0.33f), r * 0.1f), Peach)
    }
}

private val Coral = Color(0xFFEE7C54)
private val Peach = Color(0xFFFFC4A0)
private val Ember = Color(0xFFB03E22)
private val NightTop = Color(0xFF281812)
private val NightBottom = Color(0xFF0A0808)

/** The spark: an astroid-like star with concave sides (|x|^p + |y|^p = r^p, p < 1). */
private fun DrawScope.spark(c: Offset, r: Float): Path = Path().apply {
    val p = 0.62f
    val n = 96
    for (i in 0..n) {
        val t = 2 * PI * i / n
        val x = cos(t).let { sign(it) * abs(it).pow(2.0 / p) }
        val y = sin(t).let { sign(it) * abs(it).pow(2.0 / p) }
        val px = c.x + (x * r).toFloat()
        val py = c.y + (y * r).toFloat()
        if (i == 0) moveTo(px, py) else lineTo(px, py)
    }
    close()
}
