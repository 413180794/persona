// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
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
