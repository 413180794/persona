package persona.android.navigation

import kotlin.math.abs

/**
 * Where a wide window's two panes meet (the list beside its detail), in dp from the panes' left edge. Ported from the
 * old app's shell (`Fold.kt`); the user asked for it on 2026-11-01: the divider can be dragged, the right pane must not
 * get too narrow, and nothing is tuned to one device.
 */
internal object PaneSplits {
    /** The narrowest the list may be: still a readable list. */
    const val MinLeft = 280f
    /** The narrowest the detail may be: a phone's width for a conversation. */
    const val MinRight = 320f
    /** Without a dragged split the list takes about this share of the width… */
    const val DefaultShare = 0.38f
    /** …held between these widths… */
    const val DefaultLeft = 320f
    const val DefaultMax = 420f
    /** …and the detail keeps this much when it can. */
    const val DefaultRight = 400f
    /** A divider let go this close to a flat fold settles on it. */
    const val Snap = 28f
    /** Dragged this far past [MinLeft], letting go closes the list (where the tab allows it). */
    const val CollapseDrag = 64f
    /** How far one accessibility action moves the divider. */
    const val Step = 64f
}

/**
 * A vertical fold across the panes' area, in dp from its left edge. [separating]: the two sides are apart (a book-style
 * foldable half open, two screens with a gap), so no pane may run across it; a fold lying flat is barely there and only
 * a place the divider snaps to.
 */
internal data class PaneHinge(val left: Float, val right: Float, val separating: Boolean = false) {
    val center: Float get() = (left + right) / 2
}

/**
 * The list's [left] width. [fixed]: a separating fold fixes the split, nothing can be dragged (the scaffold keeps the
 * panes off the fold itself). [snaps]: the flat folds a let-go divider settles on.
 */
internal data class PaneSplit(val left: Float, val fixed: Boolean, val snaps: List<Float>)

internal fun maxLeft(width: Float): Float = (width - PaneSplits.MinRight).coerceAtLeast(PaneSplits.MinLeft)

/** The list's width before the user drags: about [PaneSplits.DefaultShare], the detail at least [PaneSplits.DefaultRight] when it can be. */
internal fun defaultLeft(width: Float): Float = (width * PaneSplits.DefaultShare)
    .coerceIn(PaneSplits.DefaultLeft, PaneSplits.DefaultMax)
    .coerceAtMost(width - PaneSplits.DefaultRight)
    .coerceIn(PaneSplits.MinLeft, maxLeft(width))

/**
 * Where the panes meet in a [width] dp wide area: a separating fold with room for both panes fixes the split (the one
 * nearest the usual split when there are two); otherwise the user's [share] of the width (kept per tab) held between the
 * panes' least widths, or the default; a flat fold close to that takes the divider. Folds too near an edge (a pane
 * would be narrower than its least width there) do not count.
 */
internal fun paneSplit(width: Float, hinges: List<PaneHinge>, share: Float?): PaneSplit {
    val lo = PaneSplits.MinLeft
    val hi = maxLeft(width)
    val usual = if (share != null && share > 0f) (width * share).coerceIn(lo, hi) else defaultLeft(width)
    val fits = hinges.filter { it.left >= lo && width - it.right >= PaneSplits.MinRight }
    fits.filter { it.separating }.minByOrNull { abs(it.center - usual) }?.let { return PaneSplit(it.left, fixed = true, snaps = emptyList()) }
    val flat = fits.map { it.center }
    return PaneSplit(nearFold(usual, flat) ?: usual, fixed = false, snaps = flat)
}

/** The flat fold within [PaneSplits.Snap] of [at], if any. */
internal fun nearFold(at: Float, snaps: List<Float>): Float? = snaps.firstOrNull { abs(it - at) <= PaneSplits.Snap }

/** Letting go here closes the list (when the tab allows it). */
internal fun closesList(at: Float): Boolean = at < PaneSplits.MinLeft - PaneSplits.CollapseDrag

/** How far the divider can be dragged: past the list's least width only when letting go there may close it. */
internal fun dragRange(width: Float, canCollapse: Boolean): ClosedFloatingPointRange<Float> =
    (if (canCollapse) 0f else PaneSplits.MinLeft)..maxLeft(width)

/**
 * Where a dragged divider settles: on a flat fold within [PaneSplits.Snap], else where it was let go, held between the
 * panes' least widths. Null: dragged far enough left to close the list ([canCollapse]).
 */
internal fun settleDivider(at: Float, width: Float, snaps: List<Float>, canCollapse: Boolean = true): Float? {
    if (canCollapse && closesList(at)) return null
    return (nearFold(at, snaps) ?: at).coerceIn(PaneSplits.MinLeft, maxLeft(width))
}

/** The share of the width a settled divider keeps, so it fits the next window it is shown in. */
internal fun shareOf(left: Float, width: Float): Float = if (width <= 0f) PaneSplits.DefaultShare else (left / width).coerceIn(0.05f, 0.95f)

/** Ticks once as a dragged divider reaches a fold, or the point where letting go closes the list. */
internal class DragTicks {
    private var fold: Float? = null
    private var closing = false

    fun reached(at: Float, snaps: List<Float>, canCollapse: Boolean): Boolean {
        val f = nearFold(at, snaps)
        val c = canCollapse && closesList(at)
        val tick = (f != null && f != fold) || (c && !closing)
        fold = f
        closing = c
        return tick
    }
}
