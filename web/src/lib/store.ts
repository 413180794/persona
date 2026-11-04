// App-wide state: what the hub pushes over the WebSocket (machines, sessions, interactions), plus the assistant.
// Views read it with useStore(selector); per-session item streams go to listeners (ChatView subscribes).

import { useRef, useSyncExternalStore } from "react";
import { agentsChanged, refreshAgentPrefs, refreshAgentSessions, resetAgents } from "./agents";
import { api, ApiError, getToken } from "./api";
import type { AgentMutes } from "./agent-groups";
import { stateOf } from "./format";
import { NO_PINS, pinsOf, type AgentPins } from "./pins";
import type { AssistantInfo, Interaction, Item, Machine, MeshHub, Session } from "./types";
import type { AgentLaunch } from "./new-agent";
import { isSpacesSupported, type Todo } from "./todos";
import { releaseAttachments } from "./attachments";
import type { OutboxEntry } from "./outbox";

export type Conn = "connecting" | "online" | "offline";

export interface State {
  conn: Conn;
  version: string;
  /** What the hub can do (hello `features`): terminal_stream, terminal_control… */
  features: string[];
  /** This hub's name and its interconnected group (docs/plan-hub-mesh.md 2.6); empty on older hubs. */
  hubName: string;
  mesh: MeshHub[];
  machines: Machine[];
  sessions: Record<string, Session[]>;
  interactions: Interaction[];
  assistant: AssistantInfo | null;
  mutes: AgentMutes;
  /** Pinned spaces / agents (GET /pins): the top of the Agents list. */
  pins: AgentPins;
  loaded: boolean;
  launches: Record<string, AgentLaunch>;
  /** Messages other pages sent for a conversation (`mid/sid`), shown there until the transcript has them (lib/outbox.ts). */
  outbox: Record<string, OutboxEntry[]>;
  todos: Todo[];
  todosSupported: boolean;
  todosLoaded: boolean;
  spacesSupported: boolean;
}

let state: State = {
  conn: "connecting",
  version: "",
  features: [],
  hubName: "",
  mesh: [],
  machines: [],
  sessions: {},
  interactions: [],
  assistant: null,
  mutes: { spaces: [], sessions: [], allowed: [] },
  pins: NO_PINS,
  loaded: false,
  launches: {},
  outbox: {},
  todos: [],
  todosSupported: true,
  todosLoaded: false,
  spacesSupported: true,
};

const listeners = new Set<() => void>();

function set(patch: Partial<State>) {
  state = { ...state, ...patch };
  listeners.forEach((l) => l());
}

export function getState() {
  return state;
}

export function getSession(mid: string, sid: string): Session | undefined {
  return (state.sessions[mid] ?? []).find((s) => s.session_id === sid)
    ?? state.assistant?.conversations.find((s) => s.machine_id === mid && s.session_id === sid);
}

// Selectors may build new arrays / objects: a result shallow-equal to the last one is the last one, so
// useSyncExternalStore sees a stable snapshot and only re-renders on a real change.
function shallowEqual(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((x, i) => Object.is(x, b[i]));
  if (a && b && typeof a === "object" && typeof b === "object" && !Array.isArray(a) && !Array.isArray(b)) {
    const ka = Object.keys(a), kb = Object.keys(b);
    return ka.length === kb.length && ka.every((k) => Object.is((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]));
  }
  return false;
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
};

export function useStore<T>(select: (s: State) => T): T {
  const last = useRef<{ v: T } | null>(null);
  const get = () => {
    const v = select(state);
    if (last.current && shallowEqual(last.current.v, v)) return last.current.v;
    last.current = { v };
    return v;
  };
  return useSyncExternalStore(subscribe, get, get); // the third: static rendering (tests)
}

// ── what the hub can do (hello `features`) ───────────────

/**
 * Whether the hub has `feature`. `unknown` is the answer until the hub has named any (before hello, or a hub that
 * sends no features): for something the web always used to do, that keeps the old behavior until the hub says.
 */
export function hubHas(features: string[], feature: string, unknown = false): boolean {
  return features.length ? features.includes(feature) : unknown;
}

