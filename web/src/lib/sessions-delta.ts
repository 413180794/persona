// How a machine's session list is kept from what the hub sends (docs/protocol/protocol.md "Session deltas", the same rules as the
// app's SessionPatch). An older hub sends the whole list every time; a hub with sessions_delta sends it once on
// connect, then the sessions that changed (whole) and the ids that went, and leaves the Codex history out of both —
// the page has that from GET /api/v1/sessions.
import type { Session } from "./types";

export const SESSIONS_DELTA_FEATURE = "sessions_delta";

/** A delta over `base`: changed sessions replace theirs in place, new ones go after, gone ones go. */
export function applySessionDelta(base: Session[], upsert: Session[], remove: string[]): Session[] {
  const gone = new Set(remove);
  const byID = new Map(upsert.map((s) => [s.session_id, s]));
  const out: Session[] = [];
  for (const s of base) {
    if (gone.has(s.session_id)) continue;
    const changed = byID.get(s.session_id);
    if (changed) byID.delete(s.session_id);
    out.push(changed ?? s);
  }
  for (const s of upsert) if (byID.has(s.session_id)) out.push(s); // new: in the order the hub sent them
  return out;
}

/** A whole list from the hub. From a delta hub it has no Codex history: the history the page already has stays. */
export function sessionSnapshot(list: Session[], over: Session[], deltaHub: boolean): Session[] {
  if (!deltaHub) return list;
  const ids = new Set(list.map((s) => s.session_id));
  return [...list, ...over.filter((s) => isCodexHistory(s) && !ids.has(s.session_id))];
}

/**
 * The list from GET /api/v1/sessions over what the page has. While a delta hub's socket is connected, what it pushed
 * is newer than this answer can be (the request went out before): only the Codex history comes from it, the history
 * the page has included. No push carries it, so this answer is the only way its state moves (seen, gone offline,
 * #2425). Otherwise the answer is the whole truth.
 */
export function sessionRefreshed(rest: Session[], over: Session[], connectedToDeltaHub: boolean): Session[] {
  if (!connectedToDeltaHub) return rest;
  const ids = new Set(over.map((s) => s.session_id));
  const fresh = new Map(rest.filter(isCodexHistory).map((s) => [s.session_id, s]));
  const history = rest.filter((s) => isCodexHistory(s) && !ids.has(s.session_id));
  // History that is gone from the machine goes, the rest is the hub's copy; the pushed sessions stay as pushed.
  const kept: Session[] = [];
  for (const s of over) {
    if (!isCodexHistory(s)) kept.push(s);
    else if (fresh.has(s.session_id)) kept.push(fresh.get(s.session_id)!);
  }
  return [...kept, ...history];
}

/** A Codex session known only from its rollout on the machine (`source_kind: codex_history`). */
export function isCodexHistory(s: Session): boolean {
  return s.source_kind === "codex_history";
}
