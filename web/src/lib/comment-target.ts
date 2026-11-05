// Which message box takes a comment made in a side panel (#2437), and what is said when it lands there. A session's
// panel (files, Git) comments into that session's box; a panel without one (a path opened from a message) uses the box
// last used. The box does not have to be on screen: the comments wait in the quote list kept for its draft key.
import type { CodeSpot } from "./comments";
import { addQuote, getQuotes, type DraftQuote } from "./drafts";
import { sessionCan } from "./format";
import { stripBase } from "./base";
import { activeComposer } from "./links";
import { parse, navigate } from "./router";
import { readView } from "./session-view";
import { getSession } from "./store";
import { toast } from "../ui/atoms";
import { t } from "../i18n";

/** The draft key of the box a comment made for `sid` goes to; null when there is none (or the session takes no messages). */
export function commentKey(mid: string, sid?: string): string | null {
  if (sid) {
    const s = getSession(mid, sid);
    return s && !sessionCan(s, "send") ? null : `${mid}/${sid}`;
  }
  return activeComposer()?.key ?? null;
}

/** The comments of the list as (number, quote) pairs: the number a card carries. */
export function numbered(list: readonly DraftQuote[]): { n: number; q: DraftQuote }[] {
  let n = 0;
  return list.filter((q) => q.comment).map((q) => ({ n: ++n, q }));
}

/** Line → card number for the places in a file or diff that have a comment waiting (`old`: the numbers of a diff's old side). */
export function markedLines(list: readonly DraftQuote[], here: (spot: CodeSpot) => boolean, old = false): Map<number, number> {
  const out = new Map<number, number>();
  for (const { n, q } of numbered(list)) {
    const spot = q.comment?.spot;
    if (spot && Boolean(spot.old) === old && here(spot)) for (let l = spot.from; l <= spot.to && l < spot.from + 200; l++) if (!out.has(l)) out.set(l, n);
  }
  return out;
}

/** Adds a finished code comment to the box of `key` and says so; on a terminal page the toast offers the conversation. */
export function addCodeComment(key: string, quote: DraftQuote, body: string): number {
  const full: DraftQuote = { ...quote, comment: { ...quote.comment!, body } };
  addQuote(key, full, false);
  const n = numbered(getQuotes(key).list).length;
  const [mid, ...rest] = key.split("/");
  const sid = rest.join("/");
  const r = parse(stripBase(location.pathname), location.search.replace(/^\?/, ""));
  const onPage = (r.name === "session" && r.mid === mid && r.sid === sid) || (r.name === "tab" && r.sid === sid);
  const term = onPage && ((r.name === "session" || r.name === "tab" ? r.view : undefined) ?? readView(mid, sid)) === "term";
  if (term) toast(t("chat-comments.added_terminal", { n }), "ok", { label: t("chat-comments.show_chat"), run: () => navigate({ ...r, view: "chat" } as typeof r, true) });
  else toast(t("chat-comments.added", { n }), "ok");
  return n;
}
