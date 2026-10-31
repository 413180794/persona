package jarvis.android.ui.media

import android.graphics.Bitmap
import android.graphics.Paint
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Undo
import androidx.compose.material.icons.filled.CropSquare
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Gesture
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
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
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties

/** One mark on the picture, in image pixels; a box is its first and last point. */
data class EditorMark(val box: Boolean, val color: Color, val points: List<Offset>)

/** Where an image of [image] size sits, fitted and centered, in a [box]. */
fun fitRect(image: Size, box: Size): Rect {
    if (image.width <= 0f || image.height <= 0f) return Rect.Zero
    val s = minOf(box.width / image.width, box.height / image.height)
    val w = image.width * s
    val h = image.height * s
    return Rect(Offset((box.width - w) / 2, (box.height - h) / 2), Size(w, h))
}

/** A point on screen to image pixels, clamped to the picture. */
fun toImage(p: Offset, fit: Rect, image: Size): Offset {
    if (fit.width <= 0f) return Offset.Zero
    val s = image.width / fit.width
    return Offset(((p.x - fit.left) * s).coerceIn(0f, image.width), ((p.y - fit.top) * s).coerceIn(0f, image.height))
}

private val editorColors = listOf(
    "红色" to Color(0xFFFF3B30), "黄色" to Color(0xFFFFCC00), "绿色" to Color(0xFF34C759),
    "蓝色" to Color(0xFF007AFF), "白色" to Color.White, "黑色" to Color.Black,
)

/**
 * Marks up a picture before it goes to the agent (iOS `ImageEditor`): a box or a pen, six colors, undo and clear,
 * full screen. [onDone] gets the picture with the marks drawn in at full resolution (the same bitmap when
 * nothing was drawn).
 */
