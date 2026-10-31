package jarvis.android.ui.machines

import jarvis.android.ui.settings.*
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.background
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.clipRect
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChange
import androidx.compose.ui.platform.LocalViewConfiguration
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.settings.ChartMath
import jarvis.android.settings.ChartSegment
import jarvis.android.settings.MetricPoint
import jarvis.android.settings.nearestPoint
import jarvis.android.settings.segments
import kotlinx.coroutines.withTimeoutOrNull
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlin.math.abs

/** What a chart draws: a name (the legend) and how to read it from a point. */
class ChartSeries(val name: String, val color: Color, val value: (MetricPoint) -> Double?)

/** The series' runs between gaps (cached per points list by the caller's `remember`). */
private fun List<ChartSeries>.segmentsOf(points: List<MetricPoint>): List<List<ChartSegment>> = map { s -> segments(points, s.value) }

/**
 * A detail chart (iOS `MetricChart`): the title and the value now (or at the held moment), the lines over [start]–[end]
 * (Unix seconds), a hairline grid with HH:mm and value labels, an optional dashed [reference] (a load chart's core
 * count), and a shared scrub: holding or dragging across any chart shows the same moment in all of them ([selection]).
 */
@Composable
fun MetricChart(
    title: String,
    points: List<MetricPoint>,
    series: List<ChartSeries>,
    start: Long,
    end: Long,
    step: Long,
    format: (Double) -> String,
    now: List<Double?>,
    selection: MutableState<Long?>,
    yFixed: Double? = null,
    reference: Pair<Double, String>? = null,
    // Byte values: ticks on 1024-based round numbers (2 MB, 4 MB), labelled with [axisFormat].
    binary: Boolean = false,
    axisFormat: (Double) -> String = format,
) {
    val c = kitColors
    val gridColor = MetricColors.grid
    val labelColor = c.secondaryLabel
    val segs = remember(points, series.size) { series.segmentsOf(points) }
    val top = remember(segs, yFixed, reference) {
        yFixed ?: ChartMath.niceMax(maxOf(segs.flatten().maxOfOrNull { s -> s.values.maxOrNull() ?: 0.0 } ?: 0.0, reference?.first ?: 0.0))
    }
    val yTicks = remember(top, binary) { ChartMath.yTicks(top, binary = binary) }
    val xTicks = remember(start, end) { ChartMath.timeTicks(start, end) }
    val picked = selection.value?.let { nearestPoint(points, it, step) }
    val measurer = rememberTextMeasurer()
    val small = TextStyle(fontSize = 11.sp, color = labelColor)
    val slop = LocalViewConfiguration.current.touchSlop
    val longPress = LocalViewConfiguration.current.longPressTimeoutMillis
    // The value labels sit right of the plot: as wide as the widest one.
    val axisPx = remember(yTicks, measurer) { yTicks.maxOfOrNull { measurer.measure(axisFormat(it), small).size.width } ?: 0 }

    Column(
        Modifier.fillMaxWidth().kitCard(padding = 14.dp).semantics(mergeDescendants = true) {
            contentDescription = title + "，" + summary(series, points, now, format)
        },
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.Bottom) {
            Text(title, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = c.label, modifier = Modifier.weight(1f), maxLines = 1)
            if (picked != null) Text(hm(picked.t), fontSize = 12.sp, color = c.secondaryLabel, maxLines = 1)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
            series.forEachIndexed { i, s ->
                val v = if (picked != null) s.value(picked) else now.getOrNull(i)
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                    if (series.size > 1) {
                        Box(Modifier.size(8.dp).background(s.color, CircleShape))
                        Text(s.name, fontSize = 12.sp, color = c.secondaryLabel, maxLines = 1)
                    }
                    Text(v?.let(format) ?: "—", fontSize = if (series.size > 1) 16.sp else 20.sp, fontWeight = FontWeight.SemiBold,
                        color = c.label, maxLines = 1)
                }
            }
        }
        Canvas(
            Modifier.fillMaxWidth().height(150.dp).pointerInput(start, end) {
                // Hold (or drag sideways) to read a moment; a vertical drag is the page's scroll and is left alone.
                awaitEachGesture {
                    val down = awaitFirstDown(requireUnconsumed = false)
                    val plotW = size.width - axisPx - 6.dp.toPx()
                    fun timeAt(x: Float) = start + ((x / plotW).coerceIn(0f, 1f) * (end - start)).toLong()
                    var active = false
                    var total = Offset.Zero
                    // Held still past the long-press timeout: start scrubbing even without moving.
                    val held = withTimeoutOrNull(longPress) {
                        while (true) {
                            val ev = awaitPointerEvent()
                            val ch = ev.changes.firstOrNull { it.id == down.id } ?: return@withTimeoutOrNull false
                            if (!ch.pressed) return@withTimeoutOrNull false
                            total += ch.positionChange()
                            if (abs(total.y) > slop && abs(total.y) > abs(total.x)) return@withTimeoutOrNull false
                            if (abs(total.x) > slop) { ch.consume(); return@withTimeoutOrNull true }
                        }
                        @Suppress("UNREACHABLE_CODE") false
                    }
                    if (held == false) return@awaitEachGesture
                    active = true
                    selection.value = timeAt(down.position.x + total.x)
                    while (active) {
                        val ev = awaitPointerEvent()
                        val ch = ev.changes.firstOrNull { it.id == down.id } ?: break
                        if (!ch.pressed) break
                        ch.consume()
                        selection.value = timeAt(ch.position.x)
                    }
                    selection.value = null
                }
            },
        ) {
            val axisW = axisPx + 6.dp.toPx()
            val axisH = 16.dp.toPx()
            val l = 0f
            val r = size.width - axisW
            val t = 6.dp.toPx()
            val b = size.height - axisH
            val hair = 0.5.dp.toPx()
            // Grid and labels.
            yTicks.forEach { v ->
                val y = b - ((v / top).toFloat() * (b - t))
                drawLine(gridColor, Offset(l, y), Offset(r, y), hair)
                val text = measurer.measure(axisFormat(v), small)
                drawText(text, topLeft = Offset(r + 6.dp.toPx(), (y - text.size.height / 2f).coerceIn(0f, b)))
            }
            xTicks.forEach { tt ->
                val x = l + ((tt - start).toFloat() / (end - start)) * (r - l)
                drawLine(gridColor, Offset(x, t), Offset(x, b), hair)
                val text = measurer.measure(hm(tt), small)
                val lx = (x - text.size.width / 2f).coerceIn(0f, r - text.size.width)
                drawText(text, topLeft = Offset(lx, b + 2.dp.toPx()))
            }
            clipRect(l, 0f, r, b) {
                series.forEachIndexed { i, s ->
                    segs[i].forEach { seg -> drawSegment(seg, s.color, start, end, top, l, r, t, b, 2.dp.toPx(), wash = series.size == 1, washAlpha = 0.10f) }
                }
                reference?.let { (v, label) ->
                    val y = b - ((v / top).toFloat() * (b - t))
                    drawLine(labelColor.copy(alpha = 0.6f), Offset(l, y), Offset(r, y), 1.dp.toPx(),
                        pathEffect = PathEffect.dashPathEffect(floatArrayOf(3.dp.toPx(), 3.dp.toPx())))
                    val text = measurer.measure(label, small)
                    drawText(text, topLeft = Offset(l + 2.dp.toPx(), (y - text.size.height - 2.dp.toPx()).coerceAtLeast(0f)))
                }
                if (picked != null) {
                    val x = l + ((picked.t - start).toFloat() / (end - start)) * (r - l)
                    drawLine(labelColor.copy(alpha = 0.5f), Offset(x, t), Offset(x, b), 1.dp.toPx())
                    series.forEach { s ->
                        val v = s.value(picked) ?: return@forEach
                        val y = b - ((v / top).toFloat() * (b - t))
                        drawCircle(c.card, 6.5.dp.toPx(), Offset(x, y))
                        drawCircle(s.color, 4.5.dp.toPx(), Offset(x, y))
                    }
                }
            }
        }
    }
}

