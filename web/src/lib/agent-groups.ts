import { byAttention, stateOf } from "./format";
import type { AgentState, Machine, Session } from "./types";

export type AgentGrouping = "space" | "state" | "machine";
export const AGENT_GROUPINGS: { key: AgentGrouping; label: string }[] = [
  { key: "space", label: "按项目" },
  { key: "state", label: "按状态" },
  { key: "machine", label: "按机器" },
];

export function agentGrouping(value: string | null): AgentGrouping {
  return AGENT_GROUPINGS.find((g) => g.key === value)?.key ?? "space";
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
    return machines.map((m) => ({ key: `machine:${m.id}`, name: m.name, sessions: live.filter((s) => s.machine_id === m.id).sort(byAttention) })).filter((g) => g.sessions.length);
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
