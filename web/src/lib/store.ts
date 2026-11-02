// App-wide state: what the hub pushes over the WebSocket (machines, sessions, interactions), plus the assistant.
// Views read it with useStore(selector); per-session item streams go to listeners (ChatView subscribes).

import { useRef, useSyncExternalStore } from "react";
import { agentsChanged, refreshAgentPrefs, refreshAgentSessions, resetAgents } from "./agents";
import { api, ApiError, getToken } from "./api";
import type { AgentMutes } from "./agent-groups";
import type { AssistantInfo, Interaction, Item, Machine, Session } from "./types";
import type { AgentLaunch } from "./new-agent";
import type { Todo } from "./todos";
import { releaseAttachments } from "./attachments";

export type Conn = "connecting" | "online" | "offline";

export interface State {
  conn: Conn;
  version: string;
  machines: Machine[];
  sessions: Record<string, Session[]>;
  interactions: Interaction[];
  assistant: AssistantInfo | null;
  mutes: AgentMutes;
  loaded: boolean;
  launches: Record<string, AgentLaunch>;
  todos: Todo[];
  todosSupported: boolean;
  todosLoaded: boolean;
}

let state: State = {
  conn: "connecting",
  version: "",
  machines: [],
  sessions: {},
  interactions: [],
  assistant: null,
  mutes: { spaces: [], sessions: [], allowed: [] },
  loaded: false,
  launches: {},
  todos: [],
  todosSupported: true,
  todosLoaded: false,
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
  return useSyncExternalStore(subscribe, () => {
    const v = select(state);
    if (last.current && shallowEqual(last.current.v, v)) return last.current.v;
    last.current = { v };
    return v;
  });
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
    case "hello":
      set({ conn: "online", version: String(msg.server_version ?? "") });
      sessionListeners.forEach((ls) => ls.forEach((l) => l.reconnected?.()));
      void refresh();
      void refreshAgentSessions();
      void refreshAgentPrefs();
      break;
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
}

export function putLaunch(launch: AgentLaunch) {
  set({ launches: { ...state.launches, [launch.id]: launch } });
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
  const [sessions, interactions, assistant, machines, mutes, todos] = await Promise.allSettled([
    api<{ sessions: Session[] }>("/sessions"),
    api<{ interactions: Interaction[] }>("/interactions"),
    api<AssistantInfo>("/assistant"),
    api<{ machines: Machine[] }>("/machines"),
    api<AgentMutes>("/mutes"),
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
  if (todos.status === "fulfilled") {
    patch.todos = todos.value.todos ?? [];
    patch.todosSupported = true;
    patch.todosLoaded = true;
  } else {
    const reason = todos.reason;
    if (reason instanceof ApiError && reason.status === 404) {
      patch.todosSupported = false;
    }
    patch.todosLoaded = true;
  }
  set(patch);
}

export async function refreshTodos() {
  try {
    const res = await api<{ todos: Todo[] }>("/todos");
    set({ todos: res.todos ?? [], todosSupported: true, todosLoaded: true });
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      set({ todosSupported: false, todosLoaded: true });
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
    mutes: { spaces: [], sessions: [], allowed: [] },
    todos: [],
    todosSupported: true,
    todosLoaded: false,
    loaded: false,
    conn: "connecting",
  });
}
