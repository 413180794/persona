import type { AgentState, Session, SessionCapabilities } from "./types";
import { formatDate, formatRelative, t } from "../i18n";
import { workLine } from "./work";

// Missing session metadata is not permission to write; legacy sources retain their existing behavior.
export function sessionCan(s: Session | undefined, operation: keyof SessionCapabilities): boolean {
  if (!s) return false;
  if (operation === "attach") return Boolean(s.source_kind && s.capabilities?.attach === true && !s.attached);
  return !s.source_kind || s.capabilities?.[operation] === true;
}

export function sourceSubtitle(s?: Session): string {
  if (!s?.source_kind) return "";
  const label = s.source_kind === "codex_app_server" ? t("lib-format.source_codex_live") : s.source_kind === "codex_history" ? t("lib-format.source_codex_history") : s.source_kind === "identity_conflict" ? t("lib-format.source_identity_conflict")
    : s.source_kind === "claude_bg" ? t("lib-format.source_claude_background")
    : s.source_kind === "runtime" ? s.agent_label || s.agent || "runtime" // an agent runtime's session (docs/runtimes.md): its own name
    : s.source_kind;
  const detail = s.source_kind !== "identity_conflict" && s.agent_name ? ` · ${s.agent_name}` : "";
  return sessionCan(s, "send") ? label + detail : t("lib-format.read_only", { source: label + detail, reason: s.read_only_reason || t("lib-format.read_only_default_reason") });
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

// 删除对话 only takes a conversation out of the list (the hub notes it; the transcript stays): offline ones, not the
// assistant's (they archive) and not one still starting (issue #801).
export function sessionDeletable(s: Session): boolean {
  return !s.assistant && s.status === "offline" && !s.starting;
}

export const STATE_LABEL: Record<AgentState, string> = {
  needs_you: t("lib-format.state_needs_you"),
  working: t("lib-format.state_working"),
  unread: t("lib-format.state_unread"),
  idle: t("lib-format.state_idle"),
  offline: t("lib-format.state_offline"),
  unknown: t("lib-format.state_unknown"),
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
  const ta = stateOf(a) === "working" ? a.state_since || a.updated_at : a.updated_at;
  const tb = stateOf(b) === "working" ? b.state_since || b.updated_at : b.updated_at;
  return (tb ?? "").localeCompare(ta ?? "") || a.session_id.localeCompare(b.session_id) || a.machine_id.localeCompare(b.machine_id);
}

export function isLive(s: Session) {
  return stateOf(s) !== "offline";
}

/** Why it waits for you, in its host's words (a botmux attention), while it needs you; a reason left over from before
 *  (offline, seen) stays out. */
export function attentionReason(s: Session): string | undefined {
  const reason = s.attention_reason?.trim();
  return reason && stateOf(s) === "needs_you" ? reason : undefined;
}

/** The line a list row shows for it: why it waits for you when its host says so, else why its host stopped it on an
 *  error, else where its work stands when the hub triaged its turn (docs/agent-work.md), else its last words (iOS
 *  listPreview). */
export function sessionPreview(s: Session): string {
  return attentionReason(s) ?? failureReason(s) ?? workLine(s) ?? s.last_text ?? "";
}

/** A failure notice without the warning sign botmux starts it with: the page draws its own (iOS Session.failureText). */
export function failureText(reason: string): string {
  return reason.trim().replace(/^[\u26A0\uFE0F\s]+/u, "").trim();
}

/** Why the host stopped the session's turn on an error it will not retry by itself (failure_reason), while the
 *  session is not working (iOS listPreview, FailureNote); undefined otherwise. */
export function failureReason(s: Session | undefined): string | undefined {
  if (!s?.failure_reason || stateOf(s) === "working") return undefined;
  return failureText(s.failure_reason) || undefined;
}

/** 在飞书打开: the session's place outside Persona, a web (https) link only (iOS externalLink). */
export function externalLink(s?: Pick<Session, "external_url">): string | undefined {
  const raw = s?.external_url?.trim();
  if (!raw) return undefined;
  try {
    return new URL(raw).protocol === "https:" ? raw : undefined;
  } catch {
    return undefined;
  }
}

export function sessionTitle(s: Pick<Session, "title" | "cwd">): string {
  return s.title?.trim() || baseName(s.cwd) || t("lib-format.untitled_session");
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

export function ago(iso?: string, now = Date.now()): string {
  if (!iso) return "";
  const ts = Date.parse(iso);
  if (!Number.isFinite(ts) || ts < 86400_000) return "";
  const s = Math.round((now - ts) / 1000);
  // 「刚刚」 rather than Intl's 「现在」 for under a minute
  if (s < 45) return t("lib-format.just_now");
  if (s < 3600) return formatRelative(-Math.round(s / 60), "minute");
  if (s < 86400) return formatRelative(-Math.round(s / 3600), "hour");
  if (s < 86400 * 7) return formatRelative(-Math.round(s / 86400), "day");
  return formatDate(ts, { month: "numeric", day: "numeric" });
}

export function duration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return t("lib-format.seconds", { s });
  const m = Math.floor(s / 60);
  if (m < 60) return s % 60 ? t("lib-format.minutes_seconds", { m, s: s % 60 }) : t("lib-format.minutes", { m });
  const h = Math.floor(m / 60);
  return m % 60 ? t("lib-format.hours_minutes", { h, m: m % 60 }) : t("lib-format.hours", { h });
}

// For a running timer in a list: minutes are enough once past one.
export function shortDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return t("lib-format.seconds", { s });
  const m = Math.floor(s / 60);
  if (m < 60) return t("lib-format.minutes", { m });
  return m % 60 ? t("lib-format.hours_minutes", { h: Math.floor(m / 60), m: m % 60 }) : t("lib-format.hours", { h: m / 60 });
}

export function clock(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const today = new Date();
  const hm = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  if (d.toDateString() === today.toDateString()) return hm;
  return t("lib-format.date_time", { date: formatDate(d, { month: "short", day: "numeric" }), time: hm });
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

/**
 * A random (v4) UUID. crypto.randomUUID only exists in a secure context (HTTPS, localhost): a hub opened over plain
 * http gets one from getRandomValues, which is everywhere. The hub takes only UUIDs as op ids (X-Jarvis-Op-Id, #1651).
 */
export function uuid(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

/** Whether s is a UUID as the hub checks op ids. */
export function isUUID(s: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(s);
}