/**
 * Feishu through lark-connector (feature `lark`, which a plugin brings): /api/v1/lark/…, the 消息 page, the 飞书 settings,
 * the side panel's readers for Feishu documents and 公司圈 posts. Without it those links are ordinary links.
 */
export const LARK_FEATURE = "lark";

export function larkOn(s: Pick<State, "features"> = state): boolean {
  return hubHas(s.features, LARK_FEATURE, true);
}

export function useLarkOn(): boolean {
  return useStore(larkOn);
}

// ── item stream (subscribed sessions) ───────────────────

export interface ItemsEvent {
  machine_id: string;
  session_id: string;
  items: Item[];
  history?: string;
  to_seq?: number;
  from_seq?: number;
  backlog?: boolean;
  more_after?: boolean;
}

type SessionListener = {
  items?: (e: ItemsEvent) => void;
  delta?: (text: string) => void;
  reconnected?: () => void;
};
const sessionListeners = new Map<string, Set<SessionListener>>();
const key = (mid: string, sid: string) => `${mid}/${sid}`;

export function listenSession(mid: string, sid: string, l: SessionListener) {
  const k = key(mid, sid);
  if (!sessionListeners.has(k)) sessionListeners.set(k, new Set());
  sessionListeners.get(k)!.add(l);
  return () => {
    sessionListeners.get(k)?.delete(l);
  };
}

// ── other pushes (inbox, lark, terminal…): pages that need them listen by type ──

type HubListener = (msg: Record<string, unknown>) => void;
const hubListeners = new Map<string, Set<HubListener>>();

/** Listen to one type of WebSocket message (`inbox`, `lark`, `terminal`, `terminal_control`…); returns the unsubscribe. */
export function onHubEvent(type: string, l: HubListener) {
  if (!hubListeners.has(type)) hubListeners.set(type, new Set());
  hubListeners.get(type)!.add(l);
  return () => {
    hubListeners.get(type)?.delete(l);
  };
}

/** Whether the socket is open now (a page that sends its own subscriptions waits for `hello` otherwise). */
export function isOpen() {
  return ws?.readyState === WebSocket.OPEN;
}

// ── WebSocket ───────────────────────────────────────────

let ws: WebSocket | null = null;
let retry = 0;
let retryTimer: number | undefined;
let lastMessage = 0;
let stopped = true;

export function send(msg: Record<string, unknown>) {
  if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
}

export function connect() {
  stopped = false;
  clearTimeout(retryTimer);
  ws?.close();
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const sock = new WebSocket(`${proto}//${location.host}/api/v1/ws?token=${encodeURIComponent(getToken())}`);
  ws = sock;
  if (state.conn !== "online") set({ conn: "connecting" });
  sock.onopen = () => {
    retry = 0;
    lastMessage = Date.now();
  };
  sock.onmessage = (ev) => {
    lastMessage = Date.now();
    let msg: Record<string, unknown>;
    try {
      msg = JSON.parse(ev.data);
    } catch {
      return;
    }
    onMessage(msg);
  };
  sock.onclose = () => {
    if (ws !== sock) return;
    ws = null;
    if (stopped) return;
    set({ conn: retry > 1 ? "offline" : "connecting" });
    const wait = Math.min(15000, 600 * 2 ** retry) * (0.8 + Math.random() * 0.4);
    retry++;
    retryTimer = window.setTimeout(connect, wait);
  };
}

export function disconnect() {
  stopped = true;
  ws?.close();
  ws = null;
}

// The hub pings every so often; a socket that went quiet (sleep, network change) is reconnected.
setInterval(() => {
  if (ws && ws.readyState === WebSocket.OPEN && Date.now() - lastMessage > 70_000) connect();
}, 10_000);
if (typeof document !== "undefined") {
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && !stopped && (!ws || Date.now() - lastMessage > 40_000)) connect();
  });
}
if (typeof window !== "undefined") {
  window.addEventListener("online", () => !stopped && connect());
}

