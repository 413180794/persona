// @vitest-environment jsdom
// Machines: every card draws its own CPU sparkline. Each one must own its gradient id (no duplicate ids on a page with
// several computers, and no fill borrowed from another card) and be announced as one image with its label.
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { MetricPoint } from "../lib/types";

vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

const { Spark } = await import("./Machines");

const points = (base: number): MetricPoint[] => [0, 1, 2, 3].map((i) => ({ t: 1000 + i * 60, cpu: base + i * 5 }) as MetricPoint);

describe("Spark", () => {
  it("gives each instance its own gradient id and a role=img svg", () => {
    const html = renderToStaticMarkup(
      <div>
        <Spark points={points(10)} />
        <Spark points={points(40)} />
      </div>,
    );
    const host = document.createElement("div");
    host.innerHTML = html;
    const svgs = [...host.querySelectorAll("svg")];
    expect(svgs).toHaveLength(2);
    const ids = svgs.map((svg) => svg.querySelector("linearGradient")?.id ?? "");
    expect(ids.every(Boolean)).toBe(true);
    expect(new Set(ids).size).toBe(2);
    for (const [i, svg] of svgs.entries()) {
      expect(svg.getAttribute("role")).toBe("img");
      expect(svg.getAttribute("aria-label")).toBeTruthy();
      const fills = [...svg.querySelectorAll("polygon")].map((p) => p.getAttribute("fill"));
      expect(fills.length).toBeGreaterThan(0);
      for (const f of fills) expect(f).toBe(`url(#${ids[i]})`);
    }
    // every id on the page is unique
    const all = [...host.querySelectorAll("[id]")].map((el) => el.id);
    expect(new Set(all).size).toBe(all.length);
  });
});
