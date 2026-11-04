import { byAttention, stateOf } from "./format";
import { hubLabel } from "./mesh";
import type { AgentState, Machine, Session } from "./types";

export type AgentGrouping = "repo" | "space" | "state" | "machine" | "todo";
export const AGENT_GROUPINGS: { key: AgentGrouping; label: string }[] = [
  { key: "repo", label: "按仓库" },
  { key: "space", label: "按项目" },
  { key: "state", label: "按状态" },
  { key: "machine", label: "按机器" },
  { key: "todo", label: "按待办" },
];

export function agentGrouping(value: string | null): AgentGrouping {
  return AGENT_GROUPINGS.find((g) => g.key === value)?.key ?? "repo";
}

export interface AgentMutes {
  spaces: string[];
  sessions: string[];
  allowed?: string[];
}

export interface AgentGroup {
  key: string;
  name: string;
  sessions: Session[];
  state?: AgentState;
  folded?: boolean;
  /** The machine's hub, only when the list spans more than one (docs/plan-hub-mesh.md 2.6). */
  hub?: string;
  todoId?: string;
  todoStatus?: string;
}

export function splitAgentSessions(all: Session[], machines: Machine[]) {
  const online = new Set(machines.filter((m) => m.online).map((m) => m.id));
  const agents = all.filter((s) => !s.assistant);
  const live = agents.filter((s) => stateOf(s) !== "offline" && online.has(s.machine_id));
  const recent = agents
    .filter((s) => stateOf(s) === "offline" || !online.has(s.machine_id))
    .sort((a, b) => (b.updated_at ?? "").localeCompare(a.updated_at ?? "") || a.machine_id.localeCompare(b.machine_id) || a.session_id.localeCompare(b.session_id))
    .slice(0, 40);
  return { live, recent };
}

export interface Space {
  key: string;
  machineId: string;
  name: string;
  sessions: Session[];
}

/** Live agents by herdr space, in herdr's order (machine, workspace, tab); agents outside any space are `loose`. The
 * sidebar keeps that order: rows that jump around as agents change state are hard to click. */
export function buildSpaces(live: Session[], machines: Machine[]): { spaces: Space[]; loose: Session[] } {
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
  const bySlot = (a: Session, b: Session) => (a.slot_index ?? 0) - (b.slot_index ?? 0) || a.session_id.localeCompare(b.session_id);
  const spaces = [...map.values()].map((sp) => ({ ...sp, index: sp.sessions[0]?.space_index ?? 0, sessions: sp.sessions.sort(bySlot) }));
  spaces.sort((a, b) => order(a.machineId) - order(b.machineId) || a.index - b.index || a.name.localeCompare(b.name));
  return { spaces: spaces.map(({ index: _index, ...sp }) => sp), loose: loose.sort(byAttention) };
}

export function isWaiting(s: Session): boolean {
  return ["idle", "unknown"].includes(stateOf(s)) && Boolean(s.loop || s.background?.length || s.subagents?.some((a) => a.running));
}

export function isQuiet(s: Session): boolean {
  return ["idle", "unknown"].includes(stateOf(s)) && !isWaiting(s);
}

function isMuted(s: Session, mutes: AgentMutes): boolean {
  const key = `${s.machine_id}/${s.session_id}`;
  if (mutes.sessions.includes(key)) return true;
  if (mutes.allowed?.includes(key)) return false;
  return Boolean(s.space_name && mutes.spaces.includes(`${s.machine_id}/${s.space_name}`));
}

function byDate(a: Session, b: Session): number {
  const date = (s: Session) => (stateOf(s) === "working" ? s.state_since || s.updated_at : s.updated_at) ?? "";
  return date(b).localeCompare(date(a)) || a.session_id.localeCompare(b.session_id) || a.machine_id.localeCompare(b.machine_id);
}

// iOS AgentListOrder: needs you always wins, even when a session or its project is muted.
export function buildAgentGroups(all: Session[], machines: Machine[], grouping: "state" | "machine", mutes: AgentMutes = { spaces: [], sessions: [] }): AgentGroup[] {
  const { live } = splitAgentSessions(all, machines);
  if (grouping === "machine") {
    return machines
      .map((m) => ({ key: `machine:${m.id}`, name: m.name, hub: hubLabel(m, machines) || undefined, sessions: live.filter((s) => s.machine_id === m.id).sort(byAttention) }))
      .filter((g) => g.sessions.length);
  }
  const groups: AgentGroup[] = [
    { key: "state:needs", name: "需要你", state: "needs_you", sessions: [] },
    { key: "state:unread", name: "完成未看", state: "unread", sessions: [] },
    { key: "state:working", name: "工作中", state: "working", sessions: [] },
    { key: "state:waiting", name: "待唤醒", sessions: [] },
    { key: "state:muted", name: "已屏蔽", sessions: [] },
    { key: "state:idle", name: "已看", state: "idle", sessions: [], folded: true },
  ];
  for (const s of live) {
    const state = stateOf(s);
    const waiting = isWaiting(s);
    const index = state === "needs_you" ? 0 : isMuted(s, mutes) && (state === "working" || state === "unread" || waiting) ? 4
      : state === "unread" ? 1 : state === "working" ? 2 : waiting ? 3 : 5;
    groups[index].sessions.push(s);
  }
  return groups.filter((g) => g.sessions.length).map((g) => ({ ...g, sessions: g.sessions.sort(byDate) }));
}
