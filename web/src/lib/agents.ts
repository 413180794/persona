// 个人 Agent: lark-connector's paired personal agents through the hub's /api/v1/agents (docs/protocol.md). A session id is
// "<source>:<sessionKey>" and bridge keys contain "/", so the whole id is percent-encoded in paths.
import { useSyncExternalStore } from "react";
import { api } from "./api";

export interface AgentSession {
  id: string;
  source: string;
  sessionKey: string;
  accountId?: string;
  provider: string;
  title: string;
  kind?: string;
  canSend: boolean;
  unreadCount: number;
  rankTimeMs: number;
  hasAvatar?: boolean;
  avatarVersion?: string;
}

// Blocks are open-ended: text and tool-call are known, anything else is shown raw.
export interface AgentBlock {
  type: string;
  text?: string;
  name?: string;
  status?: string;
  summary?: string;
  input?: unknown;
  [k: string]: unknown;
}

export interface AgentMessage {
  externalId: string;
  sessionKey?: string;
  role: string;
  blocks: AgentBlock[];
  createTimeMs: number;
  position: number;
  local?: { clientId: string; status: string };
}

export interface MessagePage {
  session?: AgentSession;
  messages: AgentMessage[];
  hasMore: boolean;
  nextCursor?: string;
}

export interface SendResult {
  outbox: { clientId: string; status: string; error?: string };
  duplicate: boolean;
  upstream?: AgentMessage;
}

export const agentPath = (id: string) => `/agents/sessions/${encodeURIComponent(id)}`;

export function fetchSessions() {
  return api<{ sessions: AgentSession[] | null }>("/agents/sessions").then((r) => r.sessions ?? []);
}

// A source's setup state (agents.Setup* on the hub): why a reachable source has nothing to list yet.
export interface AgentSourceStatus {
  source: string;
  enabled: boolean;
  error?: string;
  setup?: "missing" | "disabled" | "no_accounts" | string;
}

const SETUP_HINTS: Record<string, string> = {
  missing: "lark-connector 的版本还没有个人 Agent，升级 lark-connector 后再试",
  disabled: "lark-connector 没有开启个人 Agent：在它的部署里设置 lark-connector_AGENT_BRIDGE=true 后重启",
  no_accounts: "还没有连接个人 Agent 账号：在 lark-connector 的设置里添加 Cue 或 Grok Bot",
};

/** What to do so the sources list agents, one line per source that is not set up. */
export const setupHints = (sources: AgentSourceStatus[]) => sources.flatMap((x) => (x.setup && SETUP_HINTS[x.setup] ? [SETUP_HINTS[x.setup]] : []));

export function fetchSources() {
  return api<{ sources: AgentSourceStatus[] | null }>("/agents").then((r) => r.sources ?? []);
}

export function fetchMessages(id: string, cursor?: string, limit = 50) {
  const q = new URLSearchParams({ limit: String(limit) });
  if (cursor) q.set("cursor", cursor);
  return api<MessagePage>(`${agentPath(id)}/messages?${q}`).then((p) => ({ ...p, messages: p.messages ?? [] }));
}

export function sendMessage(id: string, clientId: string, text: string) {
  return api<SendResult>(`${agentPath(id)}/messages`, { method: "POST", json: { clientId, text, origin: "app" } });
}

export function markRead(id: string) {
  return api(`${agentPath(id)}/read`, { method: "POST" });
}

// ── pure helpers (tested) ───────────────────────────────

const PROVIDER_LABEL: Record<string, string> = { grok_bot: "Grok Bot", muse: "Muse", cue: "Cue", dots: "Dots", claude_duet: "Claude Duet" };
export function providerLabel(p: string): string {
  return PROVIDER_LABEL[p] ?? (p || "其他");
}

export interface ProviderGroup {
  provider: string;
  sessions: AgentSession[];
  unread: number;
}

// Groups by provider (the group with the most recent session first), recent first inside; q filters on title / provider.
export function groupSessions(list: AgentSession[], q = ""): ProviderGroup[] {
  const needle = q.trim().toLowerCase();
  const hit = (s: AgentSession) => !needle || s.title.toLowerCase().includes(needle) || s.provider.toLowerCase().includes(needle) || providerLabel(s.provider).toLowerCase().includes(needle);
  const by = new Map<string, AgentSession[]>();
  for (const s of list) if (hit(s)) (by.get(s.provider) ?? by.set(s.provider, []).get(s.provider)!).push(s);
  const groups = [...by].map(([provider, ss]) => {
    const sessions = ss.slice().sort((a, b) => b.rankTimeMs - a.rankTimeMs);
    return { provider, sessions, unread: sessions.reduce((n, s) => n + (s.unreadCount || 0), 0) };
  });
  return groups.sort((a, b) => (b.sessions[0]?.rankTimeMs ?? 0) - (a.sessions[0]?.rankTimeMs ?? 0));
}

