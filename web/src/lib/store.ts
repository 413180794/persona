// App-wide state: what the hub pushes over the WebSocket (machines, sessions, interactions), plus the assistant.
// Views read it with useStore(selector); per-session item streams go to listeners (ChatView subscribes).

import { BASE } from "./base";
import { useRef, useSyncExternalStore } from "react";
import { agentsChanged, refreshAgentPrefs, refreshAgentSessions, resetAgents } from "./agents";
import { applySessionDelta, SESSIONS_DELTA_FEATURE, sessionRefreshed, sessionSnapshot } from "./sessions-delta";
import { api, ApiError, CLIENT_FEATURES, CLIENT_PLATFORM, CLIENT_VERSION, errorText, getToken } from "./api";
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
  /** When this page answered it (Date.now(), set by endInteraction): views/assistant/answered-lookup.ts compares it
   *  with the agent's state_since. */
  at?: number;
  /** Handed back to the computer (dismiss; or the hub says it was handed back already): its agent still waits there,
   *  so answered-lookup does not count it as answered (#4877). */
  handedBack?: boolean;
  /** The hub has withdrawn the card (its interaction_resolved came, or it was over already): until then a sessions push
   *  that still says needs_you may be from before the answer took (#4877). */
  withdrawn?: boolean;
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
  assistantError: string | null;
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
  assistantError: null,
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

/** Called after every change of the state (lib/error-reports-start.ts watches the hello's features). */
export const subscribe = (l: () => void) => {
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
 * Feishu through the Lark gateway (feature `lark`, which an extension brings): /api/v1/lark/…, the "Messages" page, the Lark settings,
 * the side panel's readers for Feishu documents and Moments posts. Without it those links are ordinary links.
 */
export const LARK_FEATURE = "lark";

/** The web reads working Claudes' status lines (`live` pushes, lib/live.ts; docs/protocol.md "Live status"). */
export const SESSION_LIVE_FEATURE = "session_live";
/** The hub answers a client's {"type":"ping","id"} with a pong carrying the id (docs/protocol.md). */
export const APP_PING_FEATURE = "app_ping";

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
let lastPing = 0;
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
    lastPing = Date.now();
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

/** "Retry now" (the pill's details): try now instead of waiting for the backoff. */
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
  // a reconnect scheduled after a drop would otherwise fire connect(), which clears stopped (signed out: an empty token)
  clearTimeout(retryTimer);
  back();
  ws?.close();
  ws = null;
}

// A socket that went quiet (sleep, network change) is reconnected. The hub's own pings are WebSocket control frames
// the page never sees, and an idle hub pushes nothing, so a hub that has app_ping is asked: every APP_PING_EVERY a
// {"type":"ping"} goes out and its pong counts as hearing from the hub (docs/protocol.md). A hub without it is left
// as before: the quiet limit alone, so a hub with nothing to say is reconnected now and then. Checked every 5 s, so a
// ping goes out every 25 to 30 s while timers run normally; background tabs can delay it further.
const APP_PING_EVERY = 25_000;
let pingSeq = 0;

/** One look at the socket (every 5 s): reconnect a quiet one, or ask a hub with app_ping whether it is there. */
export function checkSocket() {
  const now = Date.now();
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  if (now - lastMessage > 70_000) {
    connect();
    return;
  }
  if (hubHas(state.features, APP_PING_FEATURE) && now - lastPing >= APP_PING_EVERY) {
    lastPing = now;
    send({ type: "ping", id: `web-${++pingSeq}` });
  }
}
setInterval(() => checkSocket(), 5_000);
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
    case "pong": // an app_ping answer: arriving at all is the point (lastMessage)
      break;
    case "machines":
      machinesRev++;
      set({ machines: msg.machines as Machine[] });
      break;
    case "sessions": {
      const mid = msg.machine_id as string;
      sessionsChanged(mid);
      // A delta hub's list has no Codex history (the page has that from GET /sessions): keep what it already has.
      const list = sessionSnapshot((msg.sessions as Session[]) ?? [], state.sessions[mid] ?? [], deltaOn());
      set({ sessions: { ...state.sessions, [mid]: list } });
      syncAssistant(mid, list);
      break;
    }
    case "sessions_delta": {
      const mid = msg.machine_id as string;
      sessionsChanged(mid);
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
      sessionsChanged(s.machine_id);
      upsertSession(s);
      break;
    }
    case "interaction":
      changeInteraction((msg.interaction as Interaction).id, msg.interaction as Interaction);
      break;
    case "interaction_resolved":
      changeInteraction(msg.id as string, null);
      cardWithdrawn(msg.id as string);
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

// This page's changes to the mutes and the pins: numbered, and how many PUTs are still on their way. Each goes out as a
// whole PUT and the hub pushes nothing back, so a refresh() sent before a change lands (or while its PUT is out, the
// hub may read before it writes) answers the old value, and a failed change put back over a later one undoes it.
const mutesSaves = { rev: 0, out: 0 };
const pinsSaves = { rev: 0, out: 0 };

