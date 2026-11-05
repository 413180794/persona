package persona.core.designsystem

import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.log10
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow

// Charts and number grids drawn from the design system (an extension's chart and stat_grid, docs/extensions.md「UI 插槽」):
// series colours come from the palette (the style's accent first) or a role colour (Tone); sizes, type and lines
// from the style in use. Nothing here knows about extensions.

enum class JChartKind { Bar, Line }

/** One line, or one colour of bars: a value per label, how each is written, and a role colour or the next series colour. */
data class JChartSeries(val name: String?, val values: List<Double>, val displays: List<String>, val tone: Tone? = null)

/** A chart's data: the labels along the bottom (one per value) and 1–4 series. [axis] writes the side scale's numbers. */
data class JChartData(
    val kind: JChartKind,
    val labels: List<String>,
    val series: List<JChartSeries>,
    val stacked: Boolean = false,
    val axis: (Double) -> String = ChartMath::compact,
)

/** A colour per series when the chart gives no role colour: the style's accent, then three hues that differ from it. */
fun PersonaPalette.series(index: Int): Color {
    val hues = if (dark) listOf(Tokens.Common.Dark.Hue.purple, Tokens.Common.Dark.Hue.orange, Tokens.Common.Dark.Hue.green)
    else listOf(Tokens.Common.Light.Hue.purple, Tokens.Common.Light.Hue.orange, Tokens.Common.Light.Hue.green)
    return if (index <= 0) accent else hues[(index - 1) % hues.size]
}

/** The arithmetic of a chart, apart from drawing it (tested on the JVM). */
object ChartMath {
    /** A number written short: 1,234 → 1.2K, 1,200,000 → 1.2M; small ones keep up to two decimals. */
    fun compact(value: Double): String {
        if (!value.isFinite()) return "—"
        val a = abs(value)
        val (scaled, suffix) = when {
            a >= 1e12 -> value / 1e12 to "T"
            a >= 1e9 -> value / 1e9 to "B"
            a >= 1e6 -> value / 1e6 to "M"
            a >= 1e4 -> value / 1e3 to "K"
            else -> value to ""
        }
        val digits = when {
            suffix.isNotEmpty() -> if (abs(scaled) >= 100) 0 else 1
            a >= 100 || a == floor(a) -> 0
            a >= 1 -> 1
            else -> 2
        }
        val text = if (digits == 0 && suffix.isEmpty() && a >= 1000) String.format(java.util.Locale.ROOT, "%,.0f", value)
        else String.format(java.util.Locale.ROOT, "%.${digits}f", scaled)
        return text.trimDecimals() + suffix
    }

    private fun String.trimDecimals(): String = if (contains('.')) trimEnd('0').trimEnd('.') else this

    /** The lowest and highest the plot shows: always with zero, stacked bars by their sums above and below it. */
    fun range(data: JChartData): Pair<Double, Double> {
        val n = data.labels.size
        var low = 0.0
        var high = 0.0
        if (data.kind == JChartKind.Bar && data.stacked && data.series.size > 1) {
            for (i in 0 until n) {
                val values = data.series.mapNotNull { it.values.getOrNull(i)?.takeIf(Double::isFinite) }
                high = max(high, values.filter { it > 0 }.sum())
                low = min(low, values.filter { it < 0 }.sum())
            }
        } else data.series.forEach { s -> s.values.filter(Double::isFinite).forEach { high = max(high, it); low = min(low, it) } }
        if (high == low) high = low + 1.0
        return low to high
    }

    /** About [count] evenly spaced round values that cover low…high, zero among them. */
    fun ticks(low: Double, high: Double, count: Int = 3): List<Double> {
        val span = (high - low).takeIf { it > 0 } ?: 1.0
        val raw = span / max(1, count)
        val magnitude = 10.0.pow(floor(log10(raw)))
        val step = listOf(1.0, 2.0, 2.5, 5.0, 10.0).map { it * magnitude }.first { it >= raw }
        val first = floor(low / step) * step
        val last = ceil(high / step) * step
        return generateSequence(first) { it + step }.takeWhile { it <= last + step / 2 }.map { if (abs(it) < step / 1e6) 0.0 else it }.toList()
    }

