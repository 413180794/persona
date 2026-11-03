package persona.core.designsystem

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInWindow
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.unit.dp
import dev.chrisbanes.haze.HazeInput
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.blur.HazeBlurStyle
import dev.chrisbanes.haze.blur.HazeColorEffect
import dev.chrisbanes.haze.blur.hazeBlur
import dev.chrisbanes.haze.glass.GlassStyle
import dev.chrisbanes.haze.glass.hazeGlass
import dev.chrisbanes.haze.hazeSource

/**
 * What floating glass shows through. The shell provides one per tab so its dock and the page's own floating
 * bars sample the same content; a page without a shell creates its own.
 */
val LocalGlassSource = staticCompositionLocalOf<HazeState?> { null }

/** Marks content that floating glass above it may refract or blur. No-op outside the glass style. */
@Composable
fun Modifier.glassSource(): Modifier {
    val state = LocalGlassSource.current
    return if (state == null || PersonaTheme.style != AppStyle.Glass || PersonaTheme.glass == GlassLevel.Solid) this
    else this.hazeSource(state)
}

/**
 * Floating chrome (bars, dock, decision sheets): refraction when the device can, plain blur otherwise, an
 * opaque panel as the last resort. Outside the glass style it is a normal raised surface.
 */
@Composable
fun Modifier.floatingSurface(shape: RoundedCornerShape, accentEdge: Color? = null): Modifier {
    val palette = PersonaTheme.palette
    val state = LocalGlassSource.current
    val edge = Brush.verticalGradient(listOf(accentEdge ?: palette.strokeTop, palette.strokeBottom))
    if (PersonaTheme.style != AppStyle.Glass) {
        return this.clip(shape).background(palette.surfaceOpaque).border(1.dp, accentEdge ?: palette.line, shape)
    }
    val tint = if (palette.dark) Color(0xB80A1020) else Color(0xCCFFFFFF)
    return when {
        state == null || PersonaTheme.glass == GlassLevel.Solid ->
            this.clip(shape).background(palette.surfaceOpaque.copy(alpha = 0.97f)).border(1.dp, edge, shape)
        PersonaTheme.glass == GlassLevel.Liquid -> this
            .hazeGlass(HazeInput.Sources(state), style = GlassStyle.regular.then { shape(shape); tint(tint) })
            .border(1.dp, edge, shape)
        else -> this.clip(shape)
            .hazeBlur(HazeInput.Sources(state), style = HazeBlurStyle { blurRadius(28.dp); colorEffects(listOf(HazeColorEffect.tint(tint))) })
            .border(1.dp, edge, shape)
    }
}

/** What the page is about, for the ambient light behind glass. */
enum class Ambience { Calm, NeedsYou, Working, Connected, Alert }

/** The shell's guess for root pages; pages may pass their own. */
val LocalAmbience = compositionLocalOf { Ambience.Calm }

private class Pool(val x: Float, val y: Float, val radius: Float)

/**
 * Soft pools of light behind the glass. Each pool belongs to one status colour; the page's [Ambience] decides
 * how strong each one is, and changes fade over about a second.
 *
 * [inWindow] lays the pools out against the whole window instead of this box: every page, pane and nested page that
 * paints it then paints the same light at the same place, so whatever sits side by side or inside another reads as
 * one surface with no seam (pages use it through [PageBackdrop]). A miniature of a screen keeps its own layout.
 * [mask] fades the light by the brush's alpha (the wash under the floating bars).
 */
@Composable
fun AmbientLight(ambience: Ambience, modifier: Modifier = Modifier, inWindow: Boolean = false, mask: Brush? = null) {
    val palette = PersonaTheme.palette
    val animate = !PersonaTheme.reduceMotion
    fun target(vararg pairs: Pair<Ambience, Float>, default: Float) = pairs.firstOrNull { it.first == ambience }?.second ?: default
    val spec = if (animate) tween<Float>(900) else snap()
    val amber by animateFloatAsState(target(Ambience.NeedsYou to 1f, Ambience.Alert to 0.5f, default = 0f), spec, label = "amber")
    val blue by animateFloatAsState(target(Ambience.Working to 1f, Ambience.NeedsYou to 0.75f, Ambience.Calm to 0.7f, Ambience.Connected to 0.45f, default = 0.3f), spec, label = "blue")
    val mint by animateFloatAsState(target(Ambience.Connected to 1f, Ambience.Calm to 0.55f, Ambience.Working to 0.45f, Ambience.NeedsYou to 0.5f, default = 0f), spec, label = "mint")
    val red by animateFloatAsState(target(Ambience.Alert to 0.9f, default = 0f), spec, label = "red")
    val glow = if (palette.dark) 0.42f else 0.30f
    val window = LocalWindowInfo.current.containerSize
    // Where this box sits in the window. Read only while drawing: following a sliding page redraws the light
    // without recomposing or relaying out anything.
    val origin = remember { mutableStateOf(Offset.Zero) }
    val placed = if (inWindow) Modifier.onGloballyPositioned { origin.value = it.positionInWindow() } else Modifier
    val masked = if (mask == null) Modifier else Modifier
        .graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }
        .drawWithContent { drawContent(); drawRect(mask, blendMode = BlendMode.DstIn) }
    // Clipped: pools centred outside the box (the window's other side) must not paint over the panes beside it.
    Canvas(modifier.fillMaxSize().then(placed).clipToBounds().then(masked).background(palette.background)) {
        val area = if (inWindow && window.width > 0 && window.height > 0) Size(window.width.toFloat(), window.height.toFloat()) else size
        val shift = if (inWindow) origin.value else Offset.Zero
        val unit = area.minDimension
        fun pool(color: Color, strength: Float, at: Pool) {
            if (strength <= 0.01f) return
            val center = Offset(area.width * at.x, area.height * at.y) - shift
            val radius = unit * at.radius
            drawCircle(
                Brush.radialGradient(
                    listOf(color.copy(alpha = glow * strength), color.copy(alpha = glow * strength * 0.35f), Color.Transparent),
                    center = center, radius = radius,
                ),
                radius = radius, center = center,
            )
        }
        pool(palette.needsYou.dot, amber, Pool(0.05f, 0.10f, 0.95f))
        pool(palette.danger.dot, red, Pool(0.9f, 0.08f, 0.85f))
        pool(palette.working.dot, blue, Pool(1.0f, 0.52f, 0.9f))
        pool(palette.done.dot, mint, Pool(if (ambience == Ambience.Connected) 0.5f else 0.08f, if (ambience == Ambience.Connected) 0.06f else 0.98f, 0.7f))
    }
}

/**
 * The background under a page, a pane, the shell and anything else that needs one: the page colour, and in glass
 * the ambient light laid out against the whole window. Everything that paints it lines up, so the rail, panes side by
 * side, a page inside another page and the room between them read as one surface; the cards on it are what
 * separate things (2026-11-01: no second layer under the content, no hard band at the top or bottom).
 */
@Composable
fun PageBackdrop(modifier: Modifier = Modifier, ambience: Ambience = LocalAmbience.current) {
    if (PersonaTheme.style == AppStyle.Glass) AmbientLight(ambience, modifier, inWindow = true)
    else Box(modifier.fillMaxSize().background(PersonaTheme.palette.background))
}
