package persona.core.designsystem

import android.graphics.Bitmap
import android.graphics.Paint
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.border
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.drag
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlin.math.abs

/** One colour of the image editor's marks. */
@Immutable
class MarkupColor internal constructor(val name: String, val color: Color)

/**
 * The image editor's six mark colours. Fixed on purpose, not taken from a style: a mark has to read the same in every
 * style, on the dark editor and in the picture that leaves the phone (the red, yellow, green and blue of iOS's system
 * colours, white, black — as the iOS editor).
 */
object MarkupColors {
    val Red = MarkupColor("红色", Color(0xFFFF3B30))
    val Yellow = MarkupColor("黄色", Color(0xFFFFCC00))
    val Green = MarkupColor("绿色", Color(0xFF34C759))
    val Blue = MarkupColor("蓝色", Color(0xFF007AFF))
    val White = MarkupColor("白色", Color(0xFFFFFFFF))
    val Black = MarkupColor("黑色", Color(0xFF000000))
    val all: List<MarkupColor> = listOf(Red, Yellow, Green, Blue, White, Black)
}

/**
 * One mark in picture pixels. A box is its first and last point; a pen stroke is all of them. [width] is the stroke as
 * it was on screen, in picture pixels ([ImageMath.strokeInImage]), so the result looks like what the user drew.
 */
@Immutable
internal data class EditorMark(val box: Boolean, val color: Color, val width: Float, val points: List<Offset>)

private enum class MarkupTool(val label: String) { Box("框选"), Pen("画笔") }

/** The stroke of a mark on screen. */
private val MarkStroke = 3.5.dp

/**
 * Marks up [source] before it goes to an Agent, full screen in the dark appearance: 「取消 · 编辑图片 · 完成」, a box or
 * a pen, six colours, undo and clear. [onDone] gets the picture with the marks drawn in at [source]'s own resolution
 * ([source] itself when nothing was drawn); after that the editor waits, 完成 spinning, until its caller removes it.
 * Back and 取消 call [onCancel]. Pass a software bitmap ([decodePicture] with `software = true`).
 */
@Composable
fun ImageEditor(source: Bitmap, onCancel: () -> Unit, onDone: (Bitmap) -> Unit) {
    var finishing by remember(source) { mutableStateOf(false) }
    AlwaysDark {
        JFullScreen({ if (!finishing) onCancel() }) {
            CompositionLocalProvider(LocalReadingInset provides 0.dp) {
                EditorBody(source, finishing, { finishing = it }, onCancel, onDone)
            }
        }
    }
}

@Composable
private fun EditorBody(source: Bitmap, finishing: Boolean, onFinishing: (Boolean) -> Unit, onCancel: () -> Unit, onDone: (Bitmap) -> Unit) {
    val palette = PersonaTheme.palette
    val density = LocalDensity.current
    var tool by remember { mutableStateOf(MarkupTool.Box) }
    var color by remember { mutableStateOf(MarkupColors.Red) }
    val marks = remember(source) { mutableStateListOf<EditorMark>() }
    var current by remember(source) { mutableStateOf<EditorMark?>(null) }
    var box by remember { mutableStateOf(IntSize.Zero) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val picture = remember(source) { source.asImageBitmap() }
    val imageSize = remember(source) { Size(source.width.toFloat(), source.height.toFloat()) }
    val fit = ImageMath.fitRect(imageSize, Size(box.width.toFloat(), box.height.toFloat()))
    val stroke = with(density) { MarkStroke.toPx() }
    val latestTool by rememberUpdatedState(tool)
    val latestColor by rememberUpdatedState(color)
    val latestFit by rememberUpdatedState(fit)
    val latestFinishing by rememberUpdatedState(finishing)
    val done by rememberUpdatedState(onDone)

    fun finish() {
        if (finishing) return
        onFinishing(true)
        failed = false
        val drawn = marks.toList()
        scope.launch {
            val result = try {
                if (drawn.isEmpty()) source else withContext(Dispatchers.Default) { renderMarks(source, drawn) }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Throwable) {
                null
            }
            if (result == null) { failed = true; onFinishing(false) } else done(result)
        }
    }

    Column(Modifier.fillMaxSize()) {
        Box(Modifier.fillMaxWidth().statusBarsPadding().height(56.dp).padding(horizontal = Tokens.Space.xs)) {
            JTextButton("取消", onCancel, Modifier.align(Alignment.CenterStart), enabled = !finishing)
            Text(
                if (failed) "没能画进图片，请重试" else "编辑图片",
                Modifier.align(Alignment.Center).widthIn(max = 220.dp),
                style = PersonaTheme.type.title, color = if (failed) palette.danger.text else palette.ink,
                maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            JButton(
                "完成", ::finish, Modifier.align(Alignment.CenterEnd).padding(end = Tokens.Space.s),
                size = JButtonSize.Small, loading = finishing,
            )
        }
        Box(
            Modifier.weight(1f).fillMaxWidth().onSizeChanged { box = it }
                .pointerInput(source) {
                    awaitEachGesture {
                        val down = awaitFirstDown()
                        val area = latestFit
                        if (area.width <= 0f || latestFinishing) return@awaitEachGesture
                        down.consume()
                        val start = ImageMath.toImage(down.position, area, imageSize)
                        current = EditorMark(latestTool == MarkupTool.Box, latestColor.color, ImageMath.strokeInImage(stroke, area, imageSize), listOf(start))
                        val lifted = drag(down.id) { change ->
                            val point = ImageMath.toImage(change.position, area, imageSize)
                            current = current?.let { mark -> if (mark.box) mark.copy(points = listOf(mark.points.first(), point)) else mark.copy(points = mark.points + point) }
                            change.consume()
                        }
                        val mark = current
                        current = null
                        if (lifted && mark != null && mark.points.size > 1 && (!mark.box || mark.points.first() != mark.points.last())) marks += mark
                    }
                },
        ) {
            Image(picture, "要编辑的图片", Modifier.fillMaxSize(), contentScale = ContentScale.Fit)
            Canvas(Modifier.fillMaxSize()) {
                val area = latestFit
                if (area.width > 0f) {
                    val scale = area.width / imageSize.width
                    marks.forEach { drawMark(it, area, scale) }
                    current?.let { drawMark(it, area, scale) }
                }
            }
        }
        Column(
            Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.m),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.m),
        ) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) {
                MarkupColors.all.forEach { option -> ColorSwatch(option, option == color) { color = option } }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                JSegmented(MarkupTool.entries.map { JSegment(it.label) }, tool.ordinal, { tool = MarkupTool.entries[it] })
                Spacer(Modifier.weight(1f))
                JIconButton(PhIcons.ArrowUUpLeft, "撤销", { marks.removeLastOrNull() }, enabled = marks.isNotEmpty() && !finishing)
                JIconButton(PhIcons.Trash, "清除", { marks.clear() }, enabled = marks.isNotEmpty() && !finishing)
            }
        }
    }
}

