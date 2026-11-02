package jarvis.android.ui.shell

import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.clickable
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.min
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.JMotion
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/** How far the shell's drawer is out: 0 shut, 1 open (iOS `AssistantDrawer`). */
@Stable
class DrawerState(initial: Float = 0f) {
    val progress = Animatable(initial)
    val isOpen: Boolean get() = progress.targetValue > 0.5f
    val isOut: Boolean get() = progress.value > 0f

    suspend fun open() = progress.animateTo(1f, JMotion.drawer())
    suspend fun close() = progress.animateTo(0f, JMotion.drawer())

    /**
     * Where a let-go drawer settles (iOS `settlesOpen`): a quick flick goes its way; otherwise past half (with a
     * little of the finger's momentum) opens.
     */
    suspend fun settle(velocity: Float, width: Float) {
        val open = when {
            velocity > FLICK -> true
            velocity < -FLICK -> false
            else -> progress.value + 0.2f * velocity / width > 0.5f
        }
        progress.animateTo(if (open) 1f else 0f, JMotion.drawer(), initialVelocity = velocity / width)
    }

    companion object {
        /** px/s past which a let-go drawer follows the finger's direction (300 dp/s on a 3× screen). */
        const val FLICK = 900f
    }
}

/**
 * The shell's drawer layout (iOS `AssistantDrawerLayout`): the sidebar on the left, the page as a card that slides
 * aside with it — the sidebar's right edge rides on the card's left edge, both opaque, so what is under the finger
 * moves as one. A horizontal drag anywhere on the page opens it while [enabled] (the page is at its root); a tap
 * on the pushed-aside card or a drag back closes it, as does the back gesture.
 */
@Composable
fun DrawerLayout(
    drawer: DrawerState, enabled: Boolean, modifier: Modifier = Modifier,
    sidebar: @Composable () -> Unit, page: @Composable () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val density = LocalDensity.current
    BoxWithConstraints(modifier.fillMaxSize().background(JTheme.shell.drawer)) {
        val width: Dp = min(maxWidth * 0.82f, JSize.drawerMax)
        val widthPx = with(density) { width.toPx() }
        // Only crossing 0 recomposes: the moving itself happens in the layers.
        val out by remember(drawer) { derivedStateOf { drawer.progress.value > 0f } }
        val drag = rememberDraggableState { delta ->
            scope.launch { drawer.progress.snapTo((drawer.progress.value + delta / widthPx).coerceIn(0f, 1f)) }
        }
        val draggable = Modifier.draggable(
            drag, Orientation.Horizontal, enabled = enabled || out,
            onDragStopped = { v -> drawer.settle(v, widthPx) },
        )
        BackHandler(enabled = drawer.isOpen) { scope.launch { drawer.close() } }
        Box(Modifier.fillMaxSize().then(draggable)) {
            // Built once, kept: it only moves and shows (iOS keeps its sidebar in its own host for the same reason).
            Box(
                Modifier.width(width).fillMaxHeight().graphicsLayer {
                    translationX = (drawer.progress.value - 1f) * widthPx
                    alpha = if (drawer.progress.value > 0f) 1f else 0f
                },
            ) { sidebar() }
            val corner = if (out) JRadius.page else 0.dp
            val shape = RoundedCornerShape(corner)
            Box(
                Modifier.fillMaxSize()
                    .graphicsLayer {
                        translationX = drawer.progress.value * widthPx
                        shadowElevation = if (drawer.progress.value > 0f) with(density) { 18.dp.toPx() } else 0f
                        this.shape = shape
                        clip = drawer.progress.value > 0f
                    }
                    .then(if (out && JTheme.isDark) Modifier.border(JSize.hairline * 2, Color.White.copy(alpha = 0.12f), shape) else Modifier),
            ) {
                page()
                // Out: the card is a button that closes the drawer (and nothing under it takes the touch).
                if (out) Box(
                    Modifier.fillMaxSize().clip(shape).clickable(remember { MutableInteractionSource() }, indication = null) { scope.launch { drawer.close() } },
                )
            }
        }
    }
}

/** Opens or closes the drawer from a button. */
fun DrawerState.openIn(scope: CoroutineScope) = scope.launch { open() }
fun DrawerState.closeIn(scope: CoroutineScope) = scope.launch { close() }
