// 全部对话 (docs/assistant.md「管理自己的对话」): the assistant's conversations, the main one first, the rest newest first
// by day; search on the title and the last message; the archived ones apart. Pure functions, tested without a DOM.
import type { Session } from "../../lib/types";
import { t } from "../../i18n";

/** A conversation as `GET /api/v1/assistant?archived=1` lists it. */
export type Conversation = Session & { archived?: boolean };

/** "1101 | 修复 | 搜索框和顶栏重叠" (docs/assistant.md「对话标题」) → head "1101 | 修复 | " and topic; null for other titles. */
export function splitTitle(title: string): { head: string; topic: string } | null {
  const m = /^(\d{4}\s*\|\s*[^|]+?\s*\|\s*)(.+)$/.exec(title.trim());
  return m ? { head: m[1], topic: m[2] } : null;
}

/** What a row calls it: an untitled one (empty, or Claude Code's placeholder) is 新对话. */
/** The main conversation's title: it has none of its own (the hub refuses renaming it), whatever its first message was (#1313). */
export const MAIN_TITLE = "Persona";

export function conversationTitle(s: Pick<Session, "title">, isMain = false): string {
  if (isMain) return MAIN_TITLE;
  const title = s.title?.trim();
  return !title || title === "Claude Code" || title === "assistant" ? t("assistant-conversations.untitled") : title;
}

const fold = (s: string) => s.normalize("NFKC").toLowerCase();

/** Matches the title or the last message, ignoring case and full / half width. */
export function filterConversations<T extends Conversation>(list: T[], query: string): T[] {
  const q = fold(query.trim());
  if (!q) return list;
  return list.filter((s) => fold(conversationTitle(s)).includes(q) || fold(s.last_text ?? "").includes(q));
}

function time(s: Session): number {
  const at = s.updated_at ? Date.parse(s.updated_at) : NaN;
  return Number.isFinite(at) && at > 86_400_000 ? at : 0;
}

/** Newest first, a fixed order for equal times. */
export function newestFirst<T extends Session>(list: T[]): T[] {
  return list.slice().sort((a, b) => time(b) - time(a) || a.session_id.localeCompare(b.session_id));
}

/** 今天 / 昨天 / 最近 7 天 / 更早 (and those without a time), newest first. */
export function groupByDay<T extends Session>(list: T[], now: Date): { title: string; items: T[] }[] {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1).getTime();
  const week = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 7).getTime();
  const out: { title: string; items: T[] }[] = [];
  for (const s of newestFirst(list)) {
    const at = time(s);
    const title = !at ? t("assistant-conversations.earlier") : at >= today ? t("assistant-conversations.today") : at >= yesterday ? t("assistant-conversations.yesterday") : at >= week ? t("assistant-conversations.last_7_days") : t("assistant-conversations.earlier");
    if (out.length && out[out.length - 1].title === title) out[out.length - 1].items.push(s);
    else out.push({ title, items: [s] });
  }
  return out;
}

/** One list from what was fetched with the archived ones (`fetched`), the store's list (newer conversations, without
 *  the archived ones) and the live sessions pushed since (state, title, last message; `archived` when the hub sends it). */
export function mergeConversations(fetched: Conversation[], listed: Session[], live: Session[]): Conversation[] {
  const by = new Map<string, Conversation>();
  for (const s of fetched) by.set(s.session_id, s);
  for (const s of listed) by.set(s.session_id, { ...by.get(s.session_id), ...s });
  for (const s of live) {
    const had = by.get(s.session_id);
    if (had) by.set(s.session_id, { ...had, ...s });
  }
  return [...by.values()];
}

/** The main conversation; the others (not archived), by day; the archived ones, newest first. */
export function arrange(list: Conversation[], main: string | undefined, now: Date) {
  const mainConv = list.find((s) => s.session_id === main && !s.archived);
  const rest = list.filter((s) => s.session_id !== mainConv?.session_id);
  return {
    main: mainConv,
    days: groupByDay(rest.filter((s) => !s.archived), now),
    archived: newestFirst(rest.filter((s) => s.archived)),
  };
}
