// Derived lists the sidebar, the home page and the palette share.
import { useEffect, useMemo, useState } from "react";
import { api, credentialOwner } from "./api";
import { stateOf } from "./format";
import { getState, onHubEvent, subscribe, useStore } from "./store";
import { SESSIONS_LIST_FEATURE } from "./session-list";
import type { Session } from "./types";

export function useAgentSessions(): Session[] {
  const by = useStore((s) => s.sessions);
  const summaries = useStore((s) => s.sessionSummaries);
  const bounded = useStore((s) => s.features.includes(SESSIONS_LIST_FEATURE));
  return useMemo(() => (bounded ? summaries : Object.values(by).flat()).filter((s) => !s.assistant), [by, summaries, bounded]);
}

const EMPTY_FULL_LIST = { sessions: [] as Session[], loaded: false, base: {} as Record<string, Session[]> };
const FULL_LIST_RETRY_MS = 5_000;
// Working agents emit list hints for every display update. Space complete reads from their completion
// so a large fallback list cannot be downloaded back-to-back while those hints keep arriving.
const FULL_LIST_REFRESH_MS = 3_000;

/** Pins and grouping need complete rows, including agents outside the bounded first page. Keep this
 * snapshot only while the legacy list is mounted; other views retain their bounded transport. */
export function useFullAgentSessions(): { sessions: Session[]; loaded: boolean } {
  const by = useStore((s) => s.sessions);
  const bounded = useStore((s) => s.features.includes(SESSIONS_LIST_FEATURE));
  const loaded = useStore((s) => s.loaded);
  const [full, setFull] = useState(EMPTY_FULL_LIST);
  useEffect(() => {
    if (!bounded || !loaded) return;
    const owner = credentialOwner();
    let stopped = false, pending = false, dirty = false, urgent = false;
    let nextReadAt = 0;
    let controller: AbortController | undefined;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let trailing: ReturnType<typeof setTimeout> | undefined;
    const read = async (immediate = false) => {
      if (stopped || !getState().loaded || credentialOwner() !== owner) return;
      if (pending) { dirty = true; urgent ||= immediate; return; }
      const delay = nextReadAt - Date.now();
      if (!immediate && delay > 0) {
        // Keep the deadline fixed: a continuous stream of hints must not postpone the trailing read.
        trailing ??= setTimeout(() => { trailing = undefined; void read(); }, delay);
        return;
      }
      clearTimeout(trailing);
      trailing = undefined;
      clearTimeout(retry);
      pending = true;
      controller = new AbortController();
      const base = getState().sessions;
      try {
        const result = await api<{ sessions: Session[] }>("/sessions", { signal: controller.signal });
        if (!stopped && getState().loaded && credentialOwner() === owner) {
          setFull({ sessions: (result.sessions ?? []).filter((s) => !s.assistant), loaded: true, base });
        }
      } catch {
        // A quiet hub may send no further hints after a transient failure, including the first read.
        if (!stopped) retry = setTimeout(() => { void read(true); }, FULL_LIST_RETRY_MS);
      } finally {
        pending = false;
        nextReadAt = Date.now() + FULL_LIST_REFRESH_MS;
        if (dirty) {
          const immediate = urgent;
          dirty = urgent = false;
          void read(immediate);
        }
      }
    };
    const off = ["sessions_list", "session", "sessions", "sessions_delta", "hello"].map((event) => onHubEvent(event, () => { void read(event === "hello"); }));
    // reset() publishes synchronously, before React cleans up the effect. Fence late responses then,
    // including a reset that keeps the same credentials, and release the previous account's rows.
    off.push(subscribe(() => {
      if (!getState().loaded || credentialOwner() !== owner) {
        stopped = true;
        controller?.abort();
        clearTimeout(retry);
        clearTimeout(trailing);
        setFull(EMPTY_FULL_LIST);
      }
    }));
    void read(true);
    return () => { stopped = true; controller?.abort(); clearTimeout(retry); clearTimeout(trailing); off.forEach((unsubscribe) => unsubscribe()); };
  }, [bounded, loaded]);
  return useMemo(() => {
    if (!bounded) return { sessions: Object.values(by).flat().filter((s) => !s.assistant), loaded };
    if (!loaded) return EMPTY_FULL_LIST;
    if (by === full.base) return full;
    // Reflect actions on retained details (rename, mark seen) without copying the complete list into the store.
    // Unchanged cached details may predate this HTTP snapshot, so only apply rows changed since its request.
    const key = (s: Session) => JSON.stringify([s.machine_id, s.session_id]);
    const before = new Map(Object.values(full.base).flat().map((s) => [key(s), s]));
    const current = new Map(Object.values(by).flat().map((s) => [key(s), s]));
    const sessions = full.sessions.filter((s) => !before.has(key(s)) || current.has(key(s))).map((s) => {
      const next = current.get(key(s));
      return next && next !== before.get(key(s)) ? next : s;
    });
    return { sessions, loaded: full.loaded };
  }, [bounded, loaded, full, by]);
}

/** The agents by state. `badge` is the sidebar's "Needs you" count: without quiet tasks' agents, which the main conversation
 * tells about (#1731). */
export function stateCounts(all: Session[]) {
  const c = { needs: 0, working: 0, unread: 0, badge: 0 };
  for (const s of all) {
    const st = stateOf(s);
    if (st === "needs_you") {
      c.needs++;
      if (!s.quiet) c.badge++;
    } else if (st === "working") c.working++;
    else if (st === "unread") c.unread++;
  }
  return c;
}

export function useCounts() {
  const all = useAgentSessions();
  const interactions = useStore((s) => s.interactions.length);
  const counts = useStore((s) => s.sessionCounts);
  const bounded = useStore((s) => s.features.includes(SESSIONS_LIST_FEATURE));
  return useMemo(() => {
    if (!bounded || !counts) return { ...stateCounts(all), interactions };
    const needs = counts.state.needs_you ?? 0;
    return { needs, working: counts.state.working ?? 0, unread: counts.state.unread ?? 0, badge: needs, interactions };
  }, [all, interactions, counts, bounded]);
}
