/// <reference types="node" />
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { allowedTheme, contrast, extensionThemeTokens, pageToken, themeKey, type ExtensionTheme } from "./extension-theme";

const forest: ExtensionTheme = { extension: "example-kit", extension_name: "入门套件", id: "forest", name: "森林", light: { accent: "#2E7D32", positive: "#1B5E20" }, dark: { accent: "#81C784", positive: "#A5D6A7" } };

describe("extension palette tokens", () => {
  it("accepts role colors against the actual stylesheet backgrounds and derives readable accent text", () => {
    expect(allowedTheme(forest)).toBe(true);
    expect(themeKey(forest)).toBe("example-kit/forest");
    const tokens = extensionThemeTokens(forest, "light");
    expect(tokens["--accent"]).toBe("#2E7D32");
    expect(tokens["--green"]).toBe("#1B5E20");
    expect(contrast(tokens["--accent-ink"], "#e7e9eb")).toBeGreaterThanOrEqual(4.5);
    expect(tokens).not.toHaveProperty("--page");
    expect(tokens).not.toHaveProperty("--surface");
  });
  it.each(["light", "dark"] as const)("restores pale %s success/caution/AI fills while selection, danger and pending work stay neutral", (scheme) => {
    const theme: ExtensionTheme = { ...forest, light: { ...forest.light, caution: "#795300", danger: "#a12a35", ai: "#5144b5" }, dark: { ...forest.dark, caution: "#ffc575", danger: "#ff9499", ai: "#b4aaff" } };
    const tokens = extensionThemeTokens(theme, scheme);
    const tint = scheme === "light" ? 10 : 16;
    expect(tokens["--accent-tint"]).toBe("var(--hover-2)");
    expect(tokens["--red-tint"]).toBe("var(--inset)");
    for (const [role, token] of [["positive", "green"], ["caution", "orange"], ["ai", "ai"]] as const) {
      expect(tokens[`--${token}-tint`]).toBe(`color-mix(in srgb, ${theme[scheme][role]} ${tint}%, var(--surface))`);
    }
    expect(tokens).not.toHaveProperty("--attention");
    expect(tokens).not.toHaveProperty("--status-needs");
    expect(tokens).not.toHaveProperty("--user-bubble");
  });
  it("rejects hub problems, invalid/unknown roles and insufficient contrast without generating overrides", () => {
    const invalid: ExtensionTheme[] = [{ ...forest, problems: ["unreadable"] }, { ...forest, light: { accent: "#FFFFFF" } }, { ...forest, dark: { accent: "#000000" } }, { ...forest, light: { accent: "red" } }, { ...forest, light: { accent: "#2E7D32", background: "#000000" } }];
    for (const t of invalid) {
      expect(allowedTheme(t)).toBe(false);
      expect(extensionThemeTokens(t, "light")).toEqual({});
    }
  });
});

describe("page tokens", () => {
  // What pageToken read from the whole stylesheet before it got only the token map (page-tokens.ts).
  const css = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
  const fromStylesheet = (role: string, scheme: "light" | "dark") => {
    const block = (scheme === "light" ? /:root\s*\{([^}]+)\}/ : /\.dark\s*\{([^}]+)\}/).exec(css)?.[1] ?? "";
    return new RegExp(`--${role}:\\s*(#[0-9a-f]+)`, "i").exec(block)?.[1] ?? "";
  };
  // extension-theme.ts's BACKGROUNDS and ROLES colors, plus every token the block declares.
  const roles = ["page", "canvas", "surface", "inset", "hover", "hover-2", "field", "accent", "green", "orange", "red", "ai"];
  const declared = (scheme: "light" | "dark") =>
    [...((scheme === "light" ? /:root\s*\{([^}]+)\}/ : /\.dark\s*\{([^}]+)\}/).exec(css)?.[1] ?? "").matchAll(/--([\w-]+):/g)].map((m) => m[1]);

  it.each(["light", "dark"] as const)("match the stylesheet's first %s block for every background, role and token", (scheme) => {
    expect(declared(scheme).length).toBeGreaterThan(20);
    for (const role of [...roles, ...declared(scheme)]) expect(pageToken(role, scheme), `${scheme} --${role}`).toBe(fromStylesheet(role, scheme));
    expect(pageToken("page", scheme)).toMatch(/^#[0-9a-f]+$/i);
    expect(pageToken("accent", scheme)).toMatch(/^#[0-9a-f]+$/i);
  });

  it("leave out tokens that are not hex colors there", () => {
    expect(pageToken("shadow-card", "light")).toBe("");
    expect(pageToken("no-such-token", "dark")).toBe("");
  });
});
