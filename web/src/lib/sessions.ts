// Derived lists the sidebar, the home page and the palette share.
import { useMemo } from "react";
import { byAttention, stateOf } from "./format";
import { splitAgentSessions } from "./agent-groups";
import { useStore } from "./store";
import type { Session } from "./types";

export interface Space {
  key: string;
  machineId: string;
  name: string;
  sessions: Session[];
}

export function useAgentSessions(): Session[] {
  const by = useStore((s) => s.sessions);
  return useMemo(() => Object.values(by).flat().filter((s) => !s.assistant), [by]);
}

export function useSpaces(): { spaces: Space[]; loose: Session[]; recent: Session[] } {
  const all = useAgentSessions();
  const machines = useStore((s) => s.machines);
  return useMemo(() => {
    const { live, recent } = splitAgentSessions(all, machines);
    const map = new Map<string, Space>();
    const loose: Session[] = [];
    for (const s of live) {
      if (!s.space_id) {
        loose.push(s);
        continue;
      }
      const key = `${s.machine_id}/${s.space_id}`;
      if (!map.has(key)) map.set(key, { key, machineId: s.machine_id, name: s.space_name || s.space_id, sessions: [] });
      map.get(key)!.sessions.push(s);
    }
    const order = (m: string) => machines.findIndex((x) => x.id === m);
    // The sidebar keeps herdr's order (machine, workspace, tab): rows that jump around as agents change state
    // are hard to click. What needs attention is on the home page and in the state dots.
    const bySlot = (a: Session, b: Session) => (a.slot_index ?? 0) - (b.slot_index ?? 0) || a.session_id.localeCompare(b.session_id);
    const spaces = [...map.values()].map((sp) => ({ ...sp, index: sp.sessions[0]?.space_index ?? 0, sessions: sp.sessions.sort(bySlot) }));
    spaces.sort((a, b) => order(a.machineId) - order(b.machineId) || a.index - b.index || a.name.localeCompare(b.name));
    return { spaces, loose: loose.sort(byAttention), recent };
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
