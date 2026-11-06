// The socket's side of #1574: a drop is invisible inside the grace, a restart retries at once, and a resumed hello
// refetches nothing while an ordinary one refreshes everything as before.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { applyMachine, connect, disconnect, getState, onHubEvent, reconnectNow, refresh, reset } from "./store";
import { offlineNow } from "./feedback";
import { liveEntry, resetLive } from "./live";

class FakeSocket {
  static OPEN = 1;
  static all: FakeSocket[] = [];
  readyState = 0;
  url: string;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  sent: string[] = [];
  constructor(url: string) {
    this.url = url;
    FakeSocket.all.push(this);
  }
  send(s: string) {
    this.sent.push(s);
  }
  close() {
    this.readyState = 3;
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
  push(msg: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
  drop(code = 1006) {
    this.readyState = 3;
    this.onclose?.({ code });
  }
}

const last = () => FakeSocket.all[FakeSocket.all.length - 1];
const fetched = (fetcher: ReturnType<typeof vi.fn>) => fetcher.mock.calls.map((c) => String(c[0]));

let fetcher: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.useFakeTimers();
  FakeSocket.all = [];
  vi.stubGlobal("WebSocket", FakeSocket);
  vi.stubGlobal("location", { protocol: "http:", host: "hub" });
  vi.stubGlobal("localStorage", { getItem: () => "tok", setItem: () => {}, removeItem: () => {} });
  vi.stubGlobal("window", { setTimeout: (f: () => void, ms: number) => setTimeout(f, ms), clearTimeout: (t: number) => clearTimeout(t) });
  fetcher = vi.fn().mockImplementation(() => Promise.resolve(new Response("{}", { status: 200 })));
  vi.stubGlobal("fetch", fetcher);
});