export const totalUnread = (list: AgentSession[]) => list.reduce((n, s) => n + (s.unreadCount || 0), 0);

// Pages come back in position order with the odd swap: merge by externalId, newest copy wins, sort by position.
export function mergeMessages(a: AgentMessage[], b: AgentMessage[]): AgentMessage[] {
  const m = new Map<string, AgentMessage>();
  for (const x of [...a, ...b]) m.set(x.externalId, x);
  return [...m.values()].sort((x, y) => x.position - y.position || x.createTimeMs - y.createTimeMs);
}

export type BlockView = { kind: "text"; text: string } | { kind: "reaction"; emoji: string } | { kind: "tool"; name: string; detail: string } | { kind: "raw"; json: string };

export function blockView(b: AgentBlock): BlockView {
  if (b.type === "text" && typeof b.text === "string") return { kind: "text", text: b.text };
  if (b.type === "reaction" && typeof b.emoji === "string" && b.emoji) return { kind: "reaction", emoji: b.emoji };
  if (b.type === "tool-call" || b.type === "tool_call") {
    const { type: _t, name, ...rest } = b;
    return { kind: "tool", name: String(name ?? "tool"), detail: Object.keys(rest).length ? JSON.stringify(rest, null, 2) : "" };
  }
  return { kind: "raw", json: JSON.stringify(b, null, 2) };
}

export const messageText = (m: AgentMessage) => m.blocks.filter((b) => b.type === "text").map((b) => b.text ?? "").join("\n");

// A message typed here and not yet seen in the session. It keeps its clientId so a retry is idempotent on the hub.
export interface Outgoing {
  clientId: string;
  text: string;
  at: number;
  status: "sending" | "sent" | "failed";
  error?: string;
}

// Persisted messages lose the clientId (externalId changes from local:<clientId>), so a local bubble is dropped once
// a user message with the same text, no older than it was typed (with clock slack), shows up.
export function pendingOutgoing(out: Outgoing[], messages: AgentMessage[], slackMs = 120_000): Outgoing[] {
  const used = new Set<string>();
  return out.filter((o) => {
    if (o.status === "failed" || o.status === "sending") return true;
    const m = messages.find((x) => !used.has(x.externalId) && x.role === "user" && x.createTimeMs >= o.at - slackMs && messageText(x).trim() === o.text.trim());
    if (!m) return true;
    used.add(m.externalId);
    return false;
  });
}

// ── session list store, refreshed on the hub's {"type":"agents"} event ─

type ListState = { sessions: AgentSession[]; loaded: boolean; error: string; hints: string[] };
let list: ListState = { sessions: [], loaded: false, error: "", hints: [] };
const listeners = new Set<() => void>();
const emit = (p: Partial<ListState>) => {
  list = { ...list, ...p };
  listeners.forEach((l) => l());
};

let inflight: Promise<void> | null = null;
// A call while a pull is on the way: that pull may have been answered before the change the call is for, so one more
// pull follows it (one, however many calls came meanwhile).
let queued: Promise<void> | null = null;
export function refreshAgentSessions(): Promise<void> {
  if (inflight) {
    queued ??= inflight.then(() => {
      queued = null;
      return refreshAgentSessions();
    });
    return queued;
  }
  inflight = Promise.all([fetchSessions(), fetchSources().catch(() => null)])
    .then(([sessions, sources]) => emit({ sessions, loaded: true, error: "", ...(sources ? { hints: setupHints(sources) } : {}) }))
    .catch((e) => emit({ loaded: true, error: e instanceof Error ? e.message : String(e) }))
    .finally(() => (inflight = null));
  return inflight;
}

// Called by the WebSocket handler: re-pull the list, and tell open sessions which ids moved (empty = all).
export function agentsChanged(ids: string[]) {
  void refreshAgentSessions();
  changedListeners.forEach((l) => l(ids));
}

const changedListeners = new Set<(ids: string[]) => void>();
export function onAgentsChanged(l: (ids: string[]) => void) {
  changedListeners.add(l);
  return () => {
    changedListeners.delete(l);
  };
}

export function setLocalRead(id: string) {
  if (list.sessions.some((s) => s.id === id && s.unreadCount)) emit({ sessions: list.sessions.map((s) => (s.id === id ? { ...s, unreadCount: 0 } : s)) });
}

export function resetAgents() {
  emit({ sessions: [], loaded: false, error: "", hints: [] });
}

export function useAgentList(): ListState {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => list,
  );
}

// ── list prefs: pins, view (group / sort / show / providers), custom sections — shared on the hub ─

