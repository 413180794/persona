// Derived lists the sidebar, the home page and the palette share.
import { useMemo } from "react";
import { stateOf } from "./format";
import { buildSpaces, splitAgentSessions, type Space } from "./agent-groups";
import { useStore } from "./store";
import type { Session } from "./types";

export type { Space };

export function useAgentSessions(): Session[] {
  const by = useStore((s) => s.sessions);
  return useMemo(() => Object.values(by).flat().filter((s) => !s.assistant), [by]);
}

export function useSpaces(): { spaces: Space[]; loose: Session[]; recent: Session[] } {
  const all = useAgentSessions();
  const machines = useStore((s) => s.machines);
  return useMemo(() => {
    const { live, recent } = splitAgentSessions(all, machines);
    return { ...buildSpaces(live, machines), recent };
  }, [all, machines]);
}

export function useCounts() {
  const all = useAgentSessions();
  const interactions = useStore((s) => s.interactions.length);
  return useMemo(() => {
    const c = { needs: 0, working: 0, unread: 0 };
    for (const s of all) {
      const st = stateOf(s);
      if (st === "needs_you") c.needs++;
      else if (st === "working") c.working++;
      else if (st === "unread") c.unread++;
    }
    return { ...c, interactions };
  }, [all, interactions]);
}
