// Derived lists the sidebar, the home page and the palette share.
import { useMemo } from "react";
import { stateOf } from "./format";
import { useStore } from "./store";
import type { Session } from "./types";

export function useAgentSessions(): Session[] {
  const by = useStore((s) => s.sessions);
  return useMemo(() => Object.values(by).flat().filter((s) => !s.assistant), [by]);
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
  return useMemo(() => ({ ...stateCounts(all), interactions }), [all, interactions]);
}