/** Shows `next` at once and PUTs it; on failure puts back what was before, unless a later change came, and throws. */
async function save<K extends "mutes" | "pins">(key: K, path: string, next: State[K], saves: { rev: number; out: number }) {
  const before = state[key];
  const change = ++saves.rev;
  saves.out++;
  set({ [key]: next } as Partial<State>);
  try {
    await api(path, { method: "PUT", json: next });
  } catch (e) {
    if (change === saves.rev) set({ [key]: before } as Partial<State>);
    throw e;
  } finally {
    saves.out--;
  }
}

/** "Mute" (optimistic): PUT /mutes; throws when the hub refuses, with the mutes put back. */
export function saveMutes(mutes: AgentMutes): Promise<void> {
  return save("mutes", "/mutes", mutes, mutesSaves);
}

/** One machine as the hub answered a change to it (PUT …/engine), over what the list has: the hub's next \`machines\`
 * push says the same, this only does not wait for it (iOS AppModel.applyMachine). */
export function applyMachine(m: Machine) {
  if (!state.machines.some((x) => x.id === m.id)) return;
  machinesRev++;
  set({ machines: state.machines.map((x) => (x.id === m.id ? { ...x, ...m } : x)) });
}

/** "Pin" (optimistic): PUT /pins; throws when the hub refuses, with the pins put back. */
export function savePins(pins: AgentPins): Promise<void> {
  return save("pins", "/pins", pins, pinsSaves);
}

/** "Mark all as seen": every finished-unread one of these is seen now, in one update of the list (iOS AppModel.markSeen). The
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
  const card = { ...answer, at: answer.at ?? Date.now(), withdrawn: answer.withdrawn || answer.already === true || resolvedSeen.has(id) };
  set({ answered: { ...state.answered, [id]: card } });
  setTimeout(() => dropAnswered(id, card), ANSWERED_MS);
}

// Cards the hub has withdrawn (interaction_resolved), newest last: the event often comes before the answer's own reply.
const resolvedSeen = new Set<string>();
const RESOLVED_KEPT = 200;

/** The hub withdrew a card (interaction_resolved, on the same socket as the sessions pushes). */
export function cardWithdrawn(id: string) {
  resolvedSeen.delete(id);
  resolvedSeen.add(id);
  if (resolvedSeen.size > RESOLVED_KEPT) resolvedSeen.delete(resolvedSeen.values().next().value!);
  const cur = state.answered[id];
  if (!cur || cur.withdrawn) return;
  set({ answered: { ...state.answered, [id]: { ...cur, withdrawn: true } } });
}

/** Takes an answered card away (its time is up, or closed); `only` keeps a newer one for the same id (the same answer
 *  marked withdrawn since is still that one: compared by its time). */
export function dropAnswered(id: string, only?: AnsweredCard) {
  const cur = state.answered[id];
  if (!cur || (only && cur !== only && cur.at !== only.at)) return;
  const answered = { ...state.answered };
  delete answered[id];
  set({ answered });
}

// Every request for a list the page keeps whole (/todos, /assistant), numbered: an answer to a request sent before the
// one already applied is older than what the page shows (refresh() waits for /sessions too, seconds on a busy hub) and
// must not put the old list back.
function numbered<T>(path: string) {
  let asked = 0;
  let applied = 0;
  return {
    ask() {
      const n = ++asked;
      const answer = api<T>(path);
      /** Whether this answer may be applied (no later request's answer is in place); if so it is the one in place now. */
      const fresh = () => {
        if (n < applied) return false;
        applied = n;
        return true;
      };
      return { answer, fresh };
    },
    /** Every answer still on its way is old now (another login). */
    forget() {
      applied = ++asked;
    },
  };
}

const todosAsked = numbered<{ todos: Todo[] }>("/todos");
const assistantAsked = numbered<AssistantInfo>("/assistant");

// refresh() calls, numbered the same way: one that started before the last one applied, or before reset(), is older
// than what the page shows and is dropped whole.
const refreshes = { asked: 0, applied: 0 };
// Bumped by every machines push and applyMachine(): a GET /machines answer asked for before one of them is older.
let machinesRev = 0;
// Session pushes, numbered, and the last one per machine: on a hub without deltas a GET /sessions answer asked for
// before a push to a machine must not put that machine's older list back.
let sessionsRev = 0;
const sessionsPushed = new Map<string, number>();

function sessionsChanged(mid: string) {
  sessionsPushed.set(mid, ++sessionsRev);
}

