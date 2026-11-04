// Writes that survive a hub restart (#1574): one op id per logical operation, sent as X-Jarvis-Op-Id, so the hub
// does it once however many times it arrives; a request that died on the way (the old process going, a proxy
// answering 502/503/504) is sent again with the same id when the socket is back, or after a short wait, for up to a
// minute before the caller shows its failure.
import { api, ApiError } from "./api";
import { uuid } from "./format";
import { onHubEvent } from "./store";

export const OP_HEADER = "X-Jarvis-Op-Id";
/** How long a write keeps trying through a gap before it fails. */
export const OP_RETRY_MS = 60_000;

/** Whether the request may not have reached a running hub: fetch failed, or something in front of it answered 502/503/504. */
export function isTransient(e: unknown): boolean {
  if (e instanceof TypeError) return true; // fetch: the connection failed (an abort is a DOMException)
  // the hub's own errors carry a code or say what was delivered (a machine offline, a node timing out): those are
  // answers, not gaps
  return e instanceof ApiError && [502, 503, 504].includes(e.status) && (!e.isJson || (!e.code && e.body.delivered === undefined));
}

/** The wait before attempt `n` (1-based): 0.5 s, 1 s, 2 s, then every 4 s. */
export function opDelay(n: number): number {
  return Math.min(4_000, 500 * 2 ** Math.max(0, n - 1));
}

export interface RetryClock {
  now(): number;
  /** Waits `ms`, or less when there is a reason to try sooner (the socket came back). */
  wait(ms: number): Promise<void>;
}

const realClock: RetryClock = {
  now: () => Date.now(),
  wait: (ms) =>
    new Promise<void>((done) => {
      const finish = () => {
        clearTimeout(t);
        off();
        done();
      };
      const t = setTimeout(finish, ms);
      const off = onHubEvent("hello", finish); // the hub is back: try now
    }),
};

/**
 * Runs `run` until it answers, fails with something other than a gap, or `limit` ms have passed (then the last
 * error is thrown). `run` must send the same op every time: the caller makes its ids once, outside.
 */
export async function untilDelivered<T>(run: () => Promise<T>, clock: RetryClock = realClock, limit = OP_RETRY_MS): Promise<T> {
  const start = clock.now();
  for (let n = 1; ; n++) {
    try {
      return await run();
    } catch (e) {
      const left = limit - (clock.now() - start);
      if (!isTransient(e) || left <= 0) throw e;
      await clock.wait(Math.min(opDelay(n), left));
    }
  }
}

/** A hub write with an op id (made here unless given), retried through a gap with that same id. */
export function apiOp<T>(path: string, init: Parameters<typeof api>[1] & { opId?: string } = {}, clock?: RetryClock): Promise<T> {
  const { opId = uuid(), ...rest } = init;
  return untilDelivered(() => {
    const headers = new Headers(rest.headers);
    headers.set(OP_HEADER, opId);
    return api<T>(path, { ...rest, headers });
  }, clock);
}