/** One run as a monotone curve (and the wash below it) inside the plot rect [l, r] × [t, b]. */
private fun DrawScope.drawSegment(
    seg: ChartSegment, color: Color, start: Long, end: Long, top: Double,
    l: Float, r: Float, t: Float, b: Float, width: Float, wash: Boolean, washAlpha: Float,
) {
    val n = seg.times.size
    if (n == 0 || end <= start) return
    val xs = DoubleArray(n) { l + (seg.times[it] - start).toDouble() / (end - start) * (r - l) }
    val ys = DoubleArray(n) { b - (seg.values[it] / top) * (b - t) }
    if (n == 1) {
        drawCircle(color, width, Offset(xs[0].toFloat(), ys[0].toFloat()))
        return
    }
    val m = ChartMath.monotoneTangents(xs, ys)
    val line = Path().apply {
        moveTo(xs[0].toFloat(), ys[0].toFloat())
        for (i in 0 until n - 1) {
            val h = (xs[i + 1] - xs[i]) / 3
            cubicTo(
                (xs[i] + h).toFloat(), (ys[i] + m[i] * h).toFloat(),
                (xs[i + 1] - h).toFloat(), (ys[i + 1] - m[i + 1] * h).toFloat(),
                xs[i + 1].toFloat(), ys[i + 1].toFloat(),
            )
        }
    }
    if (wash) {
        val area = Path().apply {
            addPath(line)
            lineTo(xs[n - 1].toFloat(), b)
            lineTo(xs[0].toFloat(), b)
            close()
        }
        drawPath(area, color.copy(alpha = washAlpha))
    }
    drawPath(line, color, style = Stroke(width = width, cap = StrokeCap.Round, join = StrokeJoin.Round))
}

private val hmFormat = DateTimeFormatter.ofPattern("HH:mm")

private fun hm(t: Long): String = hmFormat.format(Instant.ofEpochSecond(t).atZone(ZoneId.systemDefault()))

private fun summary(series: List<ChartSeries>, points: List<MetricPoint>, now: List<Double?>, format: (Double) -> String): String =
    series.mapIndexed { i, s ->
        val max = points.mapNotNull { s.value(it) }.maxOrNull()
        (if (series.size > 1) s.name + " " else "") + "现在 ${now.getOrNull(i)?.let(format) ?: "未知"}，最高 ${max?.let(format) ?: "未知"}"
    }.joinToString("；")

