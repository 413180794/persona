package jarvis.android.ui.shell

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.coerceIn
import androidx.window.layout.FoldingFeature
import androidx.window.layout.WindowInfoTracker
import jarvis.android.ui.design.JSize
import kotlin.math.abs

/**
 * A vertical fold across the app's window (docs/android.md「折叠屏和大屏」), in dp from the window's left edge.
 * [separating]: the two sides are apart — a book-style foldable half open, or two screens with a gap between them —
 * so no pane may run across it. Folded flat, a crease is barely there: panes may cross it, the divider only snaps to
 * it. [left] == [right] for a fold with no gap.
 */
@Immutable
data class Hinge(val left: Dp, val right: Dp, val separating: Boolean = false) {
    val center: Dp get() = (left + right) / 2
}

/** Folds to lay out around without a device (the screenshot tests), in window px like WindowManager gives them. */
@Immutable
data class HingePx(val left: Float, val right: Float, val separating: Boolean = false)

val LocalHingeOverride = staticCompositionLocalOf<List<HingePx>?> { null }

/**
 * The window's vertical folds from Jetpack WindowManager, in window px: none on a phone, a cover screen, one side of
 * a split screen, or a system that does not report them; one on a book-style foldable; two on a tri-fold opened all
 * the way. Horizontal folds (a flip phone, a book-style one turned sideways) do not split panes.
 */
@Composable
fun rememberVerticalHinges(): List<HingePx> {
    LocalHingeOverride.current?.let { return it }
    val activity = LocalContext.current.findActivity() ?: return emptyList()
    val layouts = remember(activity) { runCatching { WindowInfoTracker.getOrCreate(activity).windowLayoutInfo(activity) }.getOrNull() }
        ?: return emptyList()
    val info by layouts.collectAsState(initial = null)
    return info?.displayFeatures.orEmpty().filterIsInstance<FoldingFeature>()
        .filter { it.orientation == FoldingFeature.Orientation.VERTICAL }
        .map { HingePx(it.bounds.left.toFloat(), it.bounds.right.toFloat(), it.isSeparating) }
}

private fun Context.findActivity(): Activity? {
    var c: Context? = this
    while (c is ContextWrapper) {
        if (c is Activity) return c
        c = c.baseContext
    }
    return null
}

/**
 * The window is wide enough for two panes (Material's medium width and up, iOS's regular width): an unfolded
 * foldable, a tablet, a phone on its side, a wide split screen or desktop window. Cover screens, flip phones and
 * phones held upright stay one page.
 */
fun isWide(width: Dp): Boolean = width >= JSize.wideWindow

/**
 * How a wide window's two panes share it: the left pane's width, the line after it (a separating fold's gap, or a
 * hairline), and whether a separating fold fixes the split (the divider then cannot be dragged). [snaps] are the folds
 * lying flat: a dragged divider let go near one settles on it.
 */
@Immutable
data class PaneSplit(val left: Dp, val gap: Dp, val fixed: Boolean, val snaps: List<Dp>)

/** The pane widths every split keeps: a list still readable on the left, a chat a phone's width on the right. */
fun minLeft(): Dp = JSize.paneMinLeft
fun maxLeft(width: Dp): Dp = (width - JSize.paneMinRight).coerceAtLeast(JSize.paneMinLeft)

/** The left pane's share when the user has not dragged the divider: about 38 %, the chat gets the rest. */
const val DEFAULT_SHARE = 0.38f

/**
 * Where the panes meet in a window [width] wide (docs/android.md「折叠屏和大屏」):
 * - a separating fold with room for both panes fixes the split there (the one nearest the usual split: a tri-fold
 *   has two);
 * - otherwise the user's share of the width ([share], kept per page), held between the panes' least widths; or by
 *   default [DEFAULT_SHARE], the list [JSize.paneLeft]…[JSize.paneMax] wide, the chat at least [JSize.paneRight]
 *   if it can be; a flat fold close by takes the divider;
 * - folds lying flat are snap points for the divider, nothing more: panes run across them.
 */
fun paneSplit(width: Dp, hinges: List<Hinge>, share: Float?): PaneSplit {
    val lo = minLeft()
    val hi = maxLeft(width)
    val usual = if (share != null) (width * share).coerceIn(lo, hi)
    else (width * DEFAULT_SHARE).coerceIn(JSize.paneLeft, JSize.paneMax).coerceAtMost(width - JSize.paneRight).coerceIn(lo, hi)
    val fits = hinges.filter { it.left >= lo && width - it.right >= JSize.paneMinRight }
    fits.filter { it.separating }.minByOrNull { abs((it.center - usual).value) }?.let { h ->
        return PaneSplit(h.left, maxOf(h.right - h.left, JSize.hairline), fixed = true, snaps = emptyList())
    }
    val flat = fits.map { it.center }
    return PaneSplit(nearFold(usual, flat) ?: usual, JSize.hairline, fixed = false, snaps = flat)
}

/** The flat fold within [JSize.snap] of [at], if any. */
fun nearFold(at: Dp, snaps: List<Dp>): Dp? = snaps.firstOrNull { abs((it - at).value) <= JSize.snap.value }

/**
 * Where a dragged divider settles: on a flat fold within [JSize.snap] of it, else where it was let go, held between
 * the panes' least widths. Null: dragged far enough left to close the left pane.
 */
fun settleDivider(at: Dp, width: Dp, snaps: List<Dp>): Dp? {
    if (at < minLeft() - JSize.collapseDrag) return null
    return (nearFold(at, snaps) ?: at).coerceIn(minLeft(), maxLeft(width))
}
