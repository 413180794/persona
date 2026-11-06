// The safety net under a chat's live stream (#3139): the page follows a session by WebSocket pushes, and when one is lost
// (a socket that looks open but is not, a queue the hub had to drop) it used to stay behind until a reload. The session
// list keeps coming over the same socket and says when the transcript last grew (`updated_at`): when that is newer than
// the newest item the page holds and no items have come for a while, the page asks for what it is missing once
// (`items?after_seq=`), applies it silently, and only says "Not live" when that keeps being needed.

/** No items for this long before a page that looks behind asks. */
export const STALE_QUIET_MS = 4000;
/** At most one ask this often per session. */
export const STALE_PULL_EVERY_MS = 10000;
/** The list's `updated_at` is the file's mtime and an item's `ts` the line's own clock: closer than this is the same write. */
export const STALE_SLACK_MS = 1000;
/** Asks in a row that found what the stream should have brought (or failed) before the page says it is not live. */
export const STALE_HINT_AFTER = 2;

export interface StaleView {
  /** The session's `updated_at` as the list has it now. */
  updatedAt?: string;
  /** The newest item the page holds, in ms (0: none, or none with a time). */
  lastItemAt: number;
  /** When items last came (a push, a page, an ask), in ms. */
  lastArrival: number;
  /** When this session was last asked for, in ms (0: never). */
  lastPull: number;
  /** The `updated_at` the last ask was for: a write that added no items (a title, a mode) is not asked about twice. */
  checkedFor?: string;
  now: number;
  visible: boolean;
  /** An ask is running. */
  pulling: boolean;
}

/** Whether to ask for what the page may be missing now. */
export function shouldPull(v: StaleView): boolean {
  if (!v.visible || v.pulling || !v.updatedAt || v.updatedAt === v.checkedFor) return false;
  const updated = Date.parse(v.updatedAt);
  if (!Number.isFinite(updated) || updated <= v.lastItemAt + STALE_SLACK_MS) return false;
  return v.now - v.lastArrival >= STALE_QUIET_MS && v.now - v.lastPull >= STALE_PULL_EVERY_MS;
}

/** What the asks so far say about the stream. */
export interface StaleTally {
  /** Asks in a row that brought items the push should have. */
  recovered: number;
  /** Asks in a row that failed. */
  failed: number;
}

export const FRESH_TALLY: StaleTally = { recovered: 0, failed: 0 };

export type PullOutcome = "found" | "empty" | "failed";

export function afterPull(t: StaleTally, outcome: PullOutcome): StaleTally {
  if (outcome === "failed") return { recovered: t.recovered, failed: t.failed + 1 };
  return { recovered: outcome === "found" ? t.recovered + 1 : t.recovered, failed: 0 };
}

/** The stream brought items itself: it works. */
export function afterPush(t: StaleTally): StaleTally {
  return t.recovered || t.failed ? FRESH_TALLY : t;
}

/** Whether the page should say it is not live. */
export function notLive(t: StaleTally): boolean {
  return t.recovered >= STALE_HINT_AFTER || t.failed >= STALE_HINT_AFTER;
}

/** The time of the newest item with one, in ms (0 when none has). */
export function newestItemAt(items: readonly { ts?: string }[]): number {
  for (let i = items.length - 1; i >= 0; i--) {
    const at = Date.parse(items[i].ts ?? "");
    if (Number.isFinite(at)) return at;
  }
  return 0;
}
