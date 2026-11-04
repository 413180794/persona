import { describe, expect, it } from "vitest";
import { afterEvent, afterHello, GRACE_MS, graceUntil, isResumed, NO_CURSOR, RESTART_GRACE_MS, retryDelay, shownConn, wsQuery } from "./reconnect";

describe("grace window", () => {
  it("keeps an ordinary drop invisible for 8 s, then shows it as before", () => {
    const d = { at: 1000, restart: false };
    expect(graceUntil(d)).toBe(1000 + GRACE_MS);
    expect(shownConn(d, 3, 1000)).toBe("online");
    expect(shownConn(d, 3, 1000 + GRACE_MS - 1)).toBe("online");
    expect(shownConn(d, 0, 1000 + GRACE_MS)).toBe("connecting");
    expect(shownConn(d, 2, 1000 + GRACE_MS)).toBe("offline");
  });

  it("keeps a restart invisible for 30 s", () => {
    const d = { at: 0, restart: true };
    expect(shownConn(d, 20, RESTART_GRACE_MS - 1)).toBe("online");
    expect(shownConn(d, 20, RESTART_GRACE_MS)).toBe("offline");
  });

  it("without a drop that began online, shows what it always did", () => {
    expect(shownConn(null, 0, 0)).toBe("connecting");
    expect(shownConn(null, 1, 0)).toBe("connecting");
    expect(shownConn(null, 2, 0)).toBe("offline");
  });
});

describe("retry delay", () => {
  it("after a restart: 150–400 ms, then every 0.5–1 s for 30 s", () => {
    expect(retryDelay(0, true, 0, 0)).toBe(150);
    expect(retryDelay(0, true, 0, 0.999)).toBeLessThan(400);
    expect(retryDelay(1, true, 5_000, 0)).toBe(500);
    expect(retryDelay(9, true, 29_000, 0.999)).toBeLessThan(1000);
  });

  it("after the quick phase, and after an ordinary drop: fast once, then the old backoff", () => {
    expect(retryDelay(0, true, 30_000, 0.999)).toBeLessThanOrEqual(600);
    expect(retryDelay(0, false, 0, 0.999)).toBeLessThanOrEqual(600);
    expect(retryDelay(1, false, 0, 0.5)).toBe(1200);
    expect(retryDelay(2, false, 0, 0.5)).toBe(2400);
    expect(retryDelay(10, false, 0, 0.5)).toBe(15_000);
  });
});

describe("event cursor", () => {
  it("adds the resume point to the socket's query only once there is a stream", () => {
    expect(wsQuery("t k", NO_CURSOR)).toBe("token=t%20k");
    expect(wsQuery("t", { stream: "s1", seq: 42 })).toBe("token=t&event_stream=s1&event_seq=42");
    // The session list comes as deltas, not the whole list every half second.
    expect(wsQuery("t", { stream: "s1", seq: 42 }, ["sessions_delta"])).toBe("token=t&event_stream=s1&event_seq=42&features=sessions_delta");
    expect(wsQuery("t", NO_CURSOR, [])).toBe("token=t");
  });

  it("follows numbered broadcasts, ignores the rest and older numbers", () => {
    let c = afterHello(NO_CURSOR, { type: "hello", event_stream: "s1", event_seq: 10 });
    expect(c).toEqual({ stream: "s1", seq: 10 });
    c = afterEvent(c, { type: "sessions", event_seq: 11 });
    c = afterEvent(c, { type: "items" });
    c = afterEvent(c, { type: "todos", event_seq: 9 });
    expect(c).toEqual({ stream: "s1", seq: 11 });
  });

  it("a full hello covers up to its number; a resumed one waits for the replay", () => {
    const c = { stream: "s1", seq: 11 };
    expect(afterHello(c, { event_stream: "s1", event_seq: 20 })).toEqual({ stream: "s1", seq: 20 });
    const resumed = afterHello(c, { event_stream: "s1", event_seq: 20, resumed: true });
    expect(resumed).toEqual(c);
    expect(afterEvent(resumed, { type: "machines", event_seq: 12 })).toEqual({ stream: "s1", seq: 12 });
  });

  it("starts over on another stream, and forgets it with a hub that numbers nothing", () => {
    expect(afterHello({ stream: "s1", seq: 99 }, { event_stream: "s2", event_seq: 3 })).toEqual({ stream: "s2", seq: 3 });
    expect(afterHello({ stream: "s1", seq: 99 }, { type: "hello" })).toEqual(NO_CURSOR);
    expect(afterEvent(NO_CURSOR, { event_seq: 5 })).toEqual(NO_CURSOR);
  });

  it("only resumed: true is a resume", () => {
    expect(isResumed({ resumed: true })).toBe(true);
    expect(isResumed({ resumed: false })).toBe(false);
    expect(isResumed({})).toBe(false);
  });
});