    /**
     * Which labels to write when labels [widths] px wide, each centred under its slot of [width] / n px (kept inside
     * the plot), share [width] px: every k-th from the first, with the last in place of the one before it when they
     * would crowd. No two written labels come closer than [gap].
     */
    fun labelIndices(widths: List<Float>, width: Float, gap: Float): List<Int> {
        val n = widths.size
        if (n == 0) return emptyList()
        val spans = widths.indices.map { labelLeft(it, widths[it], width, n).let { left -> left to left + widths[it] } }
        fun fits(list: List<Int>) = list.zipWithNext().all { (a, b) -> spans[b].first >= spans[a].second + gap }
        var every = max(1, ceil((widths.max() + gap) / (width / n)).toInt())
        while (true) {
            val shown = (0 until n step every).toMutableList()
            if (shown.last() != n - 1) {
                if (fits(listOf(shown.last(), n - 1))) shown += n - 1
                else if (shown.size > 1) shown[shown.lastIndex] = n - 1
            }
            if (fits(shown)) return shown
            if (every >= n) return listOf(0)
            every++
        }
    }

    /** Where a label [w] px wide under slot [i] of [n] in [width] px starts: centred, but never past either end. */
    fun labelLeft(i: Int, w: Float, width: Float, n: Int): Float {
        val slot = width / max(1, n)
        return (slot * i + slot / 2 - w / 2).coerceIn(0f, max(0f, width - w))
    }

    /** Only the first and the last (a card): both when they fit side by side, else the first. */
    fun endIndices(n: Int, width: Float, first: Float, last: Float, gap: Float): List<Int> = when {
        n <= 0 -> emptyList()
        n == 1 -> listOf(0)
        first + last + gap <= width -> listOf(0, n - 1)
        else -> listOf(0)
    }

    /** The label under [x] px of a plot [width] px wide with [n] equal slots. */
    fun indexAt(x: Float, width: Float, n: Int): Int = if (n <= 0 || width <= 0f) 0 else floor(x / width * n).toInt().coerceIn(0, n - 1)
}