/** A mark on screen: [area] is where the picture is drawn, [scale] picture pixels to screen pixels. */
private fun DrawScope.drawMark(mark: EditorMark, area: Rect, scale: Float) {
    val points = mark.points.map { Offset(area.left + it.x * scale, area.top + it.y * scale) }
    val first = points.firstOrNull() ?: return
    val last = points.last()
    val style = Stroke(width = mark.width * scale, cap = StrokeCap.Round, join = StrokeJoin.Round)
    if (mark.box) {
        drawRect(
            mark.color, Offset(minOf(first.x, last.x), minOf(first.y, last.y)),
            Size(abs(last.x - first.x), abs(last.y - first.y)), style = style,
        )
    } else {
        val path = Path().apply { moveTo(first.x, first.y); points.drop(1).forEach { lineTo(it.x, it.y) } }
        drawPath(path, mark.color, style = style)
    }
}

/** A colour to pick: a ring around the chosen one, a thin edge on every one so black reads on the dark bar. */
@Composable
private fun ColorSwatch(option: MarkupColor, chosen: Boolean, onClick: () -> Unit) {
    val palette = PersonaTheme.palette
    Box(
        Modifier.size(44.dp).clip(CircleShape)
            .clickable(role = Role.RadioButton, onClick = onClick)
            .semantics { contentDescription = option.name; selected = chosen },
        contentAlignment = Alignment.Center,
    ) {
        Box(
            // A 0.dp border is a hairline in Compose, so the ring is added only when chosen.
            Modifier.size(34.dp).then(if (chosen) Modifier.border(2.5.dp, palette.ink, CircleShape) else Modifier).padding(4.dp)
                .clip(CircleShape).background(option.color).border(1.dp, palette.ink3, CircleShape),
        )
    }
}

/** [marks] drawn into a copy of [source], at its resolution; [source] itself when there are none. Blocking. */
internal fun renderMarks(source: Bitmap, marks: List<EditorMark>): Bitmap {
    if (marks.isEmpty()) return source
    val out = checkNotNull(source.copy(Bitmap.Config.ARGB_8888, true)) { "No memory for the marked-up picture" }
    val canvas = android.graphics.Canvas(out)
    val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeCap = Paint.Cap.ROUND
        strokeJoin = Paint.Join.ROUND
    }
    for (mark in marks) {
        val first = mark.points.firstOrNull() ?: continue
        val last = mark.points.last()
        paint.color = mark.color.toArgb()
        paint.strokeWidth = mark.width
        if (mark.box) {
            canvas.drawRect(minOf(first.x, last.x), minOf(first.y, last.y), maxOf(first.x, last.x), maxOf(first.y, last.y), paint)
        } else {
            val path = android.graphics.Path().apply {
                moveTo(first.x, first.y)
                mark.points.drop(1).forEach { lineTo(it.x, it.y) }
            }
            canvas.drawPath(path, paint)
        }
    }
    return out
}
