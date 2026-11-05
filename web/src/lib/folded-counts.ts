import { stateOf } from "./format";
import type { AgentState, Session } from "./types";

/** The states a folded sidebar row still counts, in the order it shows them: what needs you comes first (#3559). */
export const FOLDED_ORDER = [
  { key: "needs_you", state: "needs_you" },
  { key: "working", state: "working" },
  { key: "unread", state: "unread" },
] as const satisfies readonly { key: string; state: AgentState }[];

export type FoldedCounts = Record<(typeof FOLDED_ORDER)[number]["key"], number>;

/** How many of these sessions need you, are working and are done (and not yet seen). Idle and offline ones are not counted. */
export function foldedCounts(sessions: readonly Session[]): FoldedCounts {
  const counts: FoldedCounts = { needs_you: 0, working: 0, unread: 0 };
  for (const s of sessions) {
    const st = stateOf(s);
    if (st === "needs_you" || st === "working" || st === "unread") counts[st]++;
  }
  return counts;
}

/** Whether a folded row has anything to tally besides its plain count. */
export function hasFoldedCounts(c: FoldedCounts): boolean {
  return c.needs_you + c.working + c.unread > 0;
}