function onMessage(msg: Record<string, unknown>) {
  switch (msg.type) {
    case "hello": {
      const mesh = (msg.mesh as { hubs?: MeshHub[] } | undefined)?.hubs;
      set({
        conn: "online",
        version: String(msg.server_version ?? ""),
        features: Array.isArray(msg.features) ? (msg.features as string[]) : [],
        hubName: typeof msg.hub_name === "string" ? msg.hub_name : "",
        mesh: Array.isArray(mesh) ? mesh : [],
      });
      sessionListeners.forEach((ls) => ls.forEach((l) => l.reconnected?.()));
      void refresh();
      void refreshAgentSessions();
      void refreshAgentPrefs();
      break;
    }
    case "ping":
      send({ type: "pong" });
      break;
    case "machines":
      set({ machines: msg.machines as Machine[] });
      break;
    case "sessions": {
      const mid = msg.machine_id as string;
      set({ sessions: { ...state.sessions, [mid]: (msg.sessions as Session[]) ?? [] } });
      syncAssistant(mid, (msg.sessions as Session[]) ?? []);
      break;
    }
    case "session": {
      const s = msg.session as Session;
      upsertSession(s);
      break;
    }
    case "interaction": {
      const ix = msg.interaction as Interaction;
      set({ interactions: [...state.interactions.filter((i) => i.id !== ix.id), ix] });
      break;
    }
    case "interaction_resolved":
      set({ interactions: state.interactions.filter((i) => i.id !== msg.id) });
      break;
    case "items": {
      const e = msg as unknown as ItemsEvent;
      sessionListeners.get(key(e.machine_id, e.session_id))?.forEach((l) => l.items?.(e));
      break;
    }
    case "agents.prefs":
      void refreshAgentPrefs();
      break;
    case "agents":
      agentsChanged(Array.isArray(msg.session_ids) ? (msg.session_ids as string[]) : []);
      break;
    case "todos":
      void refreshTodos();
      break;
    case "delta":
      sessionListeners.get(key(msg.machine_id as string, msg.session_id as string))?.forEach((l) => l.delta?.(msg.text as string));
      break;
  }
  if (typeof msg.type === "string") hubListeners.get(msg.type)?.forEach((l) => l(msg));
}

/** Replace the mutes after a PUT /mutes (the chat's 屏蔽通知). */
export function setMutes(mutes: AgentMutes) {
  set({ mutes });
}

/** Replace the pins after a PUT /pins (optimistic; put back on failure). */
export function setPins(pins: AgentPins) {
  set({ pins });
}

/** 全部标为已读: every finished-unread one of these is seen now, in one update of the list (iOS AppModel.markSeen). The
 * hub pushes the sessions back as seen; the list does not wait for it. */
export function markSeen(list: Pick<Session, "machine_id" | "session_id">[]) {
  const keys = new Set(list.map((s) => `${s.machine_id}/${s.session_id}`));
  const now = new Date().toISOString();
  const sessions: Record<string, Session[]> = {};
  let changed = false;
  for (const [mid, ss] of Object.entries(state.sessions)) {
    sessions[mid] = ss.map((s) => {
      if (!keys.has(`${s.machine_id}/${s.session_id}`) || stateOf(s) !== "unread") return s;
      changed = true;
      if (isOpen()) markSeenLocally(s.machine_id, s.session_id);
      else void api(`/sessions/${encodeURIComponent(s.machine_id)}/${encodeURIComponent(s.session_id)}/seen`, { method: "POST" }).catch(() => {});
      return { ...s, state: "idle" as const, seen_at: now };
    });
  }
  if (changed) set({ sessions });
}

export function putLaunch(launch: AgentLaunch) {
  set({ launches: { ...state.launches, [launch.id]: launch } });
}

export function setOutbox(key: string, list: OutboxEntry[]) {
  const next = { ...state.outbox };
  if (list.length) next[key] = list;
  else delete next[key];
  set({ outbox: next });
}

// A deleted conversation leaves the list right away; the hub's next sessions push agrees (docs/protocol.md DELETE /sessions).
export function dropSession(mid: string, sid: string) {
  const list = state.sessions[mid];
  if (!list?.some((x) => x.session_id === sid)) return;
  set({ sessions: { ...state.sessions, [mid]: list.filter((x) => x.session_id !== sid) } });
}

export function upsertSession(s: Session) {
  const list = state.sessions[s.machine_id] ?? [];
  const i = list.findIndex((x) => x.session_id === s.session_id);
  const next = i < 0 ? [...list, s] : list.map((x, j) => (j === i ? { ...x, ...s } : x));
  set({ sessions: { ...state.sessions, [s.machine_id]: next } });
  syncAssistant(s.machine_id, [s]);
}