/**
 * A bar or line chart. [compact] (a card) is short and writes no scale, only the first and last labels; otherwise the
 * scale is on the right and the labels below are thinned so they never overlap. Tapping (or dragging across) picks a
 * label: its values show above the plot; tapping it again lets go. More than one series gets a legend.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun JChart(data: JChartData, modifier: Modifier = Modifier, compact: Boolean = false, title: String? = null, detail: String? = null) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val n = data.labels.size
    var picked by remember(data) { mutableStateOf<Int?>(null) }
    val colors = data.series.mapIndexed { index, s -> s.tone?.let { palette.tone(it).dot } ?: palette.series(index) }
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(if (compact) 4.dp else 6.dp)) {
        if (title != null) Text(title, style = if (compact) type.caption else type.headline, color = if (compact) palette.ink2 else palette.ink,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (data.series.size > 1) FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            data.series.forEachIndexed { index, s -> Swatch(colors[index], s.name.orEmpty(), null) }
        }
        val index = picked?.takeIf { it < n }
        if (index != null) FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(data.labels[index], style = type.micro, color = palette.ink2, maxLines = 1)
            data.series.forEachIndexed { i, s ->
                Swatch(colors[i], s.name?.takeIf { data.series.size > 1 }, s.displays.getOrNull(index) ?: "—")
            }
        }
        val separator = stringResource(R.string.designsystem_separator)
        val seriesSeparator = stringResource(R.string.designsystem_series_separator)
        val labeled = stringResource(R.string.designsystem_labeled)
        val summary = remember(data, separator, seriesSeparator, labeled) {
            data.series.joinToString(seriesSeparator) { s ->
                val values = data.labels.indices.joinToString(separator) { "${data.labels[it]} ${s.displays.getOrNull(it).orEmpty()}" }
                s.name?.let { labeled.format(it, values) } ?: values
            }
        }
        ChartPlot(data, colors, compact, index, onPick = { picked = it },
            Modifier.fillMaxWidth().height(if (compact) 72.dp else 180.dp).semantics { contentDescription = summary })
        if (detail != null) Text(detail, style = type.caption, color = palette.ink3)
    }
}

@Composable
private fun Swatch(color: Color, name: String?, value: String?) {
    val palette = PersonaTheme.palette
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(8.dp).background(color, PersonaTheme.shapes.controlShape))
        if (name != null) {
            Spacer(Modifier.width(4.dp))
            Text(name, style = PersonaTheme.type.micro, color = palette.ink2, maxLines = 1)
        }
        if (value != null) {
            Spacer(Modifier.width(4.dp))
            Text(value, style = PersonaTheme.type.micro, color = palette.ink, maxLines = 1)
        }
    }
}

@Composable
private fun ChartPlot(data: JChartData, colors: List<Color>, compact: Boolean, picked: Int?, onPick: (Int?) -> Unit, modifier: Modifier) {
    val palette = PersonaTheme.palette
    val measurer = rememberTextMeasurer()
    val textStyle = PersonaTheme.type.micro.copy(color = palette.ink3)
    val n = data.labels.size
    val (low, high) = remember(data) { ChartMath.range(data) }
    val ticks = remember(low, high) { if (compact) listOf(low, high) else ChartMath.ticks(low, high) }
    val top = ticks.max()
    val bottom = ticks.min()
    // The scale's numbers (not on a card) and every label, measured once per chart.
    val scale = remember(data, ticks, compact, textStyle) {
        if (compact) emptyList() else ticks.map { measurer.measure(data.axis(it), textStyle) }
    }
    val labels: List<TextLayoutResult> = remember(data, textStyle) { data.labels.map { measurer.measure(it, textStyle, maxLines = 1) } }
    val currentPick by rememberUpdatedState(picked)
    val pick by rememberUpdatedState(onPick)
    val gutter = (scale.maxOfOrNull { it.size.width } ?: 0).toFloat()
    Canvas(modifier
        .pointerInput(n, gutter) {
            detectTapGestures { offset ->
                val width = size.width - (if (gutter > 0) gutter + 8.dp.toPx() else 0f)
                val i = ChartMath.indexAt(offset.x, width, n)
                pick(if (offset.x > width || currentPick == i) null else i)
            }
        }
        .pointerInput(n, gutter) {
            val width = { size.width - (if (gutter > 0) gutter + 8.dp.toPx() else 0f) }
            detectHorizontalDragGestures(onDragEnd = {}) { change, _ ->
                change.consume()
                pick(ChartMath.indexAt(change.position.x, width(), n))
            }
        }) {
        val gap = 8.dp.toPx()
        val labelHeight = labels.maxOfOrNull { it.size.height }?.toFloat() ?: 0f
        val plotWidth = (size.width - (if (gutter > 0) gutter + gap else 0f)).coerceAtLeast(1f)
        val plotHeight = (size.height - labelHeight - 6.dp.toPx()).coerceAtLeast(1f)
        val slot = plotWidth / max(1, n)
        fun y(v: Double) = (plotHeight * (1 - (v - bottom) / (top - bottom))).toFloat()
        fun cx(i: Int) = slot * i + slot / 2
        // Grid lines and the scale on the right; a card has only the zero line.
        if (!compact) ticks.forEachIndexed { k, tick ->
            val yy = y(tick)
            drawLine(if (tick == 0.0) palette.ink3.copy(alpha = 0.5f) else palette.line, Offset(0f, yy), Offset(plotWidth, yy), 1.dp.toPx())
            val label = scale[k]
            drawText(label, topLeft = Offset(plotWidth + gap, (yy - label.size.height / 2).coerceIn(0f, max(0f, plotHeight - label.size.height))))
        } else drawLine(palette.line, Offset(0f, y(0.0)), Offset(plotWidth, y(0.0)), 1.dp.toPx())
        // The labels under the plot: thinned on a page, the first and last on a card.
        val widths = labels.map { it.size.width.toFloat() }
        val shown = if (compact) ChartMath.endIndices(n, plotWidth, widths.firstOrNull() ?: 0f, widths.lastOrNull() ?: 0f, gap)
        else ChartMath.labelIndices(widths, plotWidth, gap)
        shown.forEach { i ->
            val w = widths[i]
            val left = when {
                compact && i == 0 -> 0f
                compact && i == n - 1 -> plotWidth - w
                else -> ChartMath.labelLeft(i, w, plotWidth, n)
            }.coerceIn(0f, max(0f, plotWidth - w))
            drawText(labels[i], topLeft = Offset(left, plotHeight + 6.dp.toPx()))
        }
        val dim = picked != null
        when (data.kind) {
            JChartKind.Bar -> {
                val group = min(slot * 0.72f, 28.dp.toPx() * (if (data.stacked) 1 else data.series.size))
                val corner = CornerRadius(min(3.dp.toPx(), group / 6), min(3.dp.toPx(), group / 6))
                for (i in 0 until n) {
                    val alpha = if (dim && i != picked) 0.35f else 1f
                    if (data.stacked && data.series.size > 1) {
                        var up = 0.0
                        var down = 0.0
                        data.series.forEachIndexed { k, s ->
                            val v = s.values.getOrNull(i)?.takeIf(Double::isFinite) ?: return@forEachIndexed
                            val (from, to) = if (v >= 0) (up to up + v).also { up += v } else (down to down + v).also { down += v }
                            val y1 = y(to); val y0 = y(from)
                            drawRect(colors[k].copy(alpha = alpha), Offset(cx(i) - group / 2, min(y0, y1)), Size(group, abs(y0 - y1)))
                        }
                    } else {
                        val bar = group / data.series.size
                        data.series.forEachIndexed { k, s ->
                            val v = s.values.getOrNull(i)?.takeIf(Double::isFinite) ?: return@forEachIndexed
                            val y0 = y(0.0); val y1 = y(v)
                            val x = cx(i) - group / 2 + bar * k
                            val inset = if (data.series.size > 1) 1.dp.toPx() else 0f
                            drawRoundRect(colors[k].copy(alpha = alpha), Offset(x + inset / 2, min(y0, y1)), Size(bar - inset, max(1f, abs(y0 - y1))), corner)
                        }
                    }
                }
            }
            JChartKind.Line -> {
                if (picked != null) drawLine(palette.ink3, Offset(cx(picked), 0f), Offset(cx(picked), plotHeight), 1.dp.toPx())
                data.series.forEachIndexed { k, s ->
                    val points = (0 until n).mapNotNull { i -> s.values.getOrNull(i)?.takeIf(Double::isFinite)?.let { Offset(cx(i), y(it)) } }
                    if (points.isEmpty()) return@forEachIndexed
                    val line = Path().apply { points.forEachIndexed { i, p -> if (i == 0) moveTo(p.x, p.y) else lineTo(p.x, p.y) } }
                    if (data.series.size == 1 && points.size > 1) {
                        val area = Path().apply { addPath(line); lineTo(points.last().x, y(max(bottom, 0.0))); lineTo(points.first().x, y(max(bottom, 0.0))); close() }
                        drawPath(area, colors[k].copy(alpha = 0.1f))
                    }
                    drawPath(line, colors[k], style = Stroke((if (compact) 1.5.dp else 2.dp).toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
                    if (points.size == 1 || (!compact && n <= 24)) points.forEach { drawCircle(colors[k], 2.5.dp.toPx(), it) }
                    picked?.let { i -> s.values.getOrNull(i)?.takeIf(Double::isFinite)?.let { v ->
                        drawCircle(palette.background, 5.dp.toPx(), Offset(cx(i), y(v)))
                        drawCircle(colors[k], 3.5.dp.toPx(), Offset(cx(i), y(v)))
                    } }
                }
            }
        }
    }
}

/** One number of a [JStatGrid]: [detail] is coloured by [tone] (neutral is the quiet grey). */
data class JStat(val label: String, val value: String, val detail: String? = null, val tone: Tone = Tone.Neutral)

