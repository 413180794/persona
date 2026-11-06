// Derived lists the sidebar, the home page and the palette share.
import { useMemo } from "react";
import { stateOf } from "./format";
import { useStore } from "./store";
import { SESSIONS_LIST_FEATURE } from "./session-list";
import type { Session } from "./types";

export function useAgentSessions(): Session[] {
  const by = useStore((s) => s.sessions);
  const summaries = useStore((s) => s.sessionSummaries);
  const bounded = useStore((s) => s.features.includes(SESSIONS_LIST_FEATURE));
  return useMemo(() => (bounded ? summaries : Object.values(by).flat()).filter((s) => !s.assistant), [by, summaries, bounded]);
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
