// A `/s/<mid>/<sid>` link whose sid may be only the front of a session id: the assistant, notifications and
// agents_list show 8-character ids, and links get written with them (#2464). The node finds transcripts by the whole
// id only, so the page settles the prefix against this machine's session list first.

import { t } from "../i18n";
import { tabOfSessionLink } from "./herdr-tree";
import type { Route, SessionView } from "./router";
import type { Session } from "./types";

/** The shortest prefix that is looked up; anything shorter is taken as a whole id. */
export const MIN_PREFIX = 8;

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export type SessionLink =
  /** The id is a whole one: listed, or not on the list but shaped like one (the node may still have it). */
  | { kind: "exact" }
  /** One session on the machine starts with it: open that one under its whole id. */
  | { kind: "prefix"; session: Session }
  /** Several do: the person picks, newest first. */
  | { kind: "ambiguous"; candidates: Session[] }
  /** The list is not in yet and the id is not whole: wait for it rather than ask the node for a prefix. */
  | { kind: "pending" }
  /** Not on the list at all: asked of the node as it is, which says it's not found when it isn't there. */
  | { kind: "unlisted" };

export function resolveSessionLink(all: Session[], mid: string, sid: string, loaded: boolean): SessionLink {
  const mine = all.filter((s) => s.machine_id === mid);
  if (mine.some((s) => s.session_id === sid)) return { kind: "exact" };
  if (UUID_RE.test(sid)) return { kind: "exact" };
  if (!loaded) return { kind: "pending" };
  if (sid.length < MIN_PREFIX) return { kind: "unlisted" };
  const candidates = mine.filter((s) => s.session_id.startsWith(sid));
  if (candidates.length === 1) return { kind: "prefix", session: candidates[0] };
  if (candidates.length > 1) return { kind: "ambiguous", candidates: candidates.sort(newestFirst) };
  return { kind: "unlisted" };
}

function newestFirst(a: Session, b: Session): number {
  return stamp(b) - stamp(a) || a.session_id.localeCompare(b.session_id);
}

function stamp(s: Session): number {
  const t = Date.parse(s.activity_at || s.updated_at || "");
  return Number.isFinite(t) ? t : 0;
}

const NOT_FOUND_RE = /\b(?:transcript for session|subagent) \S+ not found\b/i;

/** What to say when a conversation cannot be read: "not found" from the node in words, anything else as it is. */
export function sessionLoadError(text: string): string {
  return NOT_FOUND_RE.test(text) ? t("lib-session-link.not_found") : text;
}

/** Where a session's link really goes: its tab's `/t/…` when it has one, else `/s/<mid>/<whole id>`. */
export function sessionLinkRoute(all: Session[], s: Session, view?: SessionView): Route {
  return tabOfSessionLink(all, { mid: s.machine_id, sid: s.session_id, view }) ?? { name: "session", mid: s.machine_id, sid: s.session_id, ...(view && { view }) };
}
