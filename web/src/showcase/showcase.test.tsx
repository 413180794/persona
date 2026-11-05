// The 组件展示 extension's pages (#1852) through the web's view: every page reads and draws, and every component the
// schema has is drawn by some page (none is left out as unknown).
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { INERT, ViewSections } from "../views/extensions/ViewBody";
import { SHOWCASE, showcaseKind } from "./snapshots";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

describe("组件展示", () => {
  const names = Object.keys(SHOWCASE);

  it("has every page", () => {
    expect(names.length).toBeGreaterThan(30);
    expect(names).toContain("home");
    expect(names).toContain("list-more");
  });

  it.each(names)("draws %s", (name) => {
    const view = SHOWCASE[name];
    const html = renderToStaticMarkup(<ViewSections view={view} compact={showcaseKind(name) !== "page"} {...INERT} />);
    expect(html.length).toBeGreaterThan(50);
    // every titled section is there
    for (const s of view.sections) if (s.title) expect(html).toContain(s.title.replace(/&/g, "&amp;"));
  });

  it("knows every component", () => {
    const types = new Set(Object.values(SHOWCASE).flatMap((v) => v.sections.flatMap((s) => s.items.map((i) => i.type))));
    for (const t of ["text", "metric", "progress", "row", "button", "link", "field", "divider", "chart", "stat_grid", "segmented", "markdown", "table", "empty", "loading"]) {
      expect(types).toContain(t);
    }
  });
});
