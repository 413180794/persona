// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Segmented } from "../views/assistant/parts";
import { SegmentedTrack, selectedBox } from "./Segmented";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function Demo() {
  const [v, setV] = useState("a");
  return <Segmented label="pick" value={v} onChange={setV} options={[{ v: "a", label: "A" }, { v: "b", label: "B" }]} />;
}
const radio = (name: string) => Array.from(host.querySelectorAll<HTMLElement>('[role="radio"]')).find((e) => e.textContent === name)!;

describe("segmented control", () => {
  it("keeps its radio group, checked state and clicks", () => {
    act(() => root.render(<Demo />));
    expect(host.querySelector('[role="radiogroup"]')?.getAttribute("aria-label")).toBe("pick");
    expect(radio("A").getAttribute("aria-checked")).toBe("true");
    act(() => radio("B").click());
    expect(radio("B").getAttribute("aria-checked")).toBe("true");
    expect(radio("A").getAttribute("aria-checked")).toBe("false");
  });

  it("measures the chosen segment as the thumb's box", () => {
    const track = document.createElement("div");
    track.innerHTML = '<button data-selected="false"></button><button data-selected="true"></button>';
    const [a, b] = Array.from(track.children) as HTMLElement[];
    for (const [el, x, w] of [[a, 2, 40], [b, 44, 52]] as const) {
      Object.defineProperty(el, "offsetLeft", { value: x });
      Object.defineProperty(el, "offsetTop", { value: 2 });
      Object.defineProperty(el, "offsetWidth", { value: w });
      Object.defineProperty(el, "offsetHeight", { value: 28 });
    }
    expect(selectedBox(track)).toEqual({ x: 44, y: 2, w: 52, h: 28 });
  });

  it("draws no thumb while nothing is laid out (jsdom has no layout)", () => {
    act(() => root.render(<SegmentedTrack role="tablist" label="t" thumbClassName=""><button data-selected="true" /></SegmentedTrack>));
    expect(host.querySelector('[data-testid="seg-thumb"]')).toBeNull();
  });
});

/** A ResizeObserver stand-in that records what it watches and lets a test deliver a resize callback. */
class FakeResizeObserver {
  static all: FakeResizeObserver[] = [];
  watched = new Set<Element>();
  observe = vi.fn((el: Element) => void this.watched.add(el));
  unobserve = vi.fn((el: Element) => void this.watched.delete(el));
  disconnect = vi.fn(() => this.watched.clear());
  constructor(private readonly cb: ResizeObserverCallback) {
    FakeResizeObserver.all.push(this);
  }
  fire() {
    this.cb([], this as unknown as ResizeObserver);
  }
}

function lay(el: HTMLElement, box: { x: number; w: number }) {
  Object.defineProperty(el, "offsetLeft", { configurable: true, get: () => box.x });
  Object.defineProperty(el, "offsetTop", { configurable: true, value: 2 });
  Object.defineProperty(el, "offsetWidth", { configurable: true, get: () => box.w });
  Object.defineProperty(el, "offsetHeight", { configurable: true, value: 28 });
}

describe("segmented track resize observer", () => {
  beforeEach(() => {
    FakeResizeObserver.all = [];
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  });
  afterEach(() => vi.unstubAllGlobals());

  const Track = ({ segs, sel, tick }: { segs: string[]; sel: string; tick?: number }) => (
    <SegmentedTrack role="tablist" label="t" thumbClassName="" data-tick={tick}>
      {segs.map((s) => <button key={s} data-selected={s === sel} data-seg={s}>{s}</button>)}
    </SegmentedTrack>
  );
  const seg = (name: string) => host.querySelector<HTMLElement>(`[data-seg="${name}"]`)!;

  it("keeps one observer across re-renders with the same segments", async () => {
    await act(async () => root.render(<Track segs={["a", "b"]} sel="a" tick={0} />));
    expect(FakeResizeObserver.all).toHaveLength(1);
    const ro = FakeResizeObserver.all[0];
    const observes = ro.observe.mock.calls.length;
    expect(observes).toBe(3); // the track and both segments
    for (let i = 1; i <= 5; i++) await act(async () => root.render(<Track segs={["a", "b"]} sel={i % 2 ? "b" : "a"} tick={i} />));
    expect(FakeResizeObserver.all).toHaveLength(1);
    expect(ro.disconnect).not.toHaveBeenCalled();
    expect(ro.observe).toHaveBeenCalledTimes(observes);
    act(() => root.unmount());
    expect(ro.disconnect).toHaveBeenCalledTimes(1);
    root = createRoot(host);
  });

  it("observes an added segment and drops a removed one", async () => {
    await act(async () => root.render(<Track segs={["a", "b"]} sel="a" />));
    const ro = FakeResizeObserver.all[0];
    await act(async () => root.render(<Track segs={["a", "b", "c"]} sel="a" />));
    const c = seg("c");
    expect(ro.observe).toHaveBeenCalledWith(c);
    expect(ro.watched.has(c)).toBe(true);
    await act(async () => root.render(<Track segs={["a", "c"]} sel="a" />));
    expect(ro.unobserve).toHaveBeenCalledTimes(1);
    expect([...ro.watched].some((el) => (el as HTMLElement).dataset.seg === "b")).toBe(false);
    expect(ro.watched.has(seg("a")) && ro.watched.has(c)).toBe(true);
  });

  it("moves the thumb when a segment is resized", async () => {
    await act(async () => root.render(<Track segs={["a", "b"]} sel="b" />));
    const a = { x: 2, w: 40 };
    const b = { x: 44, w: 52 };
    lay(seg("a"), a);
    lay(seg("b"), b);
    const ro = FakeResizeObserver.all[0];
    await act(async () => ro.fire());
    const thumb = () => host.querySelector<HTMLElement>('[data-testid="seg-thumb"]')!;
    expect(thumb().style.width).toBe("52px");
    expect(thumb().style.transform).toBe("translate(44px, 2px)");
    a.w = 60;
    b.x = 64;
    b.w = 70;
    await act(async () => ro.fire());
    expect(thumb().style.width).toBe("70px");
    expect(thumb().style.transform).toBe("translate(64px, 2px)");
  });
});
