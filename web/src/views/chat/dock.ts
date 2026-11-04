// What sits under the chat on a phone (#1996): the composer, and above it the cards waiting on you, the running status,
// the queue. Without a limit, two or three cards (or a keyboard) pushed the conversation away and the composer off the
// screen. The heights follow the visible viewport (`--kb-h`, lib/viewport.ts), so they shrink with the keyboard.
import { interactionAsk, interactionTitle } from "../../lib/inbox";
import type { Interaction } from "../../lib/types";
import type { Segment } from "../../lib/turns";

/** A share of the visible height, capped at `cap` px, as a CSS length. */
export function visibleShare(share: number, cap?: number): string {
  const part = `calc(var(--kb-h) * ${share})`;
  return cap === undefined ? part : `min(${cap}px, ${part})`;
}

/** Everything under the chat on a narrow screen: at most about half of what is visible. */
export const DOCK_MAX = visibleShare(0.55);
/** The composer's text box: 240px, less when little is visible, so the send button stays on screen. */
export const INPUT_MAX = visibleShare(0.35, 240);
/** A plan card's plan (it scrolls inside). */
export const PLAN_MAX = visibleShare(0.4);

export interface CardFold {
  /** Shown in full: the first `full` cards. */
  full: number;
  /** The one line in their place, or nothing: 还有 N 件 under the first (`more`), or N 件待处理 for all of them while
   *  typing (`typing`: tapping it puts the keyboard away) or on a phone on its side (`short`: tapping it opens them). */
  summary: { kind: "more" | "typing" | "short"; count: number } | null;
}

/**
 * Which of the cards waiting on you show in full. Wide screens: all of them. Narrow ones: the first, and 还有 N 件 for
 * the others until that is tapped (`open`); while the keyboard is up, or on a phone on its side (`short`) until tapped,
 * none: one line in their place.
 */
/** Whether the answered cards (their receipts) show above the composer: not while the cards are folded to one line
 *  (typing, or a short screen not opened), which is about the room the keyboard leaves. */
export function showsAnswered({ narrow, keyboard, open, short = false }: { narrow: boolean; keyboard: boolean; open: boolean; short?: boolean }): boolean {
  return !narrow || (!keyboard && (open || !short));
}

export function cardFold(count: number, { narrow, keyboard, open, short = false }: { narrow: boolean; keyboard: boolean; open: boolean; short?: boolean }): CardFold {
  if (!narrow || count === 0) return { full: count, summary: null };
  if (keyboard) return { full: 0, summary: { kind: "typing", count } };
  if (open) return { full: count, summary: null };
  if (short) return { full: 0, summary: { kind: "short", count } };
  if (count === 1) return { full: 1, summary: null };
  return { full: 1, summary: { kind: "more", count: count - 1 } };
}

/** A card in one line (the folded cards' line): what it asks, as the inbox says it, else what it waits for. */
export function cardLine(ix: Interaction): string {
  return interactionAsk(ix) || interactionTitle(ix);
}

/** When the turn going on started: its message, else its first item (SegmentView's 正在工作… counts from the same). */
export function turnStart(seg: Pick<Segment, "user" | "start"> | undefined): string | undefined {
  return seg?.user?.ts ?? seg?.start;
}
