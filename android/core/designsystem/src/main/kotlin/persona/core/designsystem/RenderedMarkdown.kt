package persona.core.designsystem

import androidx.compose.ui.res.stringResource
import android.graphics.Bitmap
import androidx.compose.foundation.Image
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.geometry.isUnspecified
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.painter.BitmapPainter
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.PlaceholderVerticalAlign
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import com.mikepenz.markdown.model.ImageData
import com.mikepenz.markdown.model.ImageTransformer
import com.mikepenz.markdown.model.ImageWidth
import com.mikepenz.markdown.model.PlaceholderConfig
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import persona.core.designsystem.MarkdownRenderer.Kind
import java.io.ByteArrayOutputStream
import kotlin.math.min

/** A fenced code block's info string (its language) and its code, from the block's raw text. */
data class Fence(val language: String, val code: String) {
    companion object {
        fun of(raw: String): Fence {
            val lines = raw.lines()
            val body = lines.drop(1)
            val closed = body.lastOrNull()?.trim()?.let { it.startsWith("```") || it.startsWith("~~~") } == true
            return Fence(lines.firstOrNull()?.trim()?.trimStart('`', '~')?.trim().orEmpty(),
                (if (closed) body.dropLast(1) else body).joinToString("\n"))
        }
    }
}

/** What a fenced block's language asks to be drawn: ```math (also latex / tex / katex) and ```mermaid; null for code. */
fun renderedFence(language: String): Kind? = when (language.trim().substringBefore(' ').lowercase()) {
    MathMarkdown.FENCE, "latex", "tex", "katex" -> Kind.DisplayMath
    "mermaid" -> Kind.Mermaid
    else -> null
}

/** The text size, in CSS px (dp with the font scale), formulas are drawn at: the body's; diagrams the caption's. */
@Composable
private fun renderPoints(kind: Kind, body: TextStyle): Float {
    val density = LocalDensity.current
    val style = if (kind == Kind.Mermaid) PersonaTheme.type.caption else body
    return with(density) { style.fontSize.toPx() / this.density }
}

private sealed interface Drawn {
    data object Loading : Drawn
    data class Picture(val bitmap: Bitmap) : Drawn
    data class Failed(val message: String) : Drawn
}

@Composable
private fun rememberDrawn(kind: Kind, source: String, points: Float): Drawn {
    val host = LocalView.current
    val dark = PersonaTheme.palette.dark
    val density = LocalDensity.current.density
    val cached = remember(kind, source, dark, points, density) { MarkdownRenderer.cached(kind, source, dark, points, density) }
    val nothing = stringResource(R.string.designsystem_render_nothing)
    // Keyed by what is drawn: a fence still streaming in, the other appearance or text size draws again.
    val drawn by produceState<Drawn>(cached?.let { Drawn.Picture(it) } ?: Drawn.Loading, kind, source, dark, points, density, host) {
        MarkdownRenderer.cached(kind, source, dark, points, density)?.let { value = Drawn.Picture(it); return@produceState }
        value = try {
            Drawn.Picture(MarkdownRenderer.image(host, kind, source, dark, points))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Drawn.Failed(e.message ?: nothing)
        }
    }
    return drawn
}

/**
 * A fenced block in Markdown ([raw], the block's text): ```math and ```mermaid drawn ([RenderedFence], [code] as the
 * fallback), anything else as [code]. One place for every Markdown surface's `codeFence` component.
 */
@Composable
fun MarkdownFence(raw: String, color: Color = PersonaTheme.palette.ink, body: TextStyle = PersonaTheme.type.body,
    code: @Composable (Fence) -> Unit) {
    val fence = remember(raw) { Fence.of(raw) }
    val kind = renderedFence(fence.language)
    if (kind != null) RenderedFence(kind, fence.code, color, body) { code(fence) } else code(fence)
}

/** [MathImages] for text in [color] at [body]'s size, kept while they stay the same. */
@Composable
fun rememberMathImages(color: Color = PersonaTheme.palette.ink, body: TextStyle = PersonaTheme.type.body,
    images: ImageTransformer? = null): MathImages = remember(color, body, images) { MathImages(color, body, images) }

/**
 * A ```math or ```mermaid fence: the picture once drawn (a formula centred, sideways when wider than the column, in the
 * text's colour; a diagram fitted to the column but not below 0.7×, then sideways, opening full screen on a tap); while
 * it draws, a spinner; when it cannot be drawn, [fallback] (the fence as code) with KaTeX's or Mermaid's words under it.
 */
@Composable
fun RenderedFence(kind: Kind, source: String, color: Color = PersonaTheme.palette.ink, body: TextStyle = PersonaTheme.type.body,
    fallback: @Composable () -> Unit) {
    when (val drawn = rememberDrawn(kind, source, renderPoints(kind, body))) {
        Drawn.Loading -> JLoading(Modifier.fillMaxWidth())
        is Drawn.Failed -> Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            fallback()
            Text(stringResource(R.string.designsystem_render_failed, drawn.message), style = PersonaTheme.type.caption, color = PersonaTheme.palette.cautionText, maxLines = 2)
        }
        is Drawn.Picture -> if (kind == Kind.Mermaid) Diagram(drawn.bitmap, source) else Formula(drawn.bitmap, source, color)
    }
}

