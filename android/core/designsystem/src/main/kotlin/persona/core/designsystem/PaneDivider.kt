package persona.core.designsystem

import androidx.compose.ui.res.stringResource
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.hoverable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsDraggedAsState
import androidx.compose.foundation.interaction.collectIsHoveredAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.systemGestureExclusion
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.input.pointer.PointerIcon
import androidx.compose.ui.input.pointer.pointerHoverIcon
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/** What the divider between a list and its detail is doing, for how it looks. */
enum class PaneDividerState {
    /** At rest between the panes. */
    Idle,
    /** Being dragged or settling. */
    Moving,
    /** Dragged far enough left that letting go closes the list. */
    Closing,
    /** The list is closed; the divider waits at the left edge to bring it back. */
    Collapsed,
}

/** How wide a finger's grip on the divider is: it reaches over both panes' edges. */
val PaneDividerTouch: Dp = 24.dp

/**
 * The handle between a wide window's two panes (the list and what it opened): a small grip on the line between them, a
 * finger's width to grab, the resize cursor under a mouse or trackpad. Dragging reports [onDrag] in pixels; the owner
 * moves the panes and decides where they settle in [onDragStopped]. TalkBack gets "Widen left pane" / "Narrow left pane", and "Collapse left pane" /
 * "Expand left pane" when the owner allows them; tapping the collapsed handle brings the list back.
 *
 * Glass: a soft pill of the page's ink; Brief: a thin ink rule; Board: a bordered grip like a board strip's edge.
 */
@Composable
fun PaneDivider(
    state: PaneDividerState,
    onDrag: (Float) -> Unit,
    onDragStarted: () -> Unit,
    onDragStopped: (velocity: Float) -> Unit,
    onWider: () -> Unit,
    onNarrower: () -> Unit,
    modifier: Modifier = Modifier,
    onCollapse: (() -> Unit)? = null,
    onExpand: (() -> Unit)? = null,
) {
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val interaction = remember { MutableInteractionSource() }
    val hovered by interaction.collectIsHoveredAsState()
    val dragged by interaction.collectIsDraggedAsState()
    val drag by rememberUpdatedState(onDrag)
    val draggable = rememberDraggableState { drag(it) }
    val active = hovered || dragged || state == PaneDividerState.Moving
    val marked = state == PaneDividerState.Closing || state == PaneDividerState.Collapsed
    val rest = when (style) { AppStyle.Glass -> palette.ink3.copy(alpha = 0.6f); AppStyle.Brief -> palette.line; AppStyle.Board -> palette.surface }
    val color by animateColorAsState(
        when {
            marked -> palette.accent
            active -> if (style == AppStyle.Board) palette.surface else palette.ink2
            else -> rest
        }, tween(PersonaTheme.motion.quick), label = "pane-divider",
    )
    val (thin, tall) = when (style) {
        AppStyle.Glass -> 4.dp to 40.dp
        AppStyle.Brief -> 2.dp to 44.dp
        AppStyle.Board -> 6.dp to 36.dp
    }
    val gripWidth by animateDpAsState(if (active || marked) thin + 2.dp else thin, tween(PersonaTheme.motion.quick), label = "pane-divider-width")
    val gripHeight by animateDpAsState(if (state == PaneDividerState.Collapsed) tall + 24.dp else if (active) tall + 8.dp else tall,
        tween(PersonaTheme.motion.quick), label = "pane-divider-height")
    val shape = if (style == AppStyle.Board) RoundedCornerShape(2.dp) else RoundedCornerShape(50)
    val reach = with(LocalDensity.current) { 48.dp.toPx() }
    val expandLeft = stringResource(R.string.designsystem_pane_expand)
    val resize = stringResource(R.string.designsystem_pane_resize)
    val collapsedState = stringResource(R.string.designsystem_pane_collapsed)
    val wider = stringResource(R.string.designsystem_pane_wider)
    val narrower = stringResource(R.string.designsystem_pane_narrower)
    val collapseLeft = stringResource(R.string.designsystem_pane_collapse)
    Box(
        modifier.width(PaneDividerTouch).fillMaxHeight()
            // Near the grip a swipe moves the divider, not the system's back gesture (at the window's edge when collapsed).
            .systemGestureExclusion { coordinates ->
                val middle = coordinates.size.height / 2f
                Rect(0f, middle - reach, coordinates.size.width.toFloat(), middle + reach)
            }
            .hoverable(interaction)
            // A mouse or a trackpad (a tablet's keyboard, a desktop window) shows the resize cursor here.
            .pointerHoverIcon(PointerIcon(android.view.PointerIcon.TYPE_HORIZONTAL_DOUBLE_ARROW))
            .draggable(draggable, Orientation.Horizontal, interactionSource = interaction,
                onDragStarted = { onDragStarted() }, onDragStopped = { velocity -> onDragStopped(velocity) })
            .then(if (state == PaneDividerState.Collapsed && onExpand != null)
                Modifier.clickable(interactionSource = interaction, indication = null, role = Role.Button, onClickLabel = expandLeft) { onExpand() }
            else Modifier)
            .semantics {
                contentDescription = resize
                if (state == PaneDividerState.Collapsed) stateDescription = collapsedState
                customActions = listOfNotNull(
                    CustomAccessibilityAction(wider) { onWider(); true }.takeIf { state != PaneDividerState.Collapsed },
                    CustomAccessibilityAction(narrower) { onNarrower(); true }.takeIf { state != PaneDividerState.Collapsed },
                    onCollapse?.takeIf { state != PaneDividerState.Collapsed }?.let { collapse -> CustomAccessibilityAction(collapseLeft) { collapse(); true } },
                    onExpand?.takeIf { state == PaneDividerState.Collapsed }?.let { expand -> CustomAccessibilityAction(expandLeft) { expand(); true } },
                )
            },
        contentAlignment = Alignment.Center,
    ) {
        Box(
            Modifier.size(gripWidth, gripHeight).clip(shape).background(color)
                .then(if (style == AppStyle.Board) Modifier.border(1.dp, if (marked) palette.accent else if (active) palette.ink2 else palette.line, shape) else Modifier),
        )
    }
}

/** A gap as wide as the panes' spacing with the divider in it, for previews and the component catalog. */
@Composable
fun PaneDividerPreview(state: PaneDividerState, modifier: Modifier = Modifier) {
    Box(modifier.width(PaneDividerTouch).height(160.dp), contentAlignment = Alignment.Center) {
        PaneDivider(state, onDrag = {}, onDragStarted = {}, onDragStopped = {}, onWider = {}, onNarrower = {},
            onCollapse = {}, onExpand = {})
    }
}