export async function refresh() {
  const gen = ++refreshes.asked;
  const machinesAt = machinesRev;
  const sessionsAt = sessionsRev;
  const before = ixRev;
  // the answers count only if this page changes nothing while they are on their way, and had no change out when they left
  const mutesAt = mutesSaves.out ? -1 : mutesSaves.rev;
  const pinsAt = pinsSaves.out ? -1 : pinsSaves.rev;
  const asked = todosAsked.ask();
  const askedAssistant = assistantAsked.ask();
  // the todos go in as soon as they answer: By to-do (the default, #2812) waits for them, not for /sessions, which can
  // take seconds on a busy hub while the socket has pushed the sessions already
  const todosDone = asked.answer.then(
    (res) => applyTodos({ status: "fulfilled", value: res }, asked.fresh),
    (reason: unknown) => applyTodos({ status: "rejected", reason }, asked.fresh),
  );
  const [sessions, interactions, assistant, machines, mutes, pins] = await Promise.allSettled([
    api<{ sessions: Session[] }>("/sessions"),
    api<{ interactions: Interaction[] }>("/interactions"),
    askedAssistant.answer,
    api<{ machines: Machine[] }>("/machines"),
    api<AgentMutes>("/mutes"),
    api<Partial<AgentPins>>("/pins"),
  ]);
  if (gen < refreshes.applied) return todosDone;
  refreshes.applied = gen;
  const patch: Partial<State> = { loaded: true };
  if (sessions.status === "fulfilled") {
    const by: Record<string, Session[]> = {};
    for (const s of sessions.value.sessions ?? []) (by[s.machine_id] ??= []).push(s);
    if (deltaOn()) {
      // What the socket pushed is newer than this answer (the request went out before): keep it, take only the Codex
      // history the answer has and the page does not.
      for (const [mid, old] of Object.entries(state.sessions)) by[mid] = sessionRefreshed(by[mid] ?? [], old, true);
    } else {
      // a machine pushed since the request went out keeps what was pushed
      for (const [mid, rev] of sessionsPushed) if (rev > sessionsAt && state.sessions[mid]) by[mid] = state.sessions[mid];
    }
    patch.sessions = by;
  }
  if (interactions.status === "fulfilled") patch.interactions = mergeInteractions(interactions.value.interactions ?? [], before);
  if (assistant.status === "fulfilled" && askedAssistant.fresh()) {
    patch.assistant = { ...assistant.value, conversations: assistant.value.conversations ?? [] };
    patch.assistantError = null;
  } else if (assistant.status === "rejected" && askedAssistant.fresh()) {
    patch.assistantError = errorText(assistant.reason);
  }
  if (machines.status === "fulfilled" && machinesAt === machinesRev) patch.machines = machines.value.machines ?? [];
  if (mutes.status === "fulfilled" && mutesAt === mutesSaves.rev) patch.mutes = { spaces: mutes.value.spaces ?? [], sessions: mutes.value.sessions ?? [], allowed: mutes.value.allowed ?? [] };
  if (pins.status === "fulfilled" && pinsAt === pinsSaves.rev) patch.pins = pinsOf(pins.value);
  set(patch);
  await todosDone;
}

/** refresh()'s GET /todos: the list, or a hub without todos (404), unless a later request's answer is in place (`fresh`);
 *  loaded either way. */
function applyTodos(todos: PromiseSettledResult<{ todos: Todo[] }>, fresh: () => boolean) {
  if (todos.status === "fulfilled") {
    if (!fresh()) return; // a later answer is in place, or this one belongs to a login reset() ended
    const list = todos.value.todos ?? [];
    return set({ todos: list, todosSupported: true, todosLoaded: true, spacesSupported: computeSpacesSupported(list) });
  }
  const reason = todos.reason;
  if (reason instanceof ApiError && reason.status === 404 && fresh()) return set({ todosSupported: false, todosLoaded: true, spacesSupported: false });
  set({ todosLoaded: true });
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

/** Takes a todo the hub just deleted out of the list at once; a /todos answer asked for before the delete is older
 *  and must not put it back. */
export function dropTodo(id: string) {
  todosAsked.forget();
  set({ todos: state.todos.filter((t) => t.id !== id) });
}

export async function refreshTodos() {
  const asked = todosAsked.ask();
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
  const asked = assistantAsked.ask();
  try {
    const a = await asked.answer;
    if (!asked.fresh()) return;
    set({ assistant: { ...a, conversations: a.conversations ?? [] }, assistantError: null });
  } catch (err) {
    if (asked.fresh()) {
      set({ assistantError: errorText(err) });
    }
    throw err;
  }
}

export function markSeenLocally(mid: string, sid: string) {
  send({ type: "seen", machine_id: mid, session_id: sid });
}

export function reset() {
  disconnect();
  cursor = NO_CURSOR; // another login: another stream
  ixChanges.clear();
  resolvedSeen.clear();
  // a /todos or /assistant answer still on its way belongs to the old login
  todosAsked.forget();
  assistantAsked.forget();
  // and so do the pins and mutes of a refresh() or a failed save still on its way
  mutesSaves.rev++;
  pinsSaves.rev++;
  // a refresh() still on its way belongs to the old login too
  refreshes.applied = ++refreshes.asked;
  sessionsPushed.clear();
  resetAgents();
  Object.values(state.launches).forEach((launch) => releaseAttachments(launch.attachments));
  set({
    machines: [],
    sessions: {},
    interactions: [],
    answered: {},
    assistant: null,
    assistantError: null,
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
