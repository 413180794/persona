package persona.feature.machines

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectDragGesturesAfterLongPress
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.*
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.clipRect
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.dp
import persona.core.contracts.MetricPoint
import persona.core.designsystem.JCard
import persona.core.designsystem.PersonaPalette
import persona.core.designsystem.PersonaTheme
import kotlin.math.max

internal enum class MetricKind(val label: String) {
    Cpu("CPU"), Memory("内存"), Rx("下行"), Tx("上行"), Load("负载");
    fun color(palette: PersonaPalette) = when (this) {
        Cpu -> palette.working.dot; Memory -> palette.done.dot
        Rx -> palette.needsYou.dot; Tx -> palette.accent; Load -> palette.ink2
    }
    fun value(point: MetricPoint): Double? = when (this) {
        Cpu -> point.cpu; Memory -> point.mem; Rx -> point.rx; Tx -> point.tx; Load -> point.load
    }
    fun format(value: Double?): String = when (this) {
        Cpu -> MetricFormat.percent(value)
        Memory -> MetricFormat.bytes(value)
        Rx, Tx -> MetricFormat.rate(value)
        Load -> MetricFormat.number(value)
    }
}

@Composable
internal fun MetricChart(
    title: String, points: List<MetricPoint>, kinds: List<MetricKind>, nowValues: List<Double?>,
    end: Long, span: Long, step: Int, selection: Long?, onSelect: (Long?) -> Unit,
    reference: Double? = null,
) {
    val picked = remember(points, selection, step) { selection?.let { selectedPoint(points, it, step) } }
    JCard(Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
        Text(title, style = PersonaTheme.type.title, color = PersonaTheme.palette.ink)
        Text(selection?.let(MetricFormat::time) ?: "现在", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
        kinds.forEachIndexed { index, kind ->
            val value = if (selection != null) picked?.let(kind::value) else nowValues.getOrNull(index)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Box(Modifier.padding(top = 6.dp).size(8.dp).background(kind.color(PersonaTheme.palette), PersonaTheme.shapes.controlShape))
                Text("${kind.label} ${kind.format(value)}", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink)
            }
        }
        if (selection != null && picked == null) Text("这个时段没有采样", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
        if (reference != null) Text("虚线：${reference.toInt()} 核", style = PersonaTheme.type.micro,
            color = PersonaTheme.palette.ink3)
        val summary = kinds.mapIndexed { index, kind ->
            val peak = points.filter { !it.gap && it.t in (end - span)..end }.mapNotNull(kind::value).maxOrNull()
            "${kind.label}，现在 ${kind.format(nowValues.getOrNull(index))}，最高 ${kind.format(peak)}"
        }.joinToString("；")
        Plot(points, kinds, end, span, step, Modifier.fillMaxWidth().height(180.dp).semantics { contentDescription = summary },
            axes = true, reference = reference, selection = selection, onSelect = onSelect)
        if (points.none { !it.gap && it.t in (end - span)..end && kinds.any { kind -> kind.value(it) != null } }) {
            Text("这段时间还没有监控数据", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
        }
    }
}

@Composable
internal fun Plot(
    points: List<MetricPoint>, kinds: List<MetricKind>, end: Long, span: Long, step: Int,
    modifier: Modifier = Modifier, axes: Boolean = false, reference: Double? = null,
    selection: Long? = null, onSelect: (Long?) -> Unit = {},
) {
    val runs = remember(points, kinds, step) { kinds.map { kind -> segments(points, step, kind::value) } }
    val start = end - span
    val maximum = if (kinds == listOf(MetricKind.Cpu)) 100.0 else max(1.0,
        max(reference ?: 0.0, runs.flatten().flatten().filter { it.t in start..end }.maxOfOrNull { it.value } ?: 0.0) * 1.1)
    val palette = PersonaTheme.palette
    val axis = PersonaTheme.palette.ink3
    val grid = PersonaTheme.palette.line
    val measure = rememberTextMeasurer()
    val textStyle = PersonaTheme.type.micro.copy(color = axis)
    val labels = if (axes) listOf(maximum, maximum / 2, 0.0).map { measure.measure(kinds.first().format(it), textStyle) } else emptyList()
    val right = labels.maxOfOrNull { it.size.width }?.toFloat()?.plus(10f) ?: 0f
    val bottom = if (axes) measure.measure("00:00", textStyle).size.height.toFloat() + 10f else 0f
    val latestSelect by rememberUpdatedState(onSelect)
    val input = if (!axes) Modifier else Modifier.pointerInput(start, span, right) {
        fun pick(x: Float) {
            val width = (size.width - right).coerceAtLeast(1f)
            latestSelect(start + (x / width).coerceIn(0f, 1f).times(span).toLong())
        }
        detectDragGesturesAfterLongPress(onDragStart = { pick(it.x) }, onDragEnd = { latestSelect(null) },
            onDragCancel = { latestSelect(null) }) { change, _ -> change.consume(); pick(change.position.x) }
    }
    Canvas(modifier.then(input)) {
        val width = (size.width - right).coerceAtLeast(1f)
        val height = (size.height - bottom).coerceAtLeast(1f)
        fun x(t: Long) = ((t - start).toDouble() / span * width).toFloat()
        fun y(v: Double) = (height * (1 - v / maximum)).toFloat()
        if (axes) {
            labels.forEachIndexed { index, label ->
                val yy = height * index / 2
                drawLine(grid, Offset(0f, yy), Offset(width, yy), strokeWidth = 1f)
                drawText(label, topLeft = Offset(width + 8f, (yy - label.size.height / 2).coerceIn(0f, max(0f, height - label.size.height))))
            }
            // Text is measured with an explicit neutral color, independent of the app accent.
            repeat(3) { index ->
                val label = measure.measure(MetricFormat.time(start + span * index / 2), textStyle)
                drawText(label, topLeft = Offset((width * index / 2 - label.size.width / 2).coerceIn(0f, max(0f, width - label.size.width)), height + 8f))
            }
        }
        // Clip the plot itself, leaving the neutral axes outside the clip.
        clipRect(0f, 0f, width, height) {
            reference?.let { drawLine(axis, Offset(0f, y(it)), Offset(width, y(it)), strokeWidth = 1.dp.toPx(),
                pathEffect = PathEffect.dashPathEffect(floatArrayOf(5.dp.toPx(), 4.dp.toPx()))) }
            runs.forEachIndexed { index, series ->
                val color = kinds[index].color(palette)
                series.forEach { run ->
                    val line = Path().apply {
                        run.forEachIndexed { i, point -> if (i == 0) moveTo(x(point.t), y(point.value)) else lineTo(x(point.t), y(point.value)) }
                    }
                    if (kinds.size == 1) {
                        val area = Path().apply { addPath(line); lineTo(x(run.last().t), height); lineTo(x(run.first().t), height); close() }
                        drawPath(area, color.copy(alpha = .1f))
                    }
                    drawPath(line, color, style = Stroke((if (axes) 2.dp else 1.5.dp).toPx(), cap = StrokeCap.Round))
                    if (run.size == 1) drawCircle(color, 2.dp.toPx(), Offset(x(run[0].t), y(run[0].value)))
                }
            }
            selection?.let { time ->
                drawLine(axis, Offset(x(time), 0f), Offset(x(time), height), 1.dp.toPx())
                selectedPoint(points, time, step)?.let { point ->
                    kinds.forEach { kind -> kind.value(point)?.takeIf(Double::isFinite)?.let {
                        drawCircle(kind.color(palette), 4.dp.toPx(), Offset(x(point.t), y(it)))
                    } }
                }
            }
        }
    }
}
