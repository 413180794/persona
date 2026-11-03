package persona.core.designsystem

import android.graphics.Paint
import android.graphics.RectF
import android.os.PowerManager
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameMillis
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.delay
import kotlin.math.cos
import kotlin.math.max
import kotlin.math.sin

/** What the persona's face shows (docs/persona.md「状态」); the wire values are persona.json's. */
enum class PersonaState(val wire: String, val label: String) {
    Idle("idle", "空闲"), Thinking("thinking", "思考中"), Working("working", "工作中"),
    NeedsYou("needs_you", "等你处理"), Offline("offline", "离线");

    /** The states whose face moves every frame (idle only blinks now and then). */
    val moves: Boolean get() = this == Thinking || this == Working || this == NeedsYou
}

/**
 * The default persona's face, 面罩 (iOS `PersonaFace`, design `.visor`): an accent disc with a dark visor, two bright
 * eyes and the brand's sparkle on top, drawn in the theme's accent so it follows the style and stays sharp at any
 * size. It moves for [state] like iOS: a blink every few seconds (空闲), eyes up under a thought bubble (思考中),
 * eyes scanning (工作中), a breathing ring in 需要你's red (等你处理), gray (离线). Reduce Motion holds each
 * state's still frame. Decorative: the control around it says who and what.
 */
@Composable
fun PersonaFace(state: PersonaState, modifier: Modifier = Modifier, size: Dp = 40.dp, tint: Color? = null) {
    val palette = PersonaTheme.palette
    val context = LocalContext.current
    // Held still with Reduce Motion and in Battery Saver, like iOS (Low Power Mode) and the glass fallback.
    val still = PersonaTheme.reduceMotion || context.getSystemService(PowerManager::class.java)?.isPowerSaveMode == true
    // 晨报 is paper and ink, its accent is the ink itself (near black / near white), where the dark visor and the white
    // sparkle would vanish: there the face is the secondary ink, a mid gray, and the style keeps no other colour.
    val base = tint ?: if (PersonaTheme.style == AppStyle.Brief) palette.ink2 else palette.accent
    val accent = if (state == PersonaState.Offline) Color(0xFF9E9E9E) else base
    val ring = palette.needsYou.dot
    val moving = state.moves && !still

    // Seconds for the moving states, about 15 frames a second like iOS (nothing runs while idle or still).
    var time by remember { mutableFloatStateOf(0f) }
    LaunchedEffect(moving) {
        if (!moving) return@LaunchedEffect
        val start = withFrameMillis { it }
        while (true) {
            time = (withFrameMillis { it } - start) / 1000f
            delay(FRAME_MS)
        }
    }
    // The idle blink: the eyes close to 12 % and open again every 3.6 s.
    val blink = remember { Animatable(1f) }
    LaunchedEffect(state, still) {
        blink.snapTo(1f)
        if (state != PersonaState.Idle || still) return@LaunchedEffect
        while (true) {
            delay(BLINK_EVERY_MS)
            blink.animateTo(0.12f, tween(BLINK_MS))
            blink.animateTo(1f, tween(BLINK_MS))
        }
    }

    val paint = remember { Paint(Paint.ANTI_ALIAS_FLAG) }
    val rect = remember { RectF() }
    Box(modifier.size(size).clearAndSetSemantics {}, contentAlignment = Alignment.Center) {
        Canvas(Modifier.fillMaxSize()) {
            visor(state, accent, ring, if (moving) time else 0f, moving, blink.value, paint, rect)
        }
        if (state == PersonaState.Thinking) {
            ThoughtBubble(size, accent, if (moving) time else 0f, moving,
                Modifier.align(Alignment.Center).offset(x = size * 0.25f, y = -size * 0.3f))
        } else {
            JIcon(PhIcons.Sparkle, null, Modifier.align(Alignment.Center).offset(x = size * 0.25f, y = -size * 0.27f),
                size = size * 0.22f, tint = Color.White.copy(alpha = if (state == PersonaState.Offline) 0.5f else 0.92f), selected = true)
        }
    }
}

