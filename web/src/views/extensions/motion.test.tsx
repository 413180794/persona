// @vitest-environment jsdom
// An extension's view drawn again with new values (#2757): a number that changed counts to the new one under the same
// key and ends exactly on it, text that isn't a number fades in, rows that come and go fade, a chart's bars move; nothing
// moves on the first draw or with reduced motion. Frames are driven by hand (requestAnimationFrame is faked).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    get matches() {
      return query.includes("reduce") && (globalThis as { __reduce?: boolean }).__reduce === true;
    },
    media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});
import { INERT, ViewSections } from "./ViewBody";
import { bezier, formatParts, numberParts, TWEEN_MS } from "../../ui/motion";
import type { UIItem, UIView } from "../../lib/extension-ui";
import { preloadPages } from "../../lib/lazy-page";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// the frames asked for and not yet run or cancelled, by id
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
  // a chart draws once it knows its width
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private cb: ResizeObserverCallback) {}
      observe() {
        this.cb([{ contentRect: { width: 320 } } as ResizeObserverEntry], this as unknown as ResizeObserver);
      }
      unobserve() {}
      disconnect() {}
    },
  );
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

/** Runs the frames due at `ms` after now. */
const tick = (ms: number) =>
  act(() => {
    clock += ms;
    const due = [...frames.values()];
    frames.clear();
    due.forEach((f) => f(clock));
  });
const settle = () => {
  for (let i = 0; i < 100 && frames.size; i++) tick(TWEEN_MS / 4);
  act(() => vi.runAllTimers());
};

const stats = (value: string, key = "counts"): UIView => ({ sections: [{ items: [{ type: "stat_grid", key, stats: [{ label: "做完", value }] } as UIItem] }] });
const draw = (v: UIView) => act(() => root.render(<ViewSections view={v} {...INERT} />));
const valueText = () => host.querySelector("[title]")!.textContent;
const setReduce = (on: boolean) => {
  (globalThis as { __reduce?: boolean }).__reduce = on;
};

describe("numbers in a value", () => {
  it("rolls only the numbers and writes them as the value does", () => {
    const p = numberParts("¥1,234.50 / 08 天");
    expect(p.nums).toEqual([1234.5, 8]);
    expect(p.shape).toBe("¥# / # 天");
    expect(formatParts(p, [999.456, 3])).toBe("¥999.46 / 03 天");
    expect(formatParts(p, p.nums)).toBe("¥1,234.50 / 08 天");
    expect(numberParts("在做").nums).toEqual([]);
  });

  it("eases with the curve the CSS uses", () => {
    const e = bezier("cubic-bezier(0.23, 1, 0.32, 1)");
    expect(e(0)).toBe(0);
    expect(e(1)).toBe(1);
    expect(e(0.5)).toBeGreaterThan(0.8); // a strong ease-out
    expect(bezier("cubic-bezier(0, 0, 1, 1)")(0.3)).toBeCloseTo(0.3, 3);
  });
});

