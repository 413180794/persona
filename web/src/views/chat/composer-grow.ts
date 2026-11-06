// The phone composer grows with the draft, then offers Expand (#5688; Android ComposerExpand.kt, iOS ShellComposerBar):
// up to a line limit taken from the visible height the box only grows; past it, it stops, scrolls inside, and a small
// ↗↙ at the text's top right opens the draft full screen. Desktop keeps its own box (INPUT_MAX, no button).

/** Where the compact composer applies: a phone-width screen, or a touch screen narrower than the sidebar breakpoint
 *  (a phone on its side). Desktop layouts keep the box they have. */
export const COMPACT_COMPOSER = "(max-width: 639px), (hover: none) and (max-width: 1023px)";

/** Share of the visible height (above the keyboard) the box may grow into before it scrolls and offers Expand. */
const GROW_SHARE = 0.22;
const MIN_LINES = 3;
const MAX_LINES = 8;

/** Lines the box grows to: 22% of what is visible in lines of `lineHeight` (iOS ComposerGrowth's share and cap), so a
 *  small phone with its keyboard up gets about 3 and a tall screen more, between 3 and 8. */
export function growLines(visible: number, lineHeight: number): number {
  if (!(visible > 0) || !(lineHeight > 0)) return MIN_LINES;
  return Math.min(MAX_LINES, Math.max(MIN_LINES, Math.floor((visible * GROW_SHARE) / lineHeight)));
}

/** The draft's wrapped lines, from the text's height (the box's scrollHeight less its vertical padding). */
export function lineCount(contentHeight: number, lineHeight: number): number {
  if (!(lineHeight > 0)) return 1;
  return Math.max(1, Math.round(contentHeight / lineHeight));
}

/** Expand shows once the draft has more lines than the box shows. With the button the text wraps narrower (more
 *  lines), so whichever state it lands in holds: it never flips back by itself. */
export const offersExpand = (lines: number, limit: number) => lines > limit;
