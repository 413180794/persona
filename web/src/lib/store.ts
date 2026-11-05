// App-wide state: what the hub pushes over the WebSocket (machines, sessions, interactions), plus the assistant.
// Views read it with useStore(selector); per-session item streams go to listeners (ChatView subscribes).

import { BASE } from "./base";
import { useRef, useSyncExternalStore } from "react";
import { agentsChanged, refreshAgentPrefs, refreshAgentSessions, resetAgents } from "./agents";
import { applySessionDelta, SESSIONS_DELTA_FEATURE, sessionRefreshed, sessionSnapshot } from "./sessions-delta";
import { api, ApiError, CLIENT_FEATURES, CLIENT_PLATFORM, CLIENT_VERSION, getToken } from "./api";
import { currentLanguage } from "../i18n";
import type { AgentMutes } from "./agent-groups";
import { stateOf } from "./format";
import { NO_PINS, pinsOf, type AgentPins } from "./pins";
import type { AssistantInfo, Interaction, Item, Machine, MeshHub, Session } from "./types";
import type { Resolution } from "./answered";
import type { AgentLaunch } from "./new-agent";
import { isSpacesSupported, type Todo } from "./todos";
import { releaseAttachments } from "./attachments";
import { dropElicitationDraft } from "./elicitation";
import type { OutboxEntry } from "./outbox";
import { NO_LINK, RESTARTING_MS, type ConnLink } from "./connection";
import { setOfflineProbe } from "./feedback";
import { afterEvent, afterHello, graceUntil, isResumed, NO_CURSOR, RESTART_FAST_MS, retryDelay, shownConn, wsQuery, type Drop, type EventCursor } from "./reconnect";

export type Conn = "connecting" | "online" | "offline";

export interface AnsweredCard {
  ix: Interaction;
  resolution: Resolution;
  /** The hub said it was answered already: the resolution is the answer that took effect, not what was just sent. */
  already?: boolean;
}

