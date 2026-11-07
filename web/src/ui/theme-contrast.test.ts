/// <reference types="node" />
import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";
vi.mock("../styles.css?raw", () => ({ default: "" }));
import { contrast } from "./extension-theme";

// The default palette's text tokens must stay readable as small text (WCAG AA 4.5:1) on every background token
// (#2804); the same backgrounds extension themes are checked against (extension-theme.ts).
const css = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
const BACKGROUNDS = ["page", "canvas", "surface", "inset", "hover", "hover-2", "field"];
const TEXT = ["ink", "ink-2", "ink-3", "accent-ink", "attention", "green", "orange", "red", "ai"];

function tokens(scheme: "light" | "dark"): Record<string, string> {
  const block = (scheme === "light" ? /:root\s*\{([^}]+)\}/ : /\.dark\s*\{([^}]+)\}/).exec(css)?.[1] ?? "";
  return Object.fromEntries([...block.matchAll(/--([\w-]+):\s*(#[0-9a-f]{3,8})\s*;/gi)].map((m) => [m[1], m[2]]));
}

describe("default palette contrast", () => {
  for (const scheme of ["light", "dark"] as const) {
    const t = tokens(scheme);
    it(`${scheme}: text tokens reach 4.5:1 on every background`, () => {
      const short = TEXT.flatMap((fg) => BACKGROUNDS.map((bg) => {
        expect(t[fg], `--${fg}`).toMatch(/^#[0-9a-f]{6}$/i);
        expect(t[bg], `--${bg}`).toMatch(/^#[0-9a-f]{3,6}$/i);
        const ratio = contrast(t[fg], t[bg]);
        return ratio < 4.5 ? `--${fg} on --${bg}: ${ratio.toFixed(2)}` : "";
      })).filter(Boolean);
      expect(short).toEqual([]);
    });
    it(`${scheme}: status and selection fills are neutral surfaces`, () => {
      const block = (scheme === "light" ? /:root\s*\{([^}]+)\}/ : /\.dark\s*\{([^}]+)\}/).exec(css)?.[1] ?? "";
      const tints = Object.fromEntries([...block.matchAll(/--([\w-]+-tint):\s*(#[0-9a-f]{3,8})\s*;/gi)].map((m) => [m[1], m[2]]));
      for (const role of ["red", "orange", "green", "ai"]) expect(tints[`${role}-tint`]).toBe(t.inset);
      expect(tints["accent-tint"]).toBe(t["hover-2"]);
      expect(contrast(t["danger-fill"], "#ffffff")).toBeGreaterThanOrEqual(4.5);
      expect(contrast(t["accent-ink"], t.surface)).toBeGreaterThanOrEqual(4.5);
      const bubble = [...css.matchAll(/--neutral-bubble:\s*(#[0-9a-f]{6})/gi)][scheme === "light" ? 0 : 1]?.[1] ?? "";
      expect(bubble).toMatch(/^#[0-9a-f]{6}$/i);
      for (const role of ["ink", "ink-2", "ink-3"]) expect(contrast(t[role], bubble)).toBeGreaterThanOrEqual(4.5);
    });
    it(`${scheme}: --ink-3 stays a step quieter than --ink-2`, () => {
      expect(contrast(t["ink-3"], t.surface)).toBeLessThan(contrast(t["ink-2"], t.surface));
    });
  }
});