/**
 * A few numbers side by side: two or three to a row by the width (three where three fit, and four as two rows of
 * two), each a label, the number and a detail line.
 */
@Composable
fun JStatGrid(stats: List<JStat>, modifier: Modifier = Modifier) {
    if (stats.isEmpty()) return
    BoxWithConstraints(modifier.fillMaxWidth()) {
        val columns = statColumns(stats.size, maxWidth.value)
        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
            stats.chunked(columns).forEach { row ->
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    row.forEach { StatCell(it, Modifier.weight(1f)) }
                    repeat(columns - row.size) { Spacer(Modifier.weight(1f)) }
                }
            }
        }
    }
}

/** How many numbers go on a row of [width] dp: all of one or two, three where they fit, four as two and two. */
fun statColumns(count: Int, width: Float): Int = when {
    count <= 2 -> max(1, count)
    count == 4 -> 2
    width >= 300f -> 3
    else -> 2
}

@Composable
private fun StatCell(stat: JStat, modifier: Modifier) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    Column(modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(stat.label, style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(stat.value, style = type.number, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
        stat.detail?.takeIf { it.isNotBlank() }?.let {
            Text(it, style = type.caption, color = if (stat.tone == Tone.Neutral) palette.ink3 else palette.tone(stat.tone).text, maxLines = 2,
                overflow = TextOverflow.Ellipsis)
        }
    }
}