export interface State {
  conn: Conn;
  /** What is behind `conn` once it dropped: since when, the next try, a restart, the network (lib/connection.ts). */
  link: ConnLink;
  version: string;
  /** What the hub can do (hello `features`): terminal_stream, terminal_control… */
  features: string[];
  /** This hub's name and its interconnected group (docs/plan-hub-mesh.md 2.6); empty on older hubs. */
  hubName: string;
  mesh: MeshHub[];
  machines: Machine[];
  sessions: Record<string, Session[]>;
  interactions: Interaction[];
  /** Cards this page just answered, by interaction id: shown answered (with the picks) for a moment, not clickable. */
  answered: Record<string, AnsweredCard>;
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
  link: { ...NO_LINK, networkUp: networkUp() },
  version: "",
  features: [],
  hubName: "",
  mesh: [],
  machines: [],
  sessions: {},
  interactions: [],
  answered: {},
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
 * Whether this connection gets the session list as deltas. The page asks for it on every connect, so a hub that has
 * it says so in hello; one that does not never sends `sessions_delta` and keeps sending the whole list.
 */
function deltaOn(): boolean {
  return state.features.includes(SESSIONS_DELTA_FEATURE);
}

/**
 * Feishu through the Lark gateway (feature `lark`, which an extension brings): /api/v1/lark/…, the 消息 page, the 飞书 settings,
 * the side panel's readers for Feishu documents and 公司圈 posts. Without it those links are ordinary links.
 */
export const LARK_FEATURE = "lark";

/** The web reads working Claudes' status lines (`live` pushes, lib/live.ts; docs/protocol.md「实时状态」). */
export const SESSION_LIVE_FEATURE = "session_live";

export function larkOn(s: Pick<State, "features"> = state): boolean {
  return hubHas(s.features, LARK_FEATURE, true);
}

export function useLarkOn(): boolean {
  return useStore(larkOn);
}

/**
 * The Feishu plugin writes document comments for the user (feature `lark_doc_comments`, #1290): the reader can reply,
 * comment, resolve and reopen. Not "unknown is what it used to do": a hub that does not say so has no such routes.
 */
export const LARK_DOC_COMMENTS_FEATURE = "lark_doc_comments";

export function docCommentsOn(s: Pick<State, "features"> = state): boolean {
  return larkOn(s) && s.features.includes(LARK_DOC_COMMENTS_FEATURE);
}

export function useDocCommentsOn(): boolean {
  return useStore(docCommentsOn);
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
// A reconnect after a drop is invisible for a while (lib/reconnect.ts): the drop that began while online, the
// attempts since, the ones in the quick phase after a restart, and the timer that shows the drop once the grace ends.
let drop: Drop | null = null;
let fastTries = 0;
let graceTimer: number | undefined;
// The hub said hub_restarting on this socket: its close is a restart whatever the close code.
let restartNotice = false;
let cursor: EventCursor = NO_CURSOR;

export function send(msg: Record<string, unknown>) {
  if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
}

export function connect() {
  stopped = false;
  clearTimeout(retryTimer);
  ws?.close();
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const sock = new WebSocket(`${proto}//${location.host}${BASE}/api/v1/ws?${wsQuery(getToken(), cursor, [SESSIONS_DELTA_FEATURE, SESSION_LIVE_FEATURE])}`);
  ws = sock;
  restartNotice = false;
  // an attempt does not move offline back to connecting: the pill stays on one phase while the hub is down instead of
  // flipping at every backoff tick (the app's ConnectionTitle); shownConn decides it when the attempt fails
  sock.onopen = () => {
    // the interface language: this connection's text, and the user's language for pushes and the assistant (docs/i18n.md)
    // and what it can render: MCP forms and links come only to a socket that says so
    sock.send(JSON.stringify({ type: "client_hello", platform: CLIENT_PLATFORM, version: CLIENT_VERSION, features: CLIENT_FEATURES, locale: currentLanguage() }));
    retry = 0;
    fastTries = 0;
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
  sock.onclose = (ev) => {
    if (ws !== sock) return;
    ws = null;
    if (stopped) return;
    const now = Date.now();
    const restart = restartNotice || ev.code === 1001;
    // the drop starts while online; a restart announced on a later attempt stretches its grace
    const next = !drop && state.conn === "online" ? { at: now, restart } : drop && restart && !drop.restart ? { ...drop, restart } : null;
    if (next) {
      drop = next;
      clearTimeout(graceTimer);
      graceTimer = window.setTimeout(showDrop, Math.max(0, graceUntil(next) - now));
    }
    const since = drop ? now - drop.at : Infinity;
    const fast = Boolean(drop?.restart) && since < RESTART_FAST_MS;
    // the ordinary backoff counts its own attempts: after a restart's quick phase it starts again from the fast one
    const wait = retryDelay(fast ? fastTries : retry - fastTries, Boolean(drop?.restart), since, Math.random());
    if (fast) fastTries++;
    retry++;
    const conn = shownConn(drop, retry - 1, now);
    // what the pill's details say (the facts change on every attempt; only the pill and its details read them)
    const link: ConnLink = { ...state.link, lastOnline: drop?.at ?? state.link.lastOnline, nextTry: now + wait, restarting: Boolean(drop?.restart) && now - (drop?.at ?? now) < RESTARTING_MS };
    set(conn !== state.conn ? { conn, link } : { link }); // inside the grace: the page still looks online
    retryTimer = window.setTimeout(connect, wait);
  };
}

// The grace is over and the socket is still not back: show it as before.
function showDrop() {
  if (!drop || stopped || state.conn !== "online") return;
  set({ conn: shownConn(drop, Math.max(0, retry - 1), Date.now()) });
}

function back() {
  drop = null;
  clearTimeout(graceTimer);
}

/** 立即重试 (the pill's details): try now instead of waiting for the backoff. */
export function reconnectNow() {
  if (stopped) return;
  connect();
}

function networkUp(): boolean {
  return typeof navigator === "undefined" || navigator.onLine !== false;
}

// An action that failed while the hub is out of reach says so once, calmly (lib/feedback.ts, ui/atoms toast).
setOfflineProbe(() => (!networkUp() ? "network" : state.conn !== "online" || drop ? "hub" : null));

export function disconnect() {
  stopped = true;
  back();
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
  window.addEventListener("online", () => {
    set({ link: { ...state.link, networkUp: true } });
    if (!stopped) connect();
  });
  window.addEventListener("offline", () => set({ link: { ...state.link, networkUp: false } }));
}

function onMessage(msg: Record<string, unknown>) {
  cursor = msg.type === "hello" ? afterHello(cursor, msg) : afterEvent(cursor, msg);
  switch (msg.type) {
    case "hello": {
      const mesh = (msg.mesh as { hubs?: MeshHub[] } | undefined)?.hubs;
      set({
        conn: "online",
        link: { ...state.link, nextTry: 0, restarting: false, networkUp: true },
        version: String(msg.server_version ?? ""),
        features: Array.isArray(msg.features) ? (msg.features as string[]) : [],
        hubName: typeof msg.hub_name === "string" ? msg.hub_name : "",
        mesh: Array.isArray(mesh) ? mesh : [],
      });
      back();
      // subscriptions are per connection: chats resubscribe from what they have either way
      sessionListeners.forEach((ls) => ls.forEach((l) => l.reconnected?.()));
      // resumed: the hub replays every broadcast missed since the cursor, nothing needs refetching
      if (!isResumed(msg)) {
        void refresh();
        void refreshAgentSessions();
        void refreshAgentPrefs();
      }
      break;
    }
    case "hub_restarting":
      restartNotice = true; // the close that follows is a restart: retry at once, stay quiet longer
      break;
    case "ping":
      send({ type: "pong" });
      break;
    case "machines":
      set({ machines: msg.machines as Machine[] });
      break;
    case "sessions": {
      const mid = msg.machine_id as string;
      // A delta hub's list has no Codex history (the page has that from GET /sessions): keep what it already has.
      const list = sessionSnapshot((msg.sessions as Session[]) ?? [], state.sessions[mid] ?? [], deltaOn());
      set({ sessions: { ...state.sessions, [mid]: list } });
      syncAssistant(mid, list);
      break;
    }
    case "sessions_delta": {
      const mid = msg.machine_id as string;
      const list = applySessionDelta(
        state.sessions[mid] ?? [],
        (msg.upsert as Session[]) ?? [],
        (msg.remove as string[]) ?? [],
      );
      set({ sessions: { ...state.sessions, [mid]: list } });
      syncAssistant(mid, list);
      break;
    }
    case "session": {
      const s = msg.session as Session;
      upsertSession(s);
      break;
    }
    case "interaction":
      changeInteraction((msg.interaction as Interaction).id, msg.interaction as Interaction);
      break;
    case "interaction_resolved":
      changeInteraction(msg.id as string, null);
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

/** One machine as the hub answered a change to it (PUT …/engine), over what the list has: the hub's next \`machines\`
 * push says the same, this only does not wait for it (iOS AppModel.applyMachine). */
export function applyMachine(m: Machine) {
  if (!state.machines.some((x) => x.id === m.id)) return;
  set({ machines: state.machines.map((x) => (x.id === m.id ? { ...x, ...m } : x)) });
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

// ── pending interactions ───────────────

// Every change to the pending interactions (an event, an answer from this page), numbered: a /interactions snapshot
// asked for before a change must not undo it when it lands. refresh() waits for /sessions too (seconds on a busy hub),
// so without this a card answered meanwhile came back, clickable, and answering it again said "no longer pending".
let ixRev = 0;
const ixChanges = new Map<string, { rev: number; ix: Interaction | null }>();
const IX_CHANGES_KEPT = 500;

function changeInteraction(id: string, ix: Interaction | null) {
  ixChanges.delete(id); // re-inserted: the oldest are dropped first
  ixChanges.set(id, { rev: ++ixRev, ix });
  if (ixChanges.size > IX_CHANGES_KEPT) ixChanges.delete(ixChanges.keys().next().value!);
  const interactions = state.interactions.filter((i) => i.id !== id);
  if (ix) interactions.push(ix);
  else dropElicitationDraft(id); // over here or elsewhere: what was typed into it goes with it
  if (!ix && interactions.length === state.interactions.length) return;
  set({ interactions });
}

/** The pending interactions from a snapshot asked for at revision `before`, with what changed since. */
export function mergeInteractions(snapshot: Interaction[], before: number): Interaction[] {
  const out = new Map(snapshot.map((i) => [i.id, i]));
  for (const [id, c] of ixChanges) {
    if (c.rev <= before) continue;
    out.delete(id);
    if (c.ix) out.set(id, c.ix);
  }
  return [...out.values()];
}

const ANSWERED_MS = 20_000;

/**
 * An interaction this page answered (or found over when it tried): its card goes at once, without waiting for the
 * hub's interaction_resolved, which may be late or lost with a socket. With `answer` it shows answered for a while.
 */
export function endInteraction(id: string, answer?: AnsweredCard) {
  changeInteraction(id, null);
  if (!answer) return;
  set({ answered: { ...state.answered, [id]: answer } });
  setTimeout(() => dropAnswered(id, answer), ANSWERED_MS);
}

/** Takes an answered card away (its time is up, or closed); `only` keeps a newer one for the same id. */
export function dropAnswered(id: string, only?: AnsweredCard) {
  const cur = state.answered[id];
  if (!cur || (only && cur !== only)) return;
  const answered = { ...state.answered };
  delete answered[id];
  set({ answered });
}

// Every /todos request, numbered: an answer to a request sent before the one already applied is older than what the
// page shows (refresh() waits for /sessions too, seconds on a busy hub) and must not put the old list back.
let todosAsked = 0;
let todosApplied = 0;

function askTodos() {
  const n = ++todosAsked;
  const answer = api<{ todos: Todo[] }>("/todos");
  /** Whether this answer may be applied (no later request's answer is in place); if so it is the one in place now. */
  const fresh = () => {
    if (n < todosApplied) return false;
    todosApplied = n;
    return true;
  };
  return { answer, fresh };
}

export async function refresh() {
  const before = ixRev;
  const asked = askTodos();
  const [sessions, interactions, assistant, machines, mutes, pins, todos] = await Promise.allSettled([
    api<{ sessions: Session[] }>("/sessions"),
    api<{ interactions: Interaction[] }>("/interactions"),
    api<AssistantInfo>("/assistant"),
    api<{ machines: Machine[] }>("/machines"),
    api<AgentMutes>("/mutes"),
    api<Partial<AgentPins>>("/pins"),
    asked.answer,
  ]);
  const patch: Partial<State> = { loaded: true };
  if (sessions.status === "fulfilled") {
    const by: Record<string, Session[]> = {};
    for (const s of sessions.value.sessions ?? []) (by[s.machine_id] ??= []).push(s);
    if (deltaOn()) {
      // What the socket pushed is newer than this answer (the request went out before): keep it, take only the Codex
      // history the answer has and the page does not.
      for (const [mid, old] of Object.entries(state.sessions)) by[mid] = sessionRefreshed(by[mid] ?? [], old, true);
    }
    patch.sessions = by;
  }
  if (interactions.status === "fulfilled") patch.interactions = mergeInteractions(interactions.value.interactions ?? [], before);
  if (assistant.status === "fulfilled") patch.assistant = { ...assistant.value, conversations: assistant.value.conversations ?? [] };
  if (machines.status === "fulfilled") patch.machines = machines.value.machines ?? [];
  if (mutes.status === "fulfilled") patch.mutes = { spaces: mutes.value.spaces ?? [], sessions: mutes.value.sessions ?? [], allowed: mutes.value.allowed ?? [] };
  if (pins.status === "fulfilled") patch.pins = pinsOf(pins.value);
  if (todos.status === "fulfilled") {
    if (asked.fresh()) {
      const list = todos.value.todos ?? [];
      patch.todos = list;
      patch.todosSupported = true;
      patch.todosLoaded = true;
      patch.spacesSupported = computeSpacesSupported(list);
    }
  } else {
    const reason = todos.reason;
    if (reason instanceof ApiError && reason.status === 404 && asked.fresh()) {
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
  const asked = askTodos();
  try {
    const res = await asked.answer;
    if (!asked.fresh()) return;
    const todos = res.todos ?? [];
    set({ todos, todosSupported: true, todosLoaded: true, spacesSupported: computeSpacesSupported(todos) });
  } catch (err) {
    if (err instanceof ApiError && err.status === 404 && asked.fresh()) {
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
  cursor = NO_CURSOR; // another login: another stream
  ixChanges.clear();
  todosApplied = ++todosAsked; // a /todos answer still on its way belongs to the old login
  resetAgents();
  Object.values(state.launches).forEach((launch) => releaseAttachments(launch.attachments));
  set({
    machines: [],
    sessions: {},
    interactions: [],
    answered: {},
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
    link: { ...NO_LINK, networkUp: networkUp() },
  });
}
