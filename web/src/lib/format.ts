import type { AgentState, Session, SessionCapabilities } from "./types";

// Missing session metadata is not permission to write; legacy sources retain their existing behavior.
export function sessionCan(s: Session | undefined, operation: keyof SessionCapabilities): boolean {
  if (!s) return false;
  if (operation === "attach") return Boolean(s.source_kind && s.capabilities?.attach === true && !s.attached);
  return !s.source_kind || s.capabilities?.[operation] === true;
}

export function sourceSubtitle(s?: Session): string {
  if (!s?.source_kind) return "";
  const label = s.source_kind === "codex_app_server" ? "Codex 实时" : s.source_kind === "codex_history" ? "Codex 历史" : s.source_kind === "identity_conflict" ? "身份冲突" : s.source_kind;
  const detail = s.source_kind !== "identity_conflict" && s.agent_name ? ` · ${s.agent_name}` : "";
  return sessionCan(s, "send") ? label + detail : `${label}${detail} · 仅查看：${s.read_only_reason || "此来源尚未允许发送"}`;
}

export function resumeCommand(s: Session): string {
  const codex = s.agent === "codex" || s.source_kind === "codex_app_server" || s.source_kind === "codex_history";
  const command = codex ? "codex resume" : s.agent === "traex" ? "traex resume" : "claude --resume";
  return `cd ${s.cwd} && ${command} ${s.source_thread_id || s.session_id}`;
}

export function sessionMenuActions(s: Session): string[] {
  return [
    ...(sessionCan(s, "attach") ? ["attach"] : []),
    ...(stateOf(s) === "working" && sessionCan(s, "interrupt") ? ["stop"] : []),
    ...(sessionCan(s, "terminal") ? ["resume", ...(!s.assistant && stateOf(s) !== "offline" ? ["close"] : [])] : []),
  ];
}

export const STATE_LABEL: Record<AgentState, string> = {
  needs_you: "需要你",
  working: "工作中",
  unread: "完成未看",
  idle: "已看",
  offline: "未运行",
  unknown: "未知",
};

// docs/agent-states.md: needs you → unread → working → idle → unknown → offline
const RANK: Record<AgentState, number> = { needs_you: 0, unread: 1, working: 2, idle: 3, unknown: 4, offline: 5 };

export function stateOf(s: Session): AgentState {
  if (s.state) return s.state;
  if (s.status === "offline") return "offline";
  if (s.status === "working") return "working";
  if (s.status === "blocked" || (s.pending_interactions ?? 0) > 0) return "needs_you";
  return "idle";
}

export function stateColor(st: AgentState): string {
  switch (st) {
    case "needs_you":
      return "var(--status-needs)";
    case "working":
      return "var(--status-working)";
    case "unread":
      return "var(--status-unread)";
    case "offline":
      return "var(--line-strong)";
    default:
      return "var(--status-idle)";
  }
}

export function byAttention(a: Session, b: Session): number {
  const d = RANK[stateOf(a)] - RANK[stateOf(b)];
  if (d) return d;
  const ta = stateOf(a) === "working" ? a.state_since : a.updated_at;
  const tb = stateOf(b) === "working" ? b.state_since : b.updated_at;
  return (tb ?? "").localeCompare(ta ?? "") || a.session_id.localeCompare(b.session_id);
}

export function isLive(s: Session) {
  return stateOf(s) !== "offline";
}

export function sessionTitle(s: Pick<Session, "title" | "cwd">): string {
  return s.title?.trim() || baseName(s.cwd) || "未命名会话";
}

export function baseName(p?: string): string {
  if (!p) return "";
  const parts = p.replace(/\/+$/, "").split("/");
  return parts[parts.length - 1] || p;
}

export function tabLabel(s: Session): string {
  const n = s.slot_name?.trim();
  if (!n) return s.slot_index ? `#${s.slot_index}` : "";
  return /^\d+$/.test(n) ? `#${n}` : n;
}

export function shortPath(p?: string): string {
  if (!p) return "";
  return p.replace(/^\/(Users|home|data\d*\/home)\/[^/]+/, "~");
}

const rtf = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });

export function ago(iso?: string, now = Date.now()): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (!Number.isFinite(t) || t < 86400_000) return "";
  const s = Math.round((now - t) / 1000);
  if (s < 45) return "刚刚";
  if (s < 3600) return rtf.format(-Math.round(s / 60), "minute");
  if (s < 86400) return rtf.format(-Math.round(s / 3600), "hour");
  if (s < 86400 * 7) return rtf.format(-Math.round(s / 86400), "day");
  return new Date(t).toLocaleDateString("zh-CN", { month: "numeric", day: "numeric" });
}

export function duration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s} 秒`;
  const m = Math.floor(s / 60);
  if (m < 60) return s % 60 ? `${m} 分 ${s % 60} 秒` : `${m} 分钟`;
  const h = Math.floor(m / 60);
  return m % 60 ? `${h} 小时 ${m % 60} 分` : `${h} 小时`;
}

// For a running timer in a list: minutes are enough once past one.
export function shortDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s} 秒`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分钟`;
  return m % 60 ? `${Math.floor(m / 60)} 小时 ${m % 60} 分` : `${m / 60} 小时`;
}

export function clock(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const today = new Date();
  const hm = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  if (d.toDateString() === today.toDateString()) return hm;
  return `${d.getMonth() + 1}月${d.getDate()}日 ${hm}`;
}

export function bytes(n?: number): string {
  if (n === undefined || !Number.isFinite(n)) return "—";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${u[i]}`;
}

export function uuid(): string {
  return crypto.randomUUID?.() ?? `${Date.now().toString(16)}-${Math.random().toString(16).slice(2)}`;
}
