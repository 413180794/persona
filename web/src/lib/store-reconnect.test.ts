// The socket's side of #1574: a drop is invisible inside the grace, a restart retries at once, and a resumed hello
// refetches nothing while an ordinary one refreshes everything as before.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { connect, getState, onHubEvent, reset } from "./store";
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
});
