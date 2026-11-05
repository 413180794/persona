// What the chat's ⋯ menu changes on a session: notifications (mutes), the run modes, the name
// (docs/protocol.md `/mutes`, `/modes`, `/rename`; iOS Pins.togglingMute, SessionModes).
import type { AgentMutes } from "./agent-groups";
import { sessionCan } from "./format";
import type { Session } from "./types";

export interface ModeOption {
  id: string;
  title: string;
  detail: string;
}

export const PERMISSION_MODES: ModeOption[] = [
  { id: "default", title: "手动确认", detail: "改文件、跑命令前都先问你" },
  { id: "acceptEdits", title: "自动改文件", detail: "改文件不问，跑命令还会问" },
  { id: "plan", title: "只做计划", detail: "只读代码、写计划，批准后才动手" },
  { id: "auto", title: "自动", detail: "自己判断风险，安全的直接做" },
  { id: "bypassPermissions", title: "全部放行", detail: "什么都不问；只能在新开时选" },
];

export const EFFORTS: ModeOption[] = [
  { id: "low", title: "低", detail: "最快，简单的事够用" },
  { id: "medium", title: "中", detail: "快一些" },
  { id: "high", title: "高", detail: "大多数任务的默认" },
  { id: "xhigh", title: "很高", detail: "更仔细，也更慢、更贵" },
  { id: "max", title: "最高", detail: "最慢最仔细" },
];

/** Codex's reasoning efforts (node/codex_effort.go); a thread offers those of its model, in `Session.efforts`. */
export const CODEX_EFFORTS: ModeOption[] = [
  { id: "minimal", title: "极低", detail: "几乎不思考，最快" },
  { id: "low", title: "低", detail: "快，简单的事够用" },
  { id: "medium", title: "中", detail: "快慢适中" },
  { id: "high", title: "高", detail: "更仔细" },
  { id: "xhigh", title: "很高", detail: "更仔细，也更慢、更贵" },
  { id: "max", title: "最高", detail: "最慢最仔细" },
  { id: "ultra", title: "极高", detail: "最仔细，还会主动分给多个子 Agent 一起做" },
];

/** Modes a running session can switch to: 全部放行 is chosen only when it starts. */
export const SWITCHABLE_MODES = PERMISSION_MODES.filter((m) => m.id !== "bypassPermissions");

export const BYPASS_MODE = "bypassPermissions";

/** What 全部放行 means, said in red where it is on (iOS's warning). */
export const BYPASS_WARNING = "全部放行：改文件、跑命令都不问你";

/** An assistant conversation (the main one too) switches to any mode, 全部放行 included: the hub restarts its
 * Claude with the new mode (#2416). Other agents keep SWITCHABLE_MODES. */
const ASSISTANT_MODES = PERMISSION_MODES.map((m) => (m.id === BYPASS_MODE ? { ...m, detail: "改文件、跑命令都不问你，有风险" } : m));

export function switchableModes(assistant: boolean | undefined): ModeOption[] {
  return assistant ? ASSISTANT_MODES : SWITCHABLE_MODES;
}

export function modeTitle(id?: string): string {
  return id ? (PERMISSION_MODES.find((m) => m.id === id)?.title ?? id) : "";
}

export function effortTitle(id?: string): string {
  return id ? (EFFORTS.find((m) => m.id === id)?.title ?? CODEX_EFFORTS.find((m) => m.id === id)?.title ?? id) : "";
}

/** Claude Code shows the two chips (its TUI's bottom bar); Codex, TRAEX and runtimes have no such switches. */
export function hasModes(s: Session | undefined): boolean {
  if (!s || s.assistant || s.source_kind) return false;
  return !s.agent || s.agent === "claude";
}

/** Only the effort chip: the node lists the levels it takes (`efforts`: a Codex thread on its app-server), and the phone may write to it. */
export function hasEffortOnly(s: Session | undefined): boolean {
  return Boolean(s && !s.assistant && !hasModes(s) && s.efforts?.length && sessionCan(s, "send"));
}

/** The effort menu's choices: the session's own levels when the node lists them, Claude Code's otherwise. */
export function effortOptions(s: Session): ModeOption[] {
  if (!s.efforts?.length) return EFFORTS;
  return s.efforts.map((id) => CODEX_EFFORTS.find((m) => m.id === id) ?? { id, title: id, detail: "" });
}

const key = (s: Pick<Session, "machine_id" | "session_id">) => `${s.machine_id}/${s.session_id}`;

/** Whether the hub sends this agent's notifications (the rule of GET /mutes). */
export function isMuted(s: Pick<Session, "machine_id" | "session_id" | "space_name">, m: AgentMutes): boolean {
  if (m.sessions.includes(key(s))) return true;
  if (m.allowed?.includes(key(s))) return false;
  return Boolean(s.space_name && m.spaces.includes(`${s.machine_id}/${s.space_name}`));
}

/** The mutes after flipping one agent: its own switch sits on top of its space's (iOS Pins.togglingMute). */
export function toggleMute(s: Pick<Session, "machine_id" | "session_id" | "space_name">, m: AgentMutes): AgentMutes {
  const k = key(s);
  const sessions = m.sessions.filter((x) => x !== k);
  const allowed = (m.allowed ?? []).filter((x) => x !== k);
  const spaceMuted = Boolean(s.space_name && m.spaces.includes(`${s.machine_id}/${s.space_name}`));
  if (isMuted(s, m)) return { spaces: m.spaces, sessions, allowed: spaceMuted ? [...allowed, k] : allowed };
  return { spaces: m.spaces, sessions: spaceMuted ? sessions : [...sessions, k], allowed };
}

/** The rename request (iOS RenameSessionAlert): only what changed; the herdr tab only for an agent in a herdr pane.
 * Returns null when nothing changed. */
export function renameBody(s: Session, tab: string, title: string): { tab_name?: string; title?: string } | null {
  const t = tab.trim(), n = title.trim();
  const body: { tab_name?: string; title?: string } = {};
  if (!s.assistant && s.slot_id && t && t !== (s.slot_name ?? "")) body.tab_name = t;
  if (n && n !== s.title) body.title = n;
  return Object.keys(body).length ? body : null;
}
