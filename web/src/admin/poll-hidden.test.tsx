// @vitest-environment jsdom
// Admin pages poll the hub only while the tab is visible: a hidden tab makes no calls, showing it again refreshes at
// once, and an unmounted page stops polling.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

const fake = vi.hoisted(() => ({ calls: [] as string[] }));
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  adminApi: (path: string) => {
    fake.calls.push(path);
    return Promise.resolve(path === "/peers" ? { peers: [], discovered: [] } : { hubs: [] });
  },
  getMesh: () => (fake.calls.push("/mesh"), Promise.resolve({ hubs: [] })),
}));

const { useVisiblePoll } = await import("./parts");
const { Peers } = await import("./pages/Peers");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let state: DocumentVisibilityState = "visible";
const setVisibility = (v: DocumentVisibilityState) => act(() => {
  state = v;
  document.dispatchEvent(new Event("visibilitychange"));
});

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};

beforeEach(() => {
  state = "visible";
  fake.calls = [];
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

function Poller({ tick, ms, enabled }: { tick: () => void; ms: number; enabled?: boolean }) {
  useVisiblePoll(tick, ms, enabled);
  return null;
}

describe("useVisiblePoll", () => {
  it("ticks only while visible, refreshes once on becoming visible, and stops on unmount", () => {
    const tick = vi.fn();
    act(() => root.render(<Poller tick={tick} ms={1000} />));
    expect(tick).not.toHaveBeenCalled(); // the page already loaded on mount
    act(() => vi.advanceTimersByTime(1000));
    expect(tick).toHaveBeenCalledTimes(1);

    setVisibility("hidden");
    act(() => vi.advanceTimersByTime(10_000));
    expect(tick).toHaveBeenCalledTimes(1);

    setVisibility("visible");
    expect(tick).toHaveBeenCalledTimes(2);
    act(() => vi.advanceTimersByTime(1000));
    expect(tick).toHaveBeenCalledTimes(3);

    act(() => root.unmount());
    root = createRoot(host);
    setVisibility("hidden");
    setVisibility("visible");
    act(() => vi.advanceTimersByTime(10_000));
    expect(tick).toHaveBeenCalledTimes(3);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does nothing while disabled", () => {
    const tick = vi.fn();
    act(() => root.render(<Poller tick={tick} ms={1000} enabled={false} />));
    act(() => vi.advanceTimersByTime(5000));
    setVisibility("hidden");
    setVisibility("visible");
    expect(tick).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("uses the latest callback without restarting the timer", () => {
    const first = vi.fn(), second = vi.fn();
    act(() => root.render(<Poller tick={first} ms={1000} />));
    act(() => root.render(<Poller tick={second} ms={1000} />));
    act(() => vi.advanceTimersByTime(1000));
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });
});

describe("Peers", () => {
  it("makes no hub calls while the tab stays hidden, and refreshes at once when shown", async () => {
    act(() => root.render(<Peers />));
    await flush();
    expect(fake.calls).toContain("/peers");
    act(() => vi.advanceTimersByTime(10_000));
    await flush();
    const visibleCalls = fake.calls.length;
    expect(visibleCalls).toBeGreaterThan(1);

    setVisibility("hidden");
    fake.calls = [];
    act(() => vi.advanceTimersByTime(30_000));
    await flush();
    expect(fake.calls).toEqual([]);

    setVisibility("visible");
    await flush();
    expect(fake.calls).toContain("/peers");
  });
});
