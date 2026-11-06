// @vitest-environment jsdom
// The shared motion hooks (#3434): the running bar's token count (merged from RunningStatusBar, the same curve and
// the same snap on a drop) and the rows that just came in. Frames are driven by hand.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    get matches() {
      return query.includes("reduce") && (globalThis as { __reduce?: boolean }).__reduce === true;
    },
    media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});
import { COUNT_MS, easeOutCubic, ENTER_MS, useArrivals, useCountUp } from "./motion";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let frames = new Map<number, FrameRequestCallback>();
let nextFrame = 0;
let clock = 0;
let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  frames = new Map();
  nextFrame = 0;
  clock = 0;
  (globalThis as { __reduce?: boolean }).__reduce = false;
  vi.stubGlobal("requestAnimationFrame", (f: FrameRequestCallback) => {
    frames.set(++nextFrame, f);
    return nextFrame;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
const tick = (ms: number) =>
  act(() => {
    clock += ms;
    const due = [...frames.values()];
    frames.clear();
    due.forEach((f) => f(clock));
  });

function Count({ n }: { n: number }) {
  return <span>{Math.round(useCountUp(n))}</span>;
}
function Rows({ keys }: { keys: string[] }) {
  const fresh = useArrivals(keys);
  return <span>{[...fresh].join(",")}</span>;
}

describe("useCountUp (the running bar's tokens)", () => {
  it("eases up along 1 − (1 − p)³ and lands on the value", () => {
    act(() => root.render(<Count n={100} />));
    expect(host.textContent).toBe("100");
    act(() => root.render(<Count n={1100} />));
    expect(host.textContent).toBe("100");
    tick(0);
    tick(COUNT_MS / 2);
    expect(host.textContent).toBe(String(Math.round(100 + 1000 * easeOutCubic(0.5))));
    tick(COUNT_MS);
    expect(host.textContent).toBe("1100");
    expect(frames.size).toBe(0);
  });

  it("snaps on a drop (a new turn) and under reduced motion", () => {
    act(() => root.render(<Count n={500} />));
    act(() => root.render(<Count n={20} />));
    expect(host.textContent).toBe("20");
    expect(frames.size).toBe(0);
    (globalThis as { __reduce?: boolean }).__reduce = true;
    act(() => root.render(<Count n={900} />));
    expect(host.textContent).toBe("900");
    expect(frames.size).toBe(0);
  });
});

describe("useArrivals", () => {
  it("names only the keys that came after the first draw, for a moment", () => {
    act(() => root.render(<Rows keys={["a", "b"]} />));
    expect(host.textContent).toBe("");
    act(() => root.render(<Rows keys={["a", "b", "c"]} />));
    expect(host.textContent).toBe("c");
    act(() => vi.advanceTimersByTime(ENTER_MS + 50));
    expect(host.textContent).toBe("");
  });

  it("names none with reduced motion", () => {
    (globalThis as { __reduce?: boolean }).__reduce = true;
    act(() => root.render(<Rows keys={["a"]} />));
    act(() => root.render(<Rows keys={["a", "b"]} />));
    expect(host.textContent).toBe("");
  });
});
