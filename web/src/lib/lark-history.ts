// Older pages of a Feishu chat, loaded as the list nears its top (iOS LarkHistoryPaging, #1033), and where the list
// stands against its newest end for 回到最新 (iOS LarkFollow / LarkPager.latest, #1034). Pure logic; LarkThread wires it.
import type { LarkMessage, LarkPage } from "./inbox";

/** Why older paging stopped: it waits for a click on the header line. */
export type HistoryProblem = "failed" | "stopped" | "account_changed" | "signed_out";

export function historyProblem(code: string | undefined): HistoryProblem {
  switch (code) {
    case "lark_account_changed": return "account_changed";
    case "lark_not_logged_in": return "signed_out";
    default: return "failed";
  }
}

/** One older page at a time. A failed or non-progressing page waits for an explicit retry; `reset` (another chat,
 * a replaced window) invalidates a reply still on its way. */
export class OlderPaging {
  private pending: { id: number; cursor: string } | null = null;
  private seq = 0;
  private visited = new Set<string>();
  problem: HistoryProblem | null = null;

  get loading(): boolean {
    return this.pending !== null;
  }

  /** A request for the page before `cursor`, or null: one is on its way, there is none, or it stopped (unless retry). */
  begin(cursor: string | undefined, retry = false): { id: number; cursor: string } | null {
    if (this.pending || !cursor || (this.problem && !retry)) return null;
    this.pending = { id: ++this.seq, cursor };
    this.problem = null;
    return this.pending;
  }

  fail(req: { id: number }, problem: HistoryProblem): boolean {
    if (this.pending?.id !== req.id) return false;
    this.pending = null;
    this.problem = problem;
    return true;
  }

  /** The page to merge in front of `have`, or null for a reply from an earlier window. Rows already shown are
   * dropped; an empty or duplicate page that still claims more, or a cursor seen before, stops paging (keeping what
   * it brought) and leaves the same boundary to retry, so it never loops. */
  finish(req: { id: number; cursor: string }, page: LarkPage, have: LarkMessage[]): LarkPage | null {
    if (this.pending?.id !== req.id) return null;
    this.pending = null;
    this.visited.add(req.cursor);
    const shown = new Set(have.map((m) => m.id));
    const messages = (page.messages ?? []).filter((m) => !shown.has(m.id));
    const next = page.older_cursor ?? messages[0]?.position;
    let older_cursor = page.older_cursor;
    if (page.has_older && (!messages.length || !next || this.visited.has(next))) {
      this.problem = "stopped";
      older_cursor = req.cursor;
    }
    return { ...page, messages, older_cursor };
  }

  reset(): void {
    this.pending = null;
    this.problem = null;
    this.visited.clear();
  }
}

/** The list is within half a screen of its top: time for the next older page. */
export function nearTop(el: { scrollTop: number; clientHeight: number }): boolean {
  return el.scrollTop <= el.clientHeight / 2;
}

/** How far the list is from its newest end, in screens. */
export function screensFromEnd(el: { scrollTop: number; clientHeight: number; scrollHeight: number }): number {
  if (el.clientHeight <= 0) return 0;
  return Math.max(0, el.scrollHeight - el.clientHeight - el.scrollTop) / el.clientHeight;
}

/** 回到最新 shows when the window is not at the newest messages, or the list is more than a screen up from them. */
export function showsJumpToLatest(hasNewer: boolean | undefined, el: { scrollTop: number; clientHeight: number; scrollHeight: number }): boolean {
  return Boolean(hasNewer) || screensFromEnd(el) > 1;
}

/** How many of the newest messages are kept for an instant 回到最新 (iOS keeps 80). */
export const LATEST_KEPT = 80;

/** The newest messages of a window that reaches the newest end, kept to go back to without waiting. */
export function latestOf(page: LarkPage): LarkPage {
  const messages = page.messages.slice(-LATEST_KEPT);
  const cut = messages.length < page.messages.length;
  return { ...page, messages, has_newer: false, newer_cursor: undefined, has_older: page.has_older || cut,
    older_cursor: cut ? messages[0]?.position : page.older_cursor };
}