@Composable
private fun Formula(bitmap: Bitmap, tex: String, color: Color) {
    val density = LocalDensity.current
    val image = remember(bitmap) { bitmap.asImageBitmap() }
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        val column = maxWidth
        Box(Modifier.horizontalScroll(rememberScrollState())) {
            Box(Modifier.widthIn(min = column), contentAlignment = Alignment.Center) {
                Image(image, stringResource(R.string.designsystem_render_formula, tex), Modifier.testTag("rendered-math")
                    .size(with(density) { bitmap.width.toDp() }, with(density) { bitmap.height.toDp() }),
                    colorFilter = ColorFilter.tint(color))
            }
        }
    }
}

/** Diagrams are drawn at the caption's size; at 0.7 of it they are still readable. */
private const val MIN_DIAGRAM_SCALE = 0.7f

@Composable
private fun Diagram(bitmap: Bitmap, source: String) {
    val density = LocalDensity.current
    val image = remember(bitmap) { bitmap.asImageBitmap() }
    var full by remember { mutableStateOf(false) }
    val label = stringResource(R.string.designsystem_render_diagram, source.take(80))
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        val natural = with(density) { Size(bitmap.width.toDp().value, bitmap.height.toDp().value) }
        val scale = (maxWidth.value / natural.width).coerceIn(MIN_DIAGRAM_SCALE, 1f)
        val picture = Modifier.size((natural.width * scale).dp, (natural.height * scale).dp)
        Box(Modifier.horizontalScroll(rememberScrollState())) {
            Image(image, label, picture.testTag("rendered-mermaid").clickable(onClickLabel = stringResource(R.string.designsystem_render_full_screen)) { full = true })
        }
    }
    if (full) {
        val png = remember(bitmap) { ViewerImage("mermaid:${System.identityHashCode(bitmap)}", label, bitmap.width, bitmap.height) {
            withContext(Dispatchers.Default) { ByteArrayOutputStream().also { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }.toByteArray() }
        } }
        ImageViewer(listOf(png), 0, onDismiss = { full = false })
    }
}

/**
 * Inline formulas in Markdown (`![…](persona-math:…)` from [MathMarkdown.rewrite]): drawn at the body's size in
 * [color], centred on the line; TeX KaTeX cannot read shows as its source in monospace. Any other image goes to
 * [images] (none when null).
 */
class MathImages(
    private val color: Color,
    private val body: TextStyle,
    private val images: ImageTransformer? = null,
) : ImageTransformer {
    @Composable
    override fun transform(link: String): ImageData? {
        val tex = MathMarkdown.tex(link) ?: return images?.transform(link)
        val points = renderPoints(Kind.InlineMath, body)
        val density = LocalDensity.current.density
        val bitmap = when (val drawn = rememberDrawn(Kind.InlineMath, tex, points)) {
            Drawn.Loading -> return null
            is Drawn.Picture -> drawn.bitmap
            is Drawn.Failed -> remember(tex, points, density) { MarkdownRenderer.source(tex, points, density) }
        }
        val painter = remember(bitmap) { BitmapPainter(bitmap.asImageBitmap()) }
        val spoken = stringResource(R.string.designsystem_render_formula, tex)
        return ImageData(painter, Modifier.semantics { contentDescription = spoken }, contentDescription = spoken,
            colorFilter = ColorFilter.tint(color))
    }

    @Composable
    override fun intrinsicSize(painter: Painter): Size =
        if (painter is BitmapPainter) painter.intrinsicSize else images?.intrinsicSize(painter) ?: painter.intrinsicSize

    override fun placeholderConfig(link: String, density: Density, containerSize: Size, imageWidth: ImageWidth, imageSize: Size,
        imageSizeChanged: ((link: String, Size) -> Unit)?): PlaceholderConfig {
        if (MathMarkdown.tex(link) == null) {
            return images?.placeholderConfig(link, density, containerSize, imageWidth, imageSize, imageSizeChanged)
                ?: super.placeholderConfig(link, density, containerSize, imageWidth, imageSize, imageSizeChanged)
        }
        // The picture's own pixels at the screen's density, smaller when wider than the column; nothing until drawn.
        if (imageSize.isUnspecified || imageSize.width <= 0f || imageSize.height <= 0f) return PlaceholderConfig(Size(1f, 1f))
        val fit = if (containerSize.isUnspecified || containerSize.width <= 0f) 1f else min(1f, containerSize.width / imageSize.width)
        return PlaceholderConfig(Size(imageSize.width * fit / density.density, imageSize.height * fit / density.density),
            PlaceholderVerticalAlign.TextCenter)
    }
}