export type GroupBy = "date" | "provider" | "none";
export type SortBy = "updated" | "unread" | "name";
export type ShowMode = "all" | "unread" | "sendable";
export interface AgentView {
  group?: GroupBy;
  sort?: SortBy;
  show?: ShowMode;
  providers?: string[]; // empty = every provider
}
export interface AgentSectionDef {
  id: string;
  name: string;
}
export interface AgentPrefs {
  pins: string[];
  view: AgentView;
  sections: AgentSectionDef[];
  assign: Record<string, string>; // session id → section id
}
export const DEFAULT_VIEW: Required<AgentView> = { group: "date", sort: "updated", show: "all", providers: [] };
export const emptyPrefs = (): AgentPrefs => ({ pins: [], view: {}, sections: [], assign: {} });
export const viewOf = (p: AgentPrefs): Required<AgentView> => ({ ...DEFAULT_VIEW, ...Object.fromEntries(Object.entries(p.view ?? {}).filter(([, v]) => v !== undefined && v !== "")) }) as Required<AgentView>;
export const isDefaultView = (p: AgentPrefs) => {
  const v = viewOf(p);
  return v.group === DEFAULT_VIEW.group && v.sort === DEFAULT_VIEW.sort && v.show === DEFAULT_VIEW.show && !v.providers.length;
};

export const GROUP_LABEL: Record<GroupBy, string> = { date: "日期", provider: "来源", none: "不分组" };
export const SORT_LABEL: Record<SortBy, string> = { updated: "最近更新", unread: "未读数", name: "名称" };
export const SHOW_LABEL: Record<ShowMode, string> = { all: "全部", unread: "仅未读", sendable: "仅可发送" };

export interface SessionGroup {
  key: string; // "pinned", "sec:<id>", "p:<provider>", "d:<bucket>", "all"
  label: string;
  sessions: AgentSession[];
  unread: number;
}

const DAY = 86_400_000;
export function dateBucket(ms: number, now = Date.now()): { key: string; label: string; order: number } {
  const d = new Date(now);
  const today = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  if (ms >= today) return { key: "today", label: "今天", order: 0 };
  if (ms >= today - DAY) return { key: "yesterday", label: "昨天", order: 1 };
  if (ms >= today - 7 * DAY) return { key: "week", label: "近 7 天", order: 2 };
  if (ms >= today - 30 * DAY) return { key: "month", label: "近 30 天", order: 3 };
  return { key: "older", label: "更早", order: 4 };
}

export function matchesQuery(s: AgentSession, q: string) {
  const needle = q.trim().toLowerCase();
  return !needle || s.title.toLowerCase().includes(needle) || s.provider.toLowerCase().includes(needle) || providerLabel(s.provider).toLowerCase().includes(needle);
}

function sorter(sort: SortBy) {
  return (a: AgentSession, b: AgentSession) =>
    sort === "unread" ? (b.unreadCount || 0) - (a.unreadCount || 0) || b.rankTimeMs - a.rankTimeMs
    : sort === "name" ? (a.title || "").localeCompare(b.title || "", "zh") || b.rankTimeMs - a.rankTimeMs
    : b.rankTimeMs - a.rankTimeMs;
}

const mk = (key: string, label: string, sessions: AgentSession[]): SessionGroup => ({ key, label, sessions, unread: sessions.reduce((n, s) => n + (s.unreadCount || 0), 0) });

// Filters (query, show, providers), then 已置顶 (pin order) first, custom sections next, then the chosen grouping,
// each sorted by the chosen order. Empty groups are dropped.
export function arrangeSessions(list: AgentSession[], prefs: AgentPrefs, q = "", now = Date.now()): SessionGroup[] {
  const v = viewOf(prefs);
  const keep = list.filter((s) => matchesQuery(s, q) && (v.show !== "unread" || s.unreadCount > 0) && (v.show !== "sendable" || s.canSend) && (!v.providers.length || v.providers.includes(s.provider)));
  const byId = new Map(keep.map((s) => [s.id, s]));
  const out: SessionGroup[] = [];
  const pinned = prefs.pins.map((id) => byId.get(id)).filter((s): s is AgentSession => !!s);
  const used = new Set(pinned.map((s) => s.id));
  if (pinned.length) out.push(mk("pinned", "已置顶", pinned));
  const cmp = sorter(v.sort);
  for (const sec of prefs.sections) {
    const ss = keep.filter((s) => !used.has(s.id) && prefs.assign[s.id] === sec.id).sort(cmp);
    ss.forEach((s) => used.add(s.id));
    if (ss.length) out.push(mk(`sec:${sec.id}`, sec.name, ss));
  }
  const rest = keep.filter((s) => !used.has(s.id)).sort(cmp);
  if (v.group === "none") {
    if (rest.length) out.push(mk("all", out.length ? "其他" : "", rest));
  } else if (v.group === "provider") {
    const by = new Map<string, AgentSession[]>();
    for (const s of rest) (by.get(s.provider) ?? by.set(s.provider, []).get(s.provider)!).push(s);
    const gs = [...by].map(([p, ss]) => mk(`p:${p}`, providerLabel(p), ss));
    const top = (g: SessionGroup) => Math.max(...g.sessions.map((s) => s.rankTimeMs));
    out.push(...(v.sort === "name" ? gs.sort((a, b) => a.label.localeCompare(b.label, "zh")) : gs.sort((a, b) => top(b) - top(a))));
  } else {
    const by = new Map<string, { label: string; order: number; ss: AgentSession[] }>();
    for (const s of rest) {
      const b = dateBucket(s.rankTimeMs, now);
      (by.get(b.key) ?? by.set(b.key, { label: b.label, order: b.order, ss: [] }).get(b.key)!).ss.push(s);
    }
    out.push(...[...by].sort((a, b) => a[1].order - b[1].order).map(([k, g]) => mk(`d:${k}`, g.label, g.ss)));
  }
  return out;
}