// The assistant's conversations are sessions on its machine too: keep the copy in `assistant` current.
function syncAssistant(mid: string, list: Session[]) {
  const a = state.assistant;
  if (!a || a.machine_id !== mid) return;
  let changed = false;
  const convs = a.conversations.map((c) => {
    const s = list.find((x) => x.session_id === c.session_id);
    if (!s) return c;
    changed = true;
    return { ...c, ...s };
  });
  if (changed) set({ assistant: { ...a, conversations: convs } });
}

export async function refresh() {
  const [sessions, interactions, assistant, machines, mutes, pins, todos] = await Promise.allSettled([
    api<{ sessions: Session[] }>("/sessions"),
    api<{ interactions: Interaction[] }>("/interactions"),
    api<AssistantInfo>("/assistant"),
    api<{ machines: Machine[] }>("/machines"),
    api<AgentMutes>("/mutes"),
    api<Partial<AgentPins>>("/pins"),
    api<{ todos: Todo[] }>("/todos"),
  ]);
  const patch: Partial<State> = { loaded: true };
  if (sessions.status === "fulfilled") {
    const by: Record<string, Session[]> = {};
    for (const s of sessions.value.sessions ?? []) (by[s.machine_id] ??= []).push(s);
    patch.sessions = by;
  }
  if (interactions.status === "fulfilled") patch.interactions = interactions.value.interactions ?? [];
  if (assistant.status === "fulfilled") patch.assistant = { ...assistant.value, conversations: assistant.value.conversations ?? [] };
  if (machines.status === "fulfilled") patch.machines = machines.value.machines ?? [];
  if (mutes.status === "fulfilled") patch.mutes = { spaces: mutes.value.spaces ?? [], sessions: mutes.value.sessions ?? [], allowed: mutes.value.allowed ?? [] };
  if (pins.status === "fulfilled") patch.pins = pinsOf(pins.value);
  if (todos.status === "fulfilled") {
    const list = todos.value.todos ?? [];
    patch.todos = list;
    patch.todosSupported = true;
    patch.todosLoaded = true;
    patch.spacesSupported = computeSpacesSupported(list);
  } else {
    const reason = todos.reason;
    if (reason instanceof ApiError && reason.status === 404) {
      patch.todosSupported = false;
      patch.spacesSupported = false;
    }
    patch.todosLoaded = true;
  }
  set(patch);
}

export function computeSpacesSupported(todos: Todo[]): boolean {
  return todos.length > 0 ? isSpacesSupported(todos) : true;
}

export function setSpacesSupported(supported: boolean) {
  set({ spacesSupported: supported });
}

/** Puts a todo the hub just answered in place of the one we have (a retried task shows starting at once). */
export function putTodo(todo: Todo) {
  set({ todos: state.todos.map((t) => (t.id === todo.id ? { ...t, ...todo } : t)) });
}

export async function refreshTodos() {
  try {
    const res = await api<{ todos: Todo[] }>("/todos");
    const todos = res.todos ?? [];
    set({ todos, todosSupported: true, todosLoaded: true, spacesSupported: computeSpacesSupported(todos) });
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      set({ todosSupported: false, todosLoaded: true, spacesSupported: false });
    }
  }
}

export async function refreshAssistant() {
  const a = await api<AssistantInfo>("/assistant");
  set({ assistant: { ...a, conversations: a.conversations ?? [] } });
}

export function markSeenLocally(mid: string, sid: string) {
  send({ type: "seen", machine_id: mid, session_id: sid });
}

export function reset() {
  disconnect();
  resetAgents();
  Object.values(state.launches).forEach((launch) => releaseAttachments(launch.attachments));
  set({
    machines: [],
    sessions: {},
    interactions: [],
    assistant: null,
    launches: {},
    outbox: {},
    mutes: { spaces: [], sessions: [], allowed: [] },
    pins: NO_PINS,
    todos: [],
    todosSupported: true,
    todosLoaded: false,
    spacesSupported: true,
    loaded: false,
    conn: "connecting",
  });
}