/** Draws 面罩 at time t; [paint] and [rect] are reused across frames for the glowing eyes. */
private fun DrawScope.visor(
    state: PersonaState, accent: Color, ring: Color, t: Float, moving: Boolean, open: Float, paint: Paint, rect: RectF,
) {
    val s = this.size.minDimension
    val offline = state == PersonaState.Offline
    // The disc, light at the top left to dark at the bottom right, and its thin light rim.
    drawCircle(Brush.linearGradient(listOf(lerp(accent, Color.White, 0.28f), accent, lerp(accent, Color.Black, 0.18f)),
        start = Offset.Zero, end = Offset(s, s)))
    // The visor: a dark capsule a little below the middle.
    val vw = s * 0.66f
    val vh = s * 0.30f
    val vTop = Offset((s - vw) / 2, (s - vh) / 2 + s * 0.02f)
    drawRoundRect(Brush.verticalGradient(listOf(Color(0xFF292929), Color(0xFF0D0D0D)), startY = vTop.y, endY = vTop.y + vh),
        vTop, Size(vw, vh), CornerRadius(vh / 2))
    val visorRim = max(0.5.dp.toPx(), s * 0.012f)
    drawRoundRect(Color.White.copy(alpha = 0.14f), Offset(vTop.x + visorRim / 2, vTop.y + visorRim / 2),
        Size(vw - visorRim, vh - visorRim), CornerRadius((vh - visorRim) / 2), style = Stroke(visorRim))

    // The eyes: where they look and how open they are for the state at time t.
    val (lookX, lookY) = when (state) {
        PersonaState.Working -> (if (moving) sin(t * 1.6f) * s * 0.05f else 0f) to 0f
        PersonaState.Thinking -> (s * 0.04f + (if (moving) sin(t * 0.9f) * s * 0.012f else 0f)) to -s * 0.035f
        else -> 0f to 0f
    }
    val ew = s * 0.085f
    val eh = (if (offline) s * 0.03f else s * 0.13f) * (if (state == PersonaState.Idle) open else 1f)
    val gap = s * 0.15f
    val cx = s / 2 + lookX
    val cy = vTop.y + vh / 2 + lookY
    val eye = if (offline) Color(0xFF808080) else lerp(accent, Color.White, 0.55f)
    paint.color = eye.toArgb()
    // The eyes glow in the accent (iOS: a shadow of radius 5 % of the side).
    if (offline) paint.clearShadowLayer() else paint.setShadowLayer(s * 0.05f, 0f, 0f, accent.copy(alpha = 0.9f).toArgb())
    drawIntoCanvas { canvas ->
        for (x in floatArrayOf(cx - gap / 2 - ew, cx + gap / 2)) {
            rect.set(x, cy - eh / 2, x + ew, cy + eh / 2)
            canvas.nativeCanvas.drawRoundRect(rect, ew / 2, ew / 2, paint)
        }
    }

    val rim = max(0.5.dp.toPx(), s * 0.015f)
    drawCircle(Color.White.copy(alpha = 0.18f), radius = (s - rim) / 2, style = Stroke(rim))
    // 需要你: a red ring inside the edge, breathing while moving.
    if (state == PersonaState.NeedsYou) {
        val w = max(RING_MIN.toPx(), s * RING_SHARE)
        val pulse = if (moving) 0.55f + 0.45f * cos(t * 3f) else 1f
        drawCircle(ring.copy(alpha = ring.alpha * pulse), radius = (s - w) / 2, style = Stroke(w))
    }
}

/** A small thought bubble whose three dots light up in turn (all lit when still). */
@Composable
private fun ThoughtBubble(size: Dp, accent: Color, t: Float, moving: Boolean, modifier: Modifier) {
    val lit = if (moving) (t * 2.5f).toInt() % 4 else 3
    Row(
        modifier.shadow(size * 0.02f, CircleShape).clip(RoundedCornerShape(50)).background(Color.White)
            .padding(horizontal = size * 0.04f, vertical = size * 0.04f),
        horizontalArrangement = Arrangement.spacedBy(size * 0.025f), verticalAlignment = Alignment.CenterVertically,
    ) {
        repeat(3) { i ->
            Box(Modifier.size(size * 0.05f).clip(CircleShape).background(if (i == lit || lit == 3) accent else Color(0xFFB3B3B3)))
        }
    }
}

private const val FRAME_MS = 66L
private const val BLINK_EVERY_MS = 3600L
private const val BLINK_MS = 110
/** 需要你's ring: this share of the face's side, never thinner than [RING_MIN] (iOS `ringShare` / `ringMin`). */
private const val RING_SHARE = 0.07f
private val RING_MIN = 1.5.dp

/**
 * The persona's face as a top-bar button (iOS `ShellPersonaButton`): as big as the bar's round buttons, read out as
 * [contentDescription] with the persona's state.
 */
@Composable
fun JPersonaButton(state: PersonaState, contentDescription: String, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier.size(48.dp).clip(CircleShape)
            .clickable(role = Role.Button, onClick = onClick)
            .semantics { this.contentDescription = contentDescription; stateDescription = "Persona，${state.label}" },
        contentAlignment = Alignment.Center,
    ) { PersonaFace(state, size = 40.dp) }
}