afterEach(() => {
  reset();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

/** Connects and says hello on stream s1 at event 5; returns the socket. */
function online() {
  connect();
  const s = last();
  s.open();
  s.push({ type: "hello", server_version: "v1", event_stream: "s1", event_seq: 5 });
  expect(getState().conn).toBe("online");
  return s;
}

describe("reconnecting", () => {
  it("first connect sends no resume point; reconnects resume from the last broadcast seen", () => {
    const s = online();
    expect(s.url).toBe("ws://hub/api/v1/ws?token=tok&features=sessions_delta,session_live"); // the session list comes as deltas; status lines are pushed
    s.push({ type: "todos", event_seq: 6 });
    s.push({ type: "items", machine_id: "m", session_id: "x", items: [] });
    s.drop();
    vi.advanceTimersByTime(600);
    expect(last().url).toBe("ws://hub/api/v1/ws?token=tok&event_stream=s1&event_seq=6&features=sessions_delta,session_live");
  });

  it("an ordinary drop stays invisible for 8 s, then shows as before", () => {
    const s = online();
    s.drop();
    expect(getState().conn).toBe("online");
    // the attempts keep failing
    for (let t = 0; t < 7_900; t += 100) {
      vi.advanceTimersByTime(100);
      const cur = last();
      if (cur !== s && cur.readyState === 0) cur.drop();
    }
    expect(getState().conn).toBe("online");
    vi.advanceTimersByTime(200);
    expect(getState().conn).not.toBe("online");
  });

  it("back within the grace: nothing changes and a resumed hello refetches nothing", () => {
    const s = online();
    const seen: string[] = [];
    const off = onHubEvent("hello", (m) => seen.push(String(m.resumed)));
    fetcher.mockClear();
    s.drop();
    vi.advanceTimersByTime(600); // the first retry is within 600 ms
    const n = last();
    expect(n).not.toBe(s);
    n.open();
    n.push({ type: "hello", server_version: "v1", event_stream: "s1", event_seq: 7, resumed: true });
    expect(getState().conn).toBe("online");
    expect(seen).toEqual(["true"]);
    expect(fetched(fetcher)).toEqual([]);
    off();
  });

  it("a hello that did not resume refreshes everything, as before", async () => {
    const s = online();
    await vi.advanceTimersByTimeAsync(0);
    fetcher.mockClear();
    s.drop();
    vi.advanceTimersByTime(600);
    last().open();
    last().push({ type: "hello", server_version: "v1", event_stream: "s1", event_seq: 9, resumed: false });
    await vi.advanceTimersByTimeAsync(0);
    expect(fetched(fetcher)).toEqual(expect.arrayContaining(["/api/v1/sessions", "/api/v1/interactions", "/api/v1/machines", "/api/v1/todos"]));
  });

  it("a restart retries at once and stays invisible for 30 s", () => {
    const s = online();
    s.push({ type: "hub_restarting", back_in_ms: 2000 });
    s.drop(1001);
    expect(getState().conn).toBe("online");
    const before = FakeSocket.all.length;
    vi.advanceTimersByTime(400);
    expect(FakeSocket.all.length).toBe(before + 1); // retried within 150–400 ms
    for (let t = 400; t < 29_500; t += 100) {
      const cur = last();
      if (cur.readyState === 0) cur.drop(1006);
      vi.advanceTimersByTime(100);
    }
    expect(getState().conn).toBe("online");
    // quick retries: about one a second
    expect(FakeSocket.all.length - before).toBeGreaterThan(20);
    last().drop(1006);
    vi.advanceTimersByTime(1_000);
    expect(getState().conn).not.toBe("online"); // as before: connecting while an attempt runs, offline between
  });

  it("disconnect cancels a reconnect already scheduled (signed out after a drop: no socket with an empty token)", () => {
    const s = online();
    s.drop();
    disconnect(); // e.g. a 401 signs out before the retry fires
    const before = FakeSocket.all.length;
    vi.advanceTimersByTime(20_000);
    expect(FakeSocket.all.length).toBe(before);
  });

  it("close code 1001 alone counts as a restart", () => {
    const s = online();
    s.drop(1001);
    vi.advanceTimersByTime(20_000);
    expect(getState().conn).toBe("online");
  });
});

describe("live pushes (#1963)", () => {
  it("asks for session_live and hands each live message to lib/live", () => {
    const s = online();
    expect(s.url).toContain("session_live");
    s.push({ type: "live", machine_id: "m", session_id: "x", live: { verb: "Frosting", tokens_down: 1200 } });
    expect(liveEntry("m", "x")?.live).toEqual({ verb: "Frosting", tokens_down: 1200 });
    s.push({ type: "live", machine_id: "m", session_id: "x", live: null });
    expect(liveEntry("m", "x")).toBeUndefined();
    resetLive();
  });

  it("a hello that did not resume forgets every line; a resumed one keeps them", () => {
    const s = online();
    s.push({ type: "live", machine_id: "m", session_id: "x", live: { verb: "Frosting" } });
    s.drop(1006);
    vi.advanceTimersByTime(1000);
    last().open();
    last().push({ type: "hello", server_version: "v1", event_stream: "s1", event_seq: 6, resumed: true });
    expect(liveEntry("m", "x")?.live).toEqual({ verb: "Frosting" });
    last().drop(1006);
    vi.advanceTimersByTime(1000);
    last().open();
    last().push({ type: "hello", server_version: "v1", event_stream: "s2", event_seq: 1, resumed: false });
    expect(liveEntry("m", "x")).toBeUndefined(); // nothing is replayed, so the lines from before the drop are gone
    resetLive();
  });
});

describe("the facts behind the pill (lib/connection.ts)", () => {
  it("a drop records since when and the next try; a restart is told; hello clears them", () => {
    const s = online();
    vi.setSystemTime(50_000);
    s.push({ type: "hub_restarting" });
    s.drop(1001);
    const link = getState().link;
    expect(link.lastOnline).toBe(50_000);
    expect(link.restarting).toBe(true);
    expect(link.nextTry).toBeGreaterThan(50_000);
    vi.advanceTimersByTime(400);
    last().open();
    last().push({ type: "hello", server_version: "v1", event_stream: "s1", event_seq: 5, resumed: true });
    expect(getState().link.restarting).toBe(false);
    expect(getState().link.nextTry).toBe(0);
  });

  it("立即重试 opens a socket now instead of waiting for the backoff", () => {
    const s = online();
    s.drop();
    for (let i = 0; i < 4; i++) {
      vi.advanceTimersByTime(20_000);
      if (last().readyState === 0) last().drop();
    }
    const before = FakeSocket.all.length;
    reconnectNow();
    expect(FakeSocket.all.length).toBe(before + 1);
  });

  it("the phase stays put while the hub is down: a new attempt does not move offline back to connecting", () => {
    const s = online();
    s.drop();
    vi.advanceTimersByTime(9_000); // past the grace
    for (let i = 0; i < 6 && getState().conn !== "offline"; i++) {
      if (last().readyState === 0) last().drop();
      vi.advanceTimersByTime(20_000);
    }
    expect(getState().conn).toBe("offline");
    // every later attempt, while its socket is opening and after it fails
    for (let i = 0; i < 4; i++) {
      vi.advanceTimersByTime(20_000); // the next attempt has started
      expect(last().readyState).toBe(0);
      expect(getState().conn).toBe("offline");
      last().drop();
      expect(getState().conn).toBe("offline");
    }
    reconnectNow();
    expect(getState().conn).toBe("offline");
  });

  it("a restart is told for 5 minutes at most, then it is just out of reach", () => {
    const s = online();
    vi.setSystemTime(100_000);
    s.push({ type: "hub_restarting" });
    s.drop(1001);
    vi.advanceTimersByTime(60_000);
    if (last().readyState === 0) last().drop();
    expect(getState().link.restarting).toBe(true);
    for (let t = 0; t < 300_000; t += 15_000) {
      vi.advanceTimersByTime(15_000);
      if (last().readyState === 0) last().drop();
    }
    expect(getState().link.restarting).toBe(false);
    expect(getState().conn).toBe("offline");
  });

  it("an error toast while out of reach is the calm line (the probe the store registers)", () => {
    const s = online();
    expect(offlineNow()).toBeNull();
    s.drop();
    expect(offlineNow()).toBe("hub"); // inside the grace too: the socket is gone
  });
});

describe("a slow refresh() does not put back an older snapshot", () => {
  type Held = Record<string, ((body: unknown) => void)[]>;
  /** GETs whose path is in `held` wait until their turn is resolved; the rest answer `answers[path]` (or {}) at once. */
  function holdFetch(held: string[], answers: Record<string, unknown> = {}): Held {
    const turns: Held = Object.fromEntries(held.map((p) => [p, []]));
    const reply = (body: unknown) => ({ ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify(body) });
    fetcher.mockImplementation(async (url: string) => {
      const path = String(url).replace(/^.*\/api\/v1/, "");
      if (turns[path]) return reply(await new Promise((r) => turns[path].push(r)));
      return reply(answers[path] ?? {});
    });
    return turns;
  }
  const settle = async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  };
  const session = (machine_id: string, session_id: string, state: string) => ({ machine_id, session_id, state, title: session_id });

  it("a machines push that comes while GET /machines is out wins over its answer", async () => {
    const turns = holdFetch(["/machines"]);
    const s = online(); // the hello refreshes
    await settle();
    expect(turns["/machines"]).toHaveLength(1);
    s.push({ type: "machines", machines: [{ id: "a", name: "A", online: false }] });
    turns["/machines"][0]({ machines: [{ id: "a", name: "A", online: true }] });
    await settle();
    expect(getState().loaded).toBe(true);
    expect(getState().machines).toEqual([{ id: "a", name: "A", online: false }]);
  });

  it("applyMachine() while GET /machines is out wins over its answer", async () => {
    const turns = holdFetch(["/machines"]);
    online();
    await settle();
    turns["/machines"][0]({ machines: [{ id: "a", name: "A", online: true, engine: "herdr" }] });
    await settle();
    const later = refresh();
    await settle();
    applyMachine({ id: "a", name: "A", online: true, engine: "orca" });
    turns["/machines"][1]({ machines: [{ id: "a", name: "A", online: true, engine: "herdr" }] });
    await later;
    expect(getState().machines[0].engine).toBe("orca");
  });

  it("a hub without deltas: a sessions push while GET /sessions is out keeps that machine's pushed list", async () => {
    const turns = holdFetch(["/sessions"]);
    const s = online(); // the hello says no sessions_delta
    await settle();
    s.push({ type: "sessions", machine_id: "m", sessions: [session("m", "x", "working")] });
    turns["/sessions"][0]({ sessions: [session("m", "x", "idle"), session("n", "y", "idle")] });
    await settle();
    expect(getState().sessions.m.map((x) => x.state)).toEqual(["working"]);
    expect(getState().sessions.n.map((x) => x.session_id)).toEqual(["y"]); // a machine not pushed takes the answer
  });

  it("reset() while a refresh() is out: its answers belong to the old login and are dropped", async () => {
    const turns = holdFetch(["/sessions"], { "/machines": { machines: [{ id: "a", name: "A", online: true }] } });
    online();
    await settle();
    reset();
    turns["/sessions"][0]({ sessions: [session("a", "x", "idle")] });
    await settle();
    const st = getState();
    expect(st.loaded).toBe(false);
    expect(st.machines).toEqual([]);
    expect(st.sessions).toEqual({});
  });

  it("two overlapping refreshes: the older one answering last does not undo the newer one", async () => {
    const turns = holdFetch(["/machines"]);
    online(); // the first refresh
    await settle();
    const newer = refresh();
    await settle();
    expect(turns["/machines"]).toHaveLength(2);
    turns["/machines"][1]({ machines: [{ id: "a", name: "A", online: false }] });
    await newer;
    turns["/machines"][0]({ machines: [{ id: "a", name: "A", online: true }] });
    await settle();
    expect(getState().machines).toEqual([{ id: "a", name: "A", online: false }]);
  });

  it("refreshes answered in order are all applied", async () => {
    const turns = holdFetch(["/machines"]);
    online();
    await settle();
    turns["/machines"][0]({ machines: [{ id: "a", name: "A", online: true }] });
    await settle();
    expect(getState().machines[0].online).toBe(true);
    const next = refresh();
    await settle();
    turns["/machines"][1]({ machines: [{ id: "a", name: "A", online: false }] });
    await next;
    expect(getState().machines[0].online).toBe(false);
  });
});