describe("a view drawn again", () => {
  it("counts a stat to its new value under the same key and ends on it", () => {
    draw(stats("2/8"));
    expect(valueText()).toBe("2/8");
    expect(host.querySelector("[data-rolling]")).toBeNull(); // the first draw doesn't move
    draw(stats("6/8"));
    expect(valueText()).toBe("2/8"); // the change starts from what was shown
    expect(host.querySelector("[data-rolling]")).not.toBeNull();
    tick(0); // the first frame starts the clock
    tick(TWEEN_MS / 4);
    const mid = Number(valueText()!.split("/")[0]);
    expect(mid).toBeGreaterThan(2);
    expect(mid).toBeLessThanOrEqual(6);
    expect(valueText()).toMatch(/^\d\/8$/); // never a half-written number
    settle();
    expect(valueText()).toBe("6/8");
    expect(host.querySelector("[data-rolling]")).toBeNull(); // at rest: just the text
  });

  it("fades in a value that isn't the same kind of number", () => {
    draw(stats("¥1.20"));
    draw(stats("在做"));
    expect(valueText()).toBe("在做");
    expect(host.querySelector("[data-fading]")).not.toBeNull();
    settle();
    expect(host.querySelector("[data-fading]")).toBeNull();
  });

  it("doesn't move with reduced motion", () => {
    setReduce(true);
    draw(stats("1.2M"));
    draw(stats("8.4M"));
    expect(valueText()).toBe("8.4M");
    expect(host.querySelector("[data-rolling], [data-fading]")).toBeNull();
    expect(frames.size).toBe(0);
  });

  it("crossfades a badge's tone and brings rows in and out by key", () => {
    const rows = (...rs: [string, string, "caution" | "danger"][]): UIView => ({
      sections: [{ items: rs.map(([key, text, tone]) => ({ type: "row", key, title: key, badge: { text, tone } }) as UIItem) }],
    });
    draw(rows(["a", "在做", "caution"], ["b", "在做", "caution"]));
    const pill = () => [...host.querySelectorAll("span")].find((s) => s.className.includes("rounded-full"))!;
    expect(pill().className).toContain("transition-[background-color,color,box-shadow]");
    const moving = () => [...host.querySelectorAll<HTMLElement>("[style]")].map((e) => e.style.animation).filter((a) => a.includes("240ms"));
    expect(moving()).toEqual([]); // nothing comes in on the first draw
    draw(rows(["a", "等你", "danger"], ["c", "在做", "caution"]));
    expect(pill().className).toContain("text-red");
    expect(host.textContent).toContain("等你");
    expect(host.textContent).toContain("b"); // b fades out where it was
    const animated = moving();
    expect(animated.some((a) => a.includes("reverse"))).toBe(true); // b going
    expect(animated.some((a) => !a.includes("reverse") && a.startsWith("fade-up"))).toBe(true); // c coming
    act(() => vi.runAllTimers());
    expect([...host.querySelectorAll(".truncate")].map((e) => e.textContent)).not.toContain("b");
  });

  it("counts from where it is to a value of another shape without breaking ('1/8' → '5/8' → '7')", () => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    draw(stats("1/8"));
    draw(stats("5/8"));
    tick(0);
    tick(TWEEN_MS / 4); // mid-roll
    expect(host.querySelector("[data-rolling]")).not.toBeNull();
    expect(() => draw(stats("7"))).not.toThrow();
    expect(valueText()).toBe("7"); // a new shape doesn't count: it fades in
    expect(host.querySelector("[data-rolling]")).toBeNull();
    expect(frames.size).toBe(0); // the old roll was stopped
    settle();
    expect(valueText()).toBe("7");
    draw(stats("2/8"));
    draw(stats("3/8"));
    tick(0);
    tick(TWEEN_MS / 4);
    expect(() => draw(stats("—"))).not.toThrow();
    expect(valueText()).toBe("—");
    settle();
    expect(valueText()).toBe("—");
    expect(errors).not.toHaveBeenCalled();
    errors.mockRestore();
  });

  it("is drawn with tabular figures only while it counts", () => {
    draw(stats("2/8"));
    const value = () => host.querySelector("[title]")!;
    expect(value().querySelector(".tabular-nums")).toBeNull();
    expect(value().className).not.toContain("tabular-nums");
    draw(stats("6/8"));
    expect(value().querySelector("[data-rolling]")!.className).toContain("tabular-nums");
    settle();
    expect(value().querySelector(".tabular-nums")).toBeNull();
  });

  it("stops asking for frames when it goes away mid-roll", () => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    draw(stats("2/8"));
    draw(stats("6/8"));
    tick(0);
    tick(TWEEN_MS / 4);
    expect(frames.size).toBe(1);
    act(() => root.unmount());
    expect(frames.size).toBe(0); // the pending frame was cancelled
    root = createRoot(host); // for afterEach
    expect(errors).not.toHaveBeenCalled();
    errors.mockRestore();
  });
});

describe("a chart drawn again", () => {
  // the chart is its own chunk (lib/lazy-page.ts): load it first, so these draws are the chart and not its placeholder
  beforeAll(async () => {
    let run = () => {};
    preloadPages({ requestIdleCallback: (f) => ((run = f), 1), cancelIdleCallback: () => {} });
    run();
    await import("./ExtensionChart");
    await new Promise((ok) => setTimeout(ok, 0));
  });
  const chart = (values: number[], labels = values.map((_, i) => `d${i + 1}`)): UIView => ({
    sections: [{ items: [{ type: "chart", key: "c", kind: "bar", series: [{ name: "s", points: labels.map((label, i) => ({ label, value: values[i] ?? 0 })) }] } as UIItem] }],
  });
  const bars = () => [...host.querySelectorAll("svg path")].map((p) => p.getAttribute("d"));
  /** What a chart with these values looks like drawn for the first time (at rest). */
  const atRest = (v: UIView) => {
    act(() => root.unmount());
    root = createRoot(host);
    draw(v);
    const d = bars();
    act(() => root.unmount());
    root = createRoot(host);
    return d;
  };

  it("moves its bars to the new values and ends exactly on them", () => {
    const end = atRest(chart([6, 8, 3]));
    draw(chart([2, 4, 1]));
    const start = bars();
    expect(start.length).toBe(3);
    draw(chart([6, 8, 3]));
    tick(0);
    tick(TWEEN_MS / 4);
    const mid = bars();
    expect(mid).not.toEqual(end);
    expect(mid.join(" ")).not.toContain("NaN");
    settle();
    expect(bars()).toEqual(end);
  });

  it("jumps to a chart of another shape mid-move without a NaN", () => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    const end = atRest(chart([5, 1, 7, 2]));
    draw(chart([2, 4, 1]));
    draw(chart([6, 8, 3]));
    tick(0);
    tick(TWEEN_MS / 4);
    draw(chart([5, 1, 7, 2]));
    expect(host.innerHTML).not.toContain("NaN");
    expect(bars()).toEqual(end); // a new shape is drawn as it is at once
    expect(frames.size).toBe(0);
    settle();
    expect(bars()).toEqual(end);
    expect(errors).not.toHaveBeenCalled();
    errors.mockRestore();
  });
});
