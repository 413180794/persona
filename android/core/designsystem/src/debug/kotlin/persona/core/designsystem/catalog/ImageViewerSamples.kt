package persona.core.designsystem.catalog

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.unit.dp
import persona.core.designsystem.ImageEditor
import persona.core.designsystem.ImageViewer
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.ViewerAction
import persona.core.designsystem.ViewerImage
import persona.core.designsystem.decodePicture
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import kotlin.math.max
import kotlin.math.min

/**
 * The catalog's "Image viewer" section: the image viewer on three pictures drawn here (the third never loads, for the error
 * state; each takes a moment, for the spinner) and the markup editor on the first. Nothing leaves the page: the
 * viewer's actions only say what they would do.
 */
@Composable
internal fun ColumnScope.ImageViewerSamples(onFeedback: (String) -> Unit) {
    val palette = PersonaTheme.palette
    val scope = rememberCoroutineScope()
    var viewer by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf<Bitmap?>(null) }
    val colors = SampleColors(
        palette.working.dot.toArgb(), palette.needsYou.dot.toArgb(), palette.done.dot.toArgb(),
        palette.surfaceOpaque.toArgb(), palette.ink.toArgb(),
    )
    val images = remember(colors) {
        listOf(
            ViewerImage("catalog-wide", "示例图片：横图", 1600, 1000) { delay(SampleDelayMs); samplePng(1600, 1000, colors, wide = true) },
            ViewerImage("catalog-tall", "示例图片：竖图", 900, 1600) { delay(SampleDelayMs); samplePng(900, 1600, colors, wide = false) },
            ViewerImage("catalog-broken", "示例图片：读不出来", 1200, 900) { delay(SampleDelayMs); error("Sample picture that never loads") },
        )
    }
    val actions = listOf(
        ViewerAction("保存", PhIcons.ArrowDown) { index, bytes -> "示例：不会真的保存（第 ${index + 1} 张，${bytes.size / 1024} KB）" },
        ViewerAction("编辑", PhIcons.PencilSimple) { _, bytes ->
            editing = withContext(Dispatchers.Default) { decodePicture(bytes, 1600, atMost = false, software = true) }
            null
        },
        ViewerAction("分享", PhIcons.ShareNetwork) { _, _ -> "示例：不会真的分享" },
        ViewerAction("拷贝", PhIcons.Copy) { _, _ -> "示例：不会真的拷贝" },
    )
    Text("图片都在本机画出来，第三张故意读不出来；按钮不会真的保存、分享或拷贝。", style = PersonaTheme.type.caption, color = palette.ink3)
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        JButton("看图（3 张）", { viewer = true }, Modifier.weight(1f), icon = PhIcons.Image)
        JButton("标注编辑", {
            scope.launch { editing = withContext(Dispatchers.Default) { sampleBitmap(1600, 1000, colors, wide = true) } }
        }, Modifier.weight(1f), kind = JButtonKind.Secondary, icon = PhIcons.PencilSimple)
    }
    if (viewer) ImageViewer(images, 0, onDismiss = { viewer = false }, actions = actions)
    editing?.let { source ->
        ImageEditor(source, onCancel = { editing = null }, onDone = { result ->
            editing = null
            onFeedback("编辑完成：${result.width}×${result.height}，标注已画进图片")
        })
    }
}

private const val SampleDelayMs = 600L

/** The palette colours the sample pictures are painted with, as ARGB. */
private data class SampleColors(val sky: Int, val sun: Int, val field: Int, val paper: Int, val ink: Int)

private fun samplePng(width: Int, height: Int, colors: SampleColors, wide: Boolean): ByteArray {
    val bitmap = sampleBitmap(width, height, colors, wide)
    return try {
        ByteArrayOutputStream().use { out ->
            check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, out))
            out.toByteArray()
        }
    } finally {
        bitmap.recycle()
    }
}

/** A made-up picture: a landscape (sky, sun, field) or a page of text lines, so zooming and marking have something to show. */
private fun sampleBitmap(width: Int, height: Int, colors: SampleColors, wide: Boolean): Bitmap {
    val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
    val canvas = Canvas(bitmap)
    val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    if (wide) {
        canvas.drawColor(colors.sky)
        paint.color = colors.sun
        canvas.drawCircle(width * 0.72f, height * 0.3f, min(width, height) * 0.16f, paint)
        paint.color = colors.field
        canvas.drawRect(0f, height * 0.68f, width.toFloat(), height.toFloat(), paint)
        paint.color = colors.ink
        paint.strokeWidth = max(width, height) / 200f
        for (i in 0 until 12) {
            val x = width * (i + 0.5f) / 12f
            canvas.drawLine(x, height * 0.68f, x + width / 40f, height * 0.62f, paint)
        }
    } else {
        canvas.drawColor(colors.paper)
        paint.color = colors.sky
        canvas.drawRect(0f, 0f, width.toFloat(), height * 0.12f, paint)
        paint.color = colors.ink
        val line = height / 28f
        for (i in 0 until 18) {
            val y = height * 0.18f + i * line
            val end = if (i % 4 == 3) width * 0.55f else width * 0.9f
            canvas.drawRect(width * 0.08f, y, end, y + line * 0.35f, paint)
        }
        paint.color = colors.sun
        canvas.drawRect(width * 0.08f, height * 0.86f, width * 0.6f, height * 0.93f, paint)
    }
    return bitmap
}
