// The installed app's launch screen (#1996): dark when the phone is, in the page's own colours (--page of styles.css),
// so starting it offline or before the page has drawn is not a white flash.
import { describe, expect, it } from "vitest";
import manifestText from "../../public/manifest.webmanifest?raw";
// index.html colours the browser's bar with the same two (it says they are --page of styles.css)
import html from "../../index.html?raw";

const manifest = JSON.parse(manifestText);
const meta = (scheme: string) => new RegExp(`theme-color" content="(#[0-9a-f]{6})" media="\\(prefers-color-scheme: ${scheme}\\)"`).exec(html)?.[1];

describe("manifest colours", () => {
  it("starts light on the light page's colour", () => {
    expect(manifest.background_color).toBe(meta("light"));
    expect(manifest.theme_color).toBe(meta("light"));
  });
  it("has a dark launch screen in the dark page's colour", () => {
    const dark = manifest.user_preferences?.color_scheme?.dark;
    expect(meta("dark")).toMatch(/^#/);
    expect(dark?.background_color).toBe(meta("dark"));
    expect(dark?.theme_color).toBe(meta("dark"));
  });
});
