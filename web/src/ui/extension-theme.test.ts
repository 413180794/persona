/// <reference types="node" />
import { describe, expect, it, vi } from "vitest";
vi.mock("../styles.css?raw", async () => {
  const { readFileSync } = await import("node:fs");
  return { default: readFileSync(new URL("../styles.css", import.meta.url), "utf8") };
});
import { allowedTheme, contrast, extensionThemeTokens, themeKey, type ExtensionTheme } from "./extension-theme";

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
  it("rejects hub problems, invalid/unknown roles and insufficient contrast without generating overrides", () => {
    const invalid: ExtensionTheme[] = [{ ...forest, problems: ["unreadable"] }, { ...forest, light: { accent: "#FFFFFF" } }, { ...forest, dark: { accent: "#000000" } }, { ...forest, light: { accent: "red" } }, { ...forest, light: { accent: "#2E7D32", background: "#000000" } }];
    for (const t of invalid) {
      expect(allowedTheme(t)).toBe(false);
      expect(extensionThemeTokens(t, "light")).toEqual({});
    }
  });
});
