// Expanding and collapsing in a scrolled list without moving what the reader is looking at (#1124).
//
// The chat holds the row that was clicked (its top, or the bottom of a work row folded from its end) where it was on
// screen while the height runs: every time the content's size changes, the scroll offset moves by however far that
// edge moved. At the end (following the newest) it keeps following instead: the bottom stays pinned, unless that
// would push the clicked row off the top, or move it more than `FOLLOW_STEP` in one frame (a folded trace's steps
// arriving at once), where it stops following and the row stays put.

export interface ScrollAnchor {
  /** Which edge of the element is held: a header's top, or a work row's bottom when it folds from its 收起 button. */
  edge: "top" | "bottom";
  /** Where that edge was on screen (px from the viewport top) when it was clicked. */
  at: number;
  /** The list was at the end when it was clicked: keep the end in view. */
  follow: boolean;
}

export interface ScrollMetrics {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
  /** The scroll view's top on screen. */
  viewTop: number;
  /** The held edge's position on screen now. */
  edgeAt: number;
  /** The held element's top on screen now (following never pushes it above `viewTop + margin`). */
  elTop: number;
}

/** How close to the scroll view's top a followed row may go before following stops. */
export const FOLLOW_MARGIN = 12;
/** The most a followed row moves in one frame: an unfolding row moves ~25 px a frame; more is content landing at once. */
export const FOLLOW_STEP = 64;

/** Where to scroll so the anchor holds, and whether it still follows the end. */
export function anchoredScroll(a: ScrollAnchor, m: ScrollMetrics): { scrollTop: number; follow: boolean } {
  const max = Math.max(0, m.scrollHeight - m.clientHeight);
  if (a.follow) {
    // Pinning the end moves everything up by (max - scrollTop); stop where the row would leave the top.
    const move = max - m.scrollTop;
    const floor = m.viewTop + FOLLOW_MARGIN;
    if (move > FOLLOW_STEP) return { scrollTop: m.scrollTop, follow: false };
    if (m.elTop - move >= floor || m.elTop < floor) return { scrollTop: max, follow: true };
    return { scrollTop: Math.min(max, Math.max(0, m.scrollTop + (m.elTop - floor))), follow: false };
  }
  return { scrollTop: Math.min(max, Math.max(0, m.scrollTop + (m.edgeAt - a.at))), follow: false };
}

/** What a collapse renders: lazy ones mount their children when they open and keep them while the height runs back to
 *  zero (the content goes down with the height instead of vanishing), then drop them. */
export function collapseMounted(lazy: boolean, open: boolean, closing: boolean): boolean {
  return !lazy || open || closing;
}
