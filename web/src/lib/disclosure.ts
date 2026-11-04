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
  /** Folding from inside a trace: the furthest down the element's top may come on screen (#1486). Holding the end
   *  brings the header down from above; it stops here instead of going on past the view's bottom. */
  topLimit?: number;
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
  let move = m.edgeAt - a.at;
  if (a.topLimit !== undefined) move = Math.max(move, m.elTop - a.topLimit);
  return { scrollTop: Math.min(max, Math.max(0, m.scrollTop + move)), follow: false };
}

/** How far down a work row's header may come when it folds from inside (its 收起 at the end, the hairline on its
 *  left), #1486. A header on screen stays where it is; one above the view comes down to where the reader clicked,
 *  kept inside the view. */
export function foldTopLimit(m: { rowTop: number; click: number; viewTop: number; viewBottom: number; header: number }): number {
  if (m.rowTop >= m.viewTop) return m.rowTop;
  return Math.max(m.viewTop + FOLLOW_MARGIN, Math.min(m.click, m.viewBottom - m.header - FOLLOW_MARGIN));
}

/** What a collapse renders: lazy ones mount their children when they open and keep them while the height runs back to
 *  zero (the content goes down with the height instead of vanishing), then drop them. */
export function collapseMounted(lazy: boolean, open: boolean, closing: boolean): boolean {
  return !lazy || open || closing;
}

/** How close to the end still counts as at the end. */
export const END_SLACK = 40;

/** Whether the chat still follows the end after a scroll event (#1340). Only the reader leaves the end: a scroll event
 *  that moves the offset up while the reader is scrolling (wheel, touch, keys, the scroll bar). The chat's own
 *  events do not: its pin to the end whose event arrives after more content landed (images, cards, a page coming in
 *  a burst) sees the end far away, and the browser pulling the offset up when the view grows for a moment (the
 *  composer re-measuring its height) moves it up. Back near the end always follows again. */
export function followsAfterScroll(following: boolean, m: { scrollTop: number; lastTop: number; gap: number; reader: boolean }): boolean {
  if (m.gap < END_SLACK) return true;
  if (m.reader && m.scrollTop < m.lastTop - 1) return false;
  return following;
}

/** How long after the reader's last wheel, touch or key a scroll still counts as theirs (smooth scrolling and momentum
 *  run on after the input). */
export const READER_WINDOW = 700;
