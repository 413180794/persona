// @vitest-environment jsdom
// A chart item's ExtensionChart is its own chunk (lib/lazy-page.ts): until it is in, the item draws the chart's frame
// (header, legend, detail) around an empty plot of the chart's height, so the page-level Suspense never shows and nothing
// below it moves; then the chart replaces it.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { UIView } from "../../lib/extension-ui";
import { INERT, ViewSections } from "./ViewBody";

// The chart's chunk arrives only when the test opens the gate.
const gate = vi.hoisted(() => {
  let open = () => {};
  const opened = new Promise<void>((ok) => (open = ok));
  return { open, opened };
});
vi.mock("./ExtensionChart", async (load) => {
  await gate.opened;
  return load();
});

// jsdom has no ResizeObserver or matchMedia
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.IS_REACT_ACT_ENVIRONMENT = true;
  g.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} };
  g.matchMedia ??= (q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} });
  // jsdom has no layout: the chart draws only once it knows its width
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => 320 });
});

const view: UIView = {
  sections: [{
    items: [{
      type: "chart", kind: "bar", title: "Runs", unit: "min", detail: "Last two days",
      series: [{ name: "ok", points: [{ label: "Mon", value: 3 }, { label: "Tue", value: 5 }] }, { name: "failed", points: [{ label: "Mon", value: 1 }, { label: "Tue", value: 0 }] }],
    }],
  }],
};

let host: HTMLDivElement;
let root: Root;
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("a chart item", () => {
  it("draws the chart's frame at the chart's height while its chunk loads, then draws the chart", async () => {
    host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const tick = () => act(async () => { for (let i = 0; i < 5; i++) await new Promise((ok) => setTimeout(ok, 0)); });
    // waits inside act (5 s at most): the chunk's import can take a while on a busy machine, and it has to finish
    // inside an act for React to draw what it suspended on
    const until = async (check: () => void) => {
      for (let i = 0; i < 500; i++) {
        await act(async () => { await new Promise((ok) => setTimeout(ok, 10)); });
        try {
          return check();
        } catch {
          // not yet
        }
      }
      check();
    };
    const placeholder = () => host.querySelector<HTMLElement>("[data-chart-placeholder]");
    // what surrounds the plot: the header row (title, unit, legend) and the detail line
    const frame = () => {
      const plot = placeholder() ?? host.querySelector("svg")!.parentElement!;
      const outer = plot.parentElement!;
      return [...outer.children].filter((c) => c !== plot).map((c) => c.outerHTML);
    };
    await act(async () => root.render(<ViewSections view={view} {...INERT} />));
    await tick();
    expect(host.querySelector("svg")).toBeNull();
    expect(placeholder()!.style.height).toBe("166px");
    const before = frame();
    expect(before).toHaveLength(2);
    expect(before.join("")).toContain("Runs");
    expect(before.join("")).toContain("failed");
    expect(before.join("")).toContain("Last two days");

    gate.open();
    await until(() => expect(host.querySelector("svg")).not.toBeNull());
    expect(placeholder()).toBeNull();
    // the same frame, and a plot as tall as the empty one was
    expect(frame()).toEqual(before);
    expect(host.querySelector("svg")!.getAttribute("height")).toBe("166");

    // once it is in, a compact chart draws at once, without the empty plot
    await act(async () => root.render(<ViewSections view={view} {...INERT} compact />));
    expect(host.querySelector("svg")).not.toBeNull();
    expect(placeholder()).toBeNull();
  });
});