@Composable
fun ImageEditor(bitmap: Bitmap, onCancel: () -> Unit, onDone: (Bitmap) -> Unit) {
    var box by remember { mutableStateOf(true) }
    var color by remember { mutableStateOf(editorColors[0].second) }
    val marks = remember { mutableStateListOf<EditorMark>() }
    var current by remember { mutableStateOf<EditorMark?>(null) }
    val imgSize = Size(bitmap.width.toFloat(), bitmap.height.toFloat())
    val bmp = remember(bitmap) { bitmap.asImageBitmap() }

    // Over the viewer (its caller stacks it on top), not a Dialog: a dialog window stops at the system bars.
    Box(Modifier.fillMaxSize()) {
        BackHandler(onBack = onCancel)
        Column(Modifier.fillMaxSize().background(Color.Black)) {
            Row(
                Modifier.fillMaxWidth().statusBarsPadding().height(56.dp).padding(horizontal = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                TextButton(onClick = onCancel) { Text("取消", color = Color.White, fontSize = 16.sp, maxLines = 1) }
                Spacer(Modifier.weight(1f))
                Text("编辑图片", color = Color.White, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
                Spacer(Modifier.weight(1f))
                TextButton(onClick = { onDone(renderMarks(bitmap, marks)) }) {
                    Text("完成", color = Color.White, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
                }
            }
            BoxWithConstraints(Modifier.weight(1f).fillMaxWidth()) {
                val boxSize = Size(constraints.maxWidth.toFloat(), constraints.maxHeight.toFloat())
                val fit = fitRect(imgSize, boxSize)
                Image(bmp, contentDescription = "要编辑的图片", contentScale = ContentScale.Fit, modifier = Modifier.fillMaxSize())
                Canvas(
                    Modifier.fillMaxSize().pointerInput(box, color, fit) {
                        detectDragGestures(
                            onDragStart = { p -> current = EditorMark(box, color, listOf(toImage(p, fit, imgSize))) },
                            onDrag = { change, _ ->
                                change.consume()
                                current = current?.let { it.copy(points = it.points + toImage(change.position, fit, imgSize)) }
                            },
                            onDragEnd = {
                                current?.takeIf { it.points.size > 1 }?.let { marks += it }
                                current = null
                            },
                            onDragCancel = { current = null },
                        )
                    },
                ) {
                    val scale = if (imgSize.width > 0) fit.width / imgSize.width else 1f
                    val stroke = Stroke(width = 3.5.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round)
                    for (m in marks + listOfNotNull(current)) {
                        val pts = m.points.map { Offset(fit.left + it.x * scale, fit.top + it.y * scale) }
                        val first = pts.firstOrNull() ?: continue
                        val last = pts.last()
                        if (m.box) {
                            drawRect(
                                m.color, Offset(minOf(first.x, last.x), minOf(first.y, last.y)),
                                Size(kotlin.math.abs(last.x - first.x), kotlin.math.abs(last.y - first.y)), style = stroke,
                            )
                        } else {
                            val path = Path().apply { moveTo(first.x, first.y); pts.drop(1).forEach { lineTo(it.x, it.y) } }
                            drawPath(path, m.color, style = stroke)
                        }
                    }
                }
            }
            Column(
                Modifier.fillMaxWidth().background(Color.Black).navigationBarsPadding().padding(horizontal = 16.dp, vertical = 10.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(14.dp, Alignment.CenterHorizontally)) {
                    editorColors.forEach { (name, c) ->
                        val on = c == color
                        Box(
                            Modifier.size(30.dp).clip(CircleShape).background(c)
                                .border(if (on) 3.dp else 1.dp, Color.White.copy(alpha = if (on) 1f else 0.4f), CircleShape)
                                .semantics { contentDescription = name; selected = on }
                                .clickable { color = c },
                        )
                    }
                }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    val chipColors = FilterChipDefaults.filterChipColors(
                        labelColor = Color.White, iconColor = Color.White,
                        selectedContainerColor = Color.White.copy(alpha = 0.25f), selectedLabelColor = Color.White,
                        selectedLeadingIconColor = Color.White,
                    )
                    FilterChip(
                        selected = box, onClick = { box = true }, colors = chipColors,
                        leadingIcon = { Icon(Icons.Default.CropSquare, null, Modifier.size(18.dp)) },
                        label = { Text("框选", maxLines = 1) },
                    )
                    FilterChip(
                        selected = !box, onClick = { box = false }, colors = chipColors,
                        leadingIcon = { Icon(Icons.Default.Gesture, null, Modifier.size(18.dp)) },
                        label = { Text("画笔", maxLines = 1) },
                    )
                    Spacer(Modifier.weight(1f))
                    val iconColors = IconButtonDefaults.filledTonalIconButtonColors(
                        containerColor = Color.White.copy(alpha = 0.18f), contentColor = Color.White,
                        disabledContainerColor = Color.White.copy(alpha = 0.08f), disabledContentColor = Color.White.copy(alpha = 0.35f),
                    )
                    FilledTonalIconButton(onClick = { if (marks.isNotEmpty()) marks.removeAt(marks.lastIndex) }, enabled = marks.isNotEmpty(), colors = iconColors) {
                        Icon(Icons.AutoMirrored.Filled.Undo, contentDescription = "撤销")
                    }
                    Spacer(Modifier.width(2.dp))
                    FilledTonalIconButton(onClick = { marks.clear() }, enabled = marks.isNotEmpty(), colors = iconColors) {
                        Icon(Icons.Default.Delete, contentDescription = "清除")
                    }
                }
            }
        }
    }
}

/** Draws [marks] into a copy of [image] (stroke width scaled to the picture, as on screen). */
fun renderMarks(image: Bitmap, marks: List<EditorMark>): Bitmap {
    if (marks.isEmpty()) return image
    val out = image.copy(Bitmap.Config.ARGB_8888, true)
    val canvas = android.graphics.Canvas(out)
    val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeCap = Paint.Cap.ROUND
        strokeJoin = Paint.Join.ROUND
        strokeWidth = maxOf(image.width, image.height) / 220f
    }
    for (m in marks) {
        val first = m.points.firstOrNull() ?: continue
        val last = m.points.last()
        paint.color = m.color.toArgb()
        if (m.box) {
            canvas.drawRect(minOf(first.x, last.x), minOf(first.y, last.y), maxOf(first.x, last.x), maxOf(first.y, last.y), paint)
        } else {
            val path = android.graphics.Path().apply { moveTo(first.x, first.y); m.points.drop(1).forEach { lineTo(it.x, it.y) } }
            canvas.drawPath(path, paint)
        }
    }
    return out
}
