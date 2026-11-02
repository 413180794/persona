package jarvis.android.ui.shell

import android.content.Context
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.hoverable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsDraggedAsState
import androidx.compose.foundation.interaction.collectIsHoveredAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.PointerIcon
import androidx.compose.ui.input.pointer.pointerHoverIcon
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.Haptics
import jarvis.android.ui.design.JMotion
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.hapticView
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * How wide the left pane is, per page, as its share of the window (so it fits any window: the inner screen upright or
 * on its side, a tablet, a resized desktop window), kept on this phone.
 */
class SplitPrefs(context: Context?) {
    private val p = runCatching { context?.applicationContext?.getSharedPreferences("ui", Context.MODE_PRIVATE) }.getOrNull()

    fun share(kind: PaneKind): Float? = p?.getFloat(key(kind), -1f)?.takeIf { it > 0f }
    fun save(kind: PaneKind, share: Float) { p?.edit()?.putFloat(key(kind), share)?.apply() }
    private fun key(kind: PaneKind) = "ui.split." + kind.name.lowercase()
}

/** The two-pane places, each keeping its own divider: 助手, Agents, 收件箱 and 设置. */
enum class PaneKind { Assistant, Agents, Inbox, Settings }

/**
 * A wide window's two panes (iOS's split view) with a divider the finger drags (Material's pane expansion): the left
 * pane follows it, ticking past a fold; let go, it settles on a flat fold nearby, or — dragged well past the left
 * pane's least width — closes the left pane ([onCollapse]; none: it stops there). The share is kept ([onShare]).
 * A separating fold fixes the split: no handle. No [left]: the right pane takes it all.
 */
@Composable
fun Panes(
    width: Dp, split: PaneSplit, left: (@Composable () -> Unit)?,
    onShare: (Float) -> Unit, onCollapse: (() -> Unit)?,
    right: @Composable () -> Unit,
) {
    // Where the divider is, in dp. Read only in layout and drawing: a drag re-lays out the panes, never recomposes them.
    var at by remember { mutableFloatStateOf(split.left.value) }
    LaunchedEffect(split.left, width) { at = split.left.value }
    val lo = minLeft()
    Box(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxSize()) {
            if (left != null) {
                Box(
                    Modifier
                        .layout { measurable, constraints ->
                            val w = at.dp.roundToPx().coerceIn(0, constraints.maxWidth)
                            val p = measurable.measure(Constraints.fixed(w, constraints.maxHeight))
                            layout(w, constraints.maxHeight) { p.place(0, 0) }
                        }
                        // Dragged past its least width the list fades: let go now and it closes.
                        .graphicsLayer { alpha = if (at >= lo.value) 1f else (1f - (lo.value - at) / JSize.collapseDrag.value).coerceIn(FADED, 1f) },
                ) { left() }
                Box(Modifier.width(split.gap).fillMaxHeight().background(JTheme.colors.separator))
            }
            Box(Modifier.weight(1f).fillMaxHeight()) { right() }
        }
        if (left != null && !split.fixed) Divider(width, split, { at }, { at = it }, onShare, onCollapse)
    }
}

private const val FADED = 0.35f

/** The divider's handle: a pill on the line, a finger's width to grab, the resize cursor under a mouse. */
@Composable
private fun Divider(
    width: Dp, split: PaneSplit, at: () -> Float, moveTo: (Float) -> Unit,
    onShare: (Float) -> Unit, onCollapse: (() -> Unit)?,
) {
    val density = LocalDensity.current
    val view = hapticView()
    val scope = rememberCoroutineScope()
    val settling = remember { arrayOfNulls<Job>(1) }
    val ticks = remember { DragTicks() }
    val interaction = remember { MutableInteractionSource() }
    val dragged by interaction.collectIsDraggedAsState()
    val hovered by interaction.collectIsHoveredAsState()
    val active = dragged || hovered
    val lowest = if (onCollapse != null) (minLeft() - JSize.collapseDrag - JSpace.l).value else minLeft().value
    val highest = maxLeft(width).value

    fun settle(to: Dp?, velocity: Float = 0f) {
        settling[0]?.cancel()
        settling[0] = scope.launch {
            if (to == null) {
                onCollapse?.invoke()
                moveTo(split.left.value)
                return@launch
            }
            animate(at(), to.value, initialVelocity = velocity / density.density, animationSpec = JMotion.drawer()) { v, _ -> moveTo(v) }
            onShare(to / width)
        }
    }
    val drag = rememberDraggableState { delta ->
        val now = (at() + delta / density.density).coerceIn(lowest, highest)
        moveTo(now)
        if (ticks.reached(now.dp, split.snaps, onCollapse != null)) Haptics.tap(view)
    }
    val pill by animateDpAsState(if (active) JSize.dotXS else JSize.barWide, JMotion.state(), label = "handle")
    val tint by animateColorAsState(if (active) JTheme.colors.secondaryText else JTheme.colors.tertiaryText, JMotion.state(), label = "handle")
    Box(
        Modifier
            .offset { IntOffset((at().dp - JSize.handleTouch / 2).roundToPx(), 0) }
            .width(JSize.handleTouch).fillMaxHeight()
            .hoverable(interaction)
            // A mouse or a trackpad (a desktop window, a tablet's keyboard) shows the resize cursor here.
            .pointerHoverIcon(PointerIcon(android.view.PointerIcon.TYPE_HORIZONTAL_DOUBLE_ARROW))
            .draggable(
                drag, Orientation.Horizontal, interactionSource = interaction,
                onDragStarted = { settling[0]?.cancel() },
                onDragStopped = { v -> settle(settleDivider(at().dp, width, split.snaps) ?: if (onCollapse == null) minLeft() else null, v) },
            )
            .semantics {
                contentDescription = "调整左右两栏的宽度"
                customActions = listOfNotNull(
                    CustomAccessibilityAction("左栏变宽") { settle((at().dp + STEP).coerceAtMost(maxLeft(width))); true },
                    CustomAccessibilityAction("左栏变窄") { settle((at().dp - STEP).coerceAtLeast(minLeft())); true },
                    onCollapse?.let { CustomAccessibilityAction("收起左栏") { it(); true } },
                )
            },
        contentAlignment = Alignment.Center,
    ) {
        Box(Modifier.width(pill).height(JSize.handlePill).clip(CircleShape).background(tint))
    }
}

/** How far one accessibility action moves the divider. */
private val STEP = JSpace.xxl * 2

/** Ticks once as a dragged divider reaches a fold, or the point where letting go closes the left pane. */
private class DragTicks {
    private var fold: Dp? = null
    private var closing = false

    fun reached(at: Dp, snaps: List<Dp>, canClose: Boolean): Boolean {
        val f = nearFold(at, snaps)
        val c = canClose && at < minLeft() - JSize.collapseDrag
        val tick = (f != null && f != fold) || (c && !closing)
        fold = f
        closing = c
        return tick
    }
}
