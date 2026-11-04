// How the socket comes back after a drop without the page noticing (#1574): when to retry, how long the page keeps
// showing "online" while it does, and where the hub's broadcast stream left off (so a reconnect can replay only what
// was missed instead of refetching everything). Pure: store.ts drives it, the tests drive it directly.
import type { Conn } from "./store";

/** How long a drop stays invisible: an ordinary one, and one the hub announced (hub_restarting / close 1001). */
export const GRACE_MS = 8_000;
export const RESTART_GRACE_MS = 30_000;
/** How long the quick retries after a restart go on before the ordinary backoff takes over. */
export const RESTART_FAST_MS = 30_000;

export interface Drop {
  /** When the socket went (ms). */
  at: number;
  /** The hub said it was restarting (a hub_restarting message, or close code 1001). */
  restart: boolean;
}

/** Until when a drop keeps the page looking online. */
export function graceUntil(d: Drop): number {
  return d.at + (d.restart ? RESTART_GRACE_MS : GRACE_MS);
}

/**
 * What the page shows while reconnecting. `drop`: the drop that began while the page was online (none: it never was,
 * or it is back). `tries`: failed attempts since the drop. Inside the grace window nothing changes; after it, as
 * before: connecting for the first two attempts, then offline.
 */
export function shownConn(drop: Drop | null, tries: number, now: number): Conn {
  if (drop && now < graceUntil(drop)) return "online";
  return tries > 1 ? "offline" : "connecting";
}

/**
 * The wait before the next attempt. `attempt`: attempts so far in the current phase (0 = the first after the drop
 * or after the quick phase ended); `rand` in [0, 1). A restart retries right away (150–400 ms) and then every
 * 0.5–1 s while `sinceDrop` < RESTART_FAST_MS; anything else retries fast once (300–600 ms), then backs off as before.
 */
export function retryDelay(attempt: number, restart: boolean, sinceDrop: number, rand: number): number {
  if (restart && sinceDrop < RESTART_FAST_MS) return attempt === 0 ? 150 + rand * 250 : 500 + rand * 500;
  if (attempt === 0) return 300 + rand * 300;
  return Math.min(15_000, 600 * 2 ** attempt) * (0.8 + rand * 0.4);
}

// ── the hub's broadcast stream ──────────────────────────

/** Where this page is in the hub's numbered broadcasts: kept across reconnects, lost on a reload. */
export interface EventCursor {
  stream: string;
  seq: number;
}

export const NO_CURSOR: EventCursor = { stream: "", seq: 0 };

/** The WebSocket's query: the token, plus where to resume from once a hello gave a stream. */
export function wsQuery(token: string, c: EventCursor, features: string[] = []): string {
  let q = `token=${encodeURIComponent(token)}`;
  if (c.stream) q += `&event_stream=${encodeURIComponent(c.stream)}&event_seq=${c.seq}`;
  if (features.length) q += `&features=${features.map(encodeURIComponent).join(",")}`;
  return q;
}

/**
 * The cursor after a hello. Another stream (or a hub that numbers nothing) starts over at the hello's number. On the
 * same stream a resumed hello leaves it where it was: the replay that follows moves it, so a drop in the middle of the
 * replay resumes from the last one that arrived. A full refresh covers everything up to the hello's number.
 */
export function afterHello(c: EventCursor, hello: Record<string, unknown>): EventCursor {
  const stream = typeof hello.event_stream === "string" ? hello.event_stream : "";
  const seq = typeof hello.event_seq === "number" ? hello.event_seq : 0;
  if (!stream) return NO_CURSOR;
  if (stream !== c.stream) return { stream, seq };
  if (hello.resumed === true) return c;
  return { stream, seq: Math.max(c.seq, seq) };
}

/** The cursor after any other message: a numbered broadcast moves it forward. */
export function afterEvent(c: EventCursor, msg: Record<string, unknown>): EventCursor {
  const seq = msg.event_seq;
  if (!c.stream || typeof seq !== "number" || seq <= c.seq) return c;
  return { stream: c.stream, seq };
}

/** Whether a hello resumed the stream: the hub replays what was missed, so nothing needs refetching. */
export function isResumed(hello: Record<string, unknown>): boolean {
  return hello.resumed === true;
}