export function togglePin(p: AgentPrefs, id: string): AgentPrefs {
  return { ...p, pins: p.pins.includes(id) ? p.pins.filter((x) => x !== id) : [id, ...p.pins] };
}
export function assignSection(p: AgentPrefs, id: string, section: string | null): AgentPrefs {
  const assign = { ...p.assign };
  if (section) assign[id] = section;
  else delete assign[id];
  return { ...p, assign };
}
export function addSection(p: AgentPrefs, name: string, id = `s${Date.now().toString(36)}`): AgentPrefs {
  return { ...p, sections: [...p.sections, { id, name: name.trim() }] };
}
export function removeSection(p: AgentPrefs, id: string): AgentPrefs {
  return { ...p, sections: p.sections.filter((s) => s.id !== id), assign: Object.fromEntries(Object.entries(p.assign).filter(([, v]) => v !== id)) };
}
// 重置为默认 resets the view only; pins and sections stay.
export const resetView = (p: AgentPrefs): AgentPrefs => ({ ...p, view: {} });
export function setView(p: AgentPrefs, patch: AgentView): AgentPrefs {
  return { ...p, view: { ...p.view, ...patch } };
}
export function toggleProvider(p: AgentPrefs, provider: string): AgentPrefs {
  const cur = viewOf(p).providers;
  return setView(p, { providers: cur.includes(provider) ? cur.filter((x) => x !== provider) : [...cur, provider] });
}

export function normalizePrefs(x: Partial<AgentPrefs> | null | undefined): AgentPrefs {
  return { pins: Array.isArray(x?.pins) ? x!.pins : [], view: x?.view && typeof x.view === "object" ? x.view : {}, sections: Array.isArray(x?.sections) ? x!.sections : [], assign: x?.assign && typeof x.assign === "object" ? x.assign : {} };
}

// Hub-backed (GET/PUT /agents/prefs) so web and iOS share pins; mirrored to localStorage, which is also the
// fallback when the hub predates the endpoint.
const PREFS_LS = "jarvis.agents.prefs";
const readLocal = (): AgentPrefs => {
  try {
    return normalizePrefs(JSON.parse(localStorage.getItem(PREFS_LS) ?? "null"));
  } catch {
    return emptyPrefs();
  }
};
let prefs: AgentPrefs = typeof localStorage === "undefined" ? emptyPrefs() : readLocal();
let hubPrefs = true;
const prefListeners = new Set<() => void>();
const emitPrefs = (p: AgentPrefs) => {
  prefs = p;
  try {
    localStorage.setItem(PREFS_LS, JSON.stringify(p));
  } catch {
    /* private mode */
  }
  prefListeners.forEach((l) => l());
};

export function refreshAgentPrefs(): Promise<void> {
  return api<AgentPrefs>("/agents/prefs")
    .then((p) => {
      hubPrefs = true;
      emitPrefs(normalizePrefs(p));
    })
    .catch((e) => {
      if ((e as { status?: number }).status === 404 || (e as { status?: number }).status === 405) hubPrefs = false;
    });
}

let saveSeq = 0;
export function updateAgentPrefs(f: (p: AgentPrefs) => AgentPrefs) {
  const next = f(prefs);
  emitPrefs(next);
  if (!hubPrefs) return;
  const seq = ++saveSeq;
  api<AgentPrefs>("/agents/prefs", { method: "PUT", json: next })
    .then((p) => seq === saveSeq && emitPrefs(normalizePrefs(p)))
    .catch((e) => {
      if ((e as { status?: number }).status === 404 || (e as { status?: number }).status === 405) hubPrefs = false;
    });
}

export function useAgentPrefs(): AgentPrefs {
  return useSyncExternalStore(
    (l) => {
      prefListeners.add(l);
      return () => prefListeners.delete(l);
    },
    () => prefs,
  );
}
