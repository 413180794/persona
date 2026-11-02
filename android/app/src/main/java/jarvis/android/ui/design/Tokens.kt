package jarvis.android.ui.design

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

// The design system's numbers (apple/Shared/Design/Theme.swift, docs/android.md「设计系统」): pages take spacing,
// corners, sizes and motion from here and never write a number of their own (TokenRatchetTest).

/** Spacing on a 4 dp grid (iOS `Theme.Space`): 2 4 6 8 12 16 24 32, nothing in between. Pages keep 16 from the edges. */
object JSpace {
    val xxs: Dp = 2.dp
    val xs: Dp = 4.dp
    val sm: Dp = 6.dp
    val s: Dp = 8.dp
    val m: Dp = 12.dp
    val l: Dp = 16.dp
    val xl: Dp = 24.dp
    val xxl: Dp = 32.dp

    /** Between the lines of one row: title, preview, meta. */
    val lines: Dp = xs
    /** From a title to the content under it. */
    val titleToContent: Dp = s
    /** Between blocks on a page or in a card. */
    val block: Dp = l
    /** Between sections. */
    val section: Dp = xl
    /** From the screen's edges to the content. */
    val page: Dp = l
}

/** Corner radii (iOS `Theme.Radius`): five, plus capsules and circles. */
object JRadius {
    /** Badges, inline code, small marks. */
    val badge: Dp = 6.dp
    /** Code blocks, thumbnails, segmented controls, small tiles. */
    val control: Dp = 10.dp
    /** Cards, groups, an open work line, option rows. */
    val card: Dp = 14.dp
    /** Message bubbles. */
    val bubble: Dp = 20.dp
    /** Floating panels over the chat (interactions, the key bar), the composer's field. */
    val panel: Dp = 22.dp
    /** A whole page pushed aside as a card (beside the drawer): about a phone screen's own corners. */
    val page: Dp = 32.dp
}

/**
 * Sizes of what is drawn next to text (iOS `Theme.Size`): dots, icons, the wells behind them, avatars, touch.
 * Lines (4 dp and less) never scale; the rest is in dp at the default text size.
 */
object JSize {
    // Lines, rules and bars.
    val hairline: Dp = 0.5.dp
    val line: Dp = 1.dp
    val lineMedium: Dp = 1.5.dp
    val rule: Dp = 2.dp
    val bar: Dp = 3.dp
    val barWide: Dp = 4.dp
    // Dots and small marks.
    val grabber: Dp = 5.dp
    val dotXS: Dp = 6.dp
    val dotS: Dp = 7.dp
    val dot: Dp = 8.dp
    val dotL: Dp = 9.dp
    val dotXL: Dp = 10.dp
    val markXS: Dp = 12.dp
    val markS: Dp = 14.dp
    val mark: Dp = 15.dp
    val markM: Dp = 16.dp
    val markL: Dp = 17.dp
    val badge: Dp = 18.dp
    // Icons and the wells behind them.
    val iconXS: Dp = 20.dp
    val iconS: Dp = 22.dp
    val icon: Dp = 24.dp
    val iconL: Dp = 26.dp
    val wellXS: Dp = 28.dp
    val wellS: Dp = 30.dp
    val well: Dp = 32.dp
    val wellM: Dp = 34.dp
    val wellL: Dp = 36.dp
    val wellXL: Dp = 38.dp
    // Faces and touch.
    val avatar: Dp = 40.dp
    val avatarM: Dp = 42.dp
    /** The smallest thing a finger should hit. */
    val tapTarget: Dp = 44.dp
    val avatarL: Dp = 46.dp
    val avatarXL: Dp = 48.dp
    // Boxes: thumbnails, previews, panels.
    val box50: Dp = 50.dp
    val box56: Dp = 56.dp
    val box64: Dp = 64.dp
    val box72: Dp = 72.dp
    val box76: Dp = 76.dp
    val box80: Dp = 80.dp
    /** Jarvis' mark on the welcome page. */
    val box96: Dp = 96.dp
    val box120: Dp = 120.dp
    val box140: Dp = 140.dp
    /** The voice page's orb (its halo is [box200]). */
    val box160: Dp = 160.dp
    val box200: Dp = 200.dp
    val box220: Dp = 220.dp
    val box280: Dp = 280.dp
    val box300: Dp = 300.dp
    /** The drawer: 82 % of the screen, never wider than this. */
    val drawerMax: Dp = 380.dp
    /** A page meant to be read (the conversation on a wide screen): no wider than this, centered. */
    val readable: Dp = 760.dp
    /** From this window width on, two panes side by side (an unfolded foldable, a tablet; iOS's regular width). */
    val wideWindow: Dp = 600.dp
    /** The narrowest the left pane (a list, the sidebar) and the right one (a chat: a phone's width) may get. */
    val paneMinLeft: Dp = 280.dp
    val paneMinRight: Dp = 320.dp
    /**
     * Where the divider starts: the list gets about 38 % of the window, at least [paneLeft] and at most [paneMax], and
     * the chat keeps [paneRight] when it can (the user may drag it anywhere between the least widths).
     */
    val paneLeft: Dp = 320.dp
    val paneMax: Dp = 420.dp
    val paneRight: Dp = 400.dp
    /** A dragged divider let go this close to a fold settles on it. */
    val snap: Dp = 28.dp
    /** Dragged this far past the left pane's least width, the left pane closes. */
    val collapseDrag: Dp = 64.dp
    /** The divider's handle: what a finger grabs, and the pill drawn in it. */
    val handleTouch: Dp = 24.dp
    val handlePill: Dp = 40.dp
    /** The shell's top bar under the status bar: what pages leave free at their top. */
    val shellBar: Dp = 60.dp
    /** The shell's switch: the width of each segment. */
    val segment: Dp = 76.dp
}

/**
 * Motion (iOS `Theme.Motion`): a state change snaps, a layout change glides, opening things in the chat reveals.
 * Nothing repeats except the 需要你 dot and the work spinner.
 */
object JMotion {
    /** iOS's `.snappy`: quick out, soft landing. */
    val snappy = CubicBezierEasing(0.2f, 0f, 0f, 1f)
    val smooth = CubicBezierEasing(0.4f, 0f, 0.2f, 1f)
    const val STATE_MS = 250
    const val LAYOUT_MS = 300
    const val REVEAL_MS = 280
    const val CONCEAL_MS = 200

    fun <T> state(): FiniteAnimationSpec<T> = tween(STATE_MS, easing = snappy)
    fun <T> layout(): FiniteAnimationSpec<T> = tween(LAYOUT_MS, easing = smooth)
    fun <T> reveal(): FiniteAnimationSpec<T> = tween(REVEAL_MS, easing = smooth)
    fun <T> conceal(): FiniteAnimationSpec<T> = tween(CONCEAL_MS, easing = smooth)

    /** A drawer let go by the finger: no bounce, carrying on at its speed. */
    fun <T> drawer(): FiniteAnimationSpec<T> = spring(dampingRatio = Spring.DampingRatioNoBouncy, stiffness = 420f)
}
