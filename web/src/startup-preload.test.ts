// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { LANGS, preferredLanguage } from "./i18n";
import { PRELOAD_LANGS, preloadScript } from "./startup-preload";

const files = {
  shared: ["assets/react.js"],
  app: ["assets/App.js", "assets/App.css"],
  screen: ["assets/ScreenView.js"],
  catalogs: Object.fromEntries(PRELOAD_LANGS.map((lang) => [lang, `assets/${lang}.js`])),
};

// Runs the inline script on a <head> holding only the page's <base> (index.html always has one, set by the hub) and
// returns the hrefs it added, in order.
function run(base = "/"): string[] {
  document.head.innerHTML = `<base href="${base}" />`;
  new Function(preloadScript(files))();
  return [...document.head.querySelectorAll("link")].map((l) => l.getAttribute("href")!.replace(/^\.\//, ""));
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  document.head.innerHTML = "";
});

describe("startup preload script", () => {
  it("has a catalog for every client language", () => {
    expect([...PRELOAD_LANGS].sort()).toEqual([...LANGS].sort());
  });

  it.each([
    [null, ["zh-TW"]],
    [null, ["ja_JP"]],
    [null, ["fr-FR", "zh-CN"]],
    [null, []],
    ["ja", ["en-US"]],
    ["en", ["zh-CN"]],
    ["klingon", ["ja"]],
  ])("picks the catalog preferredLanguage picks (stored %s, system %j)", (stored, languages) => {
    if (stored) localStorage.setItem("persona.language", stored);
    vi.spyOn(navigator, "languages", "get").mockReturnValue(languages);
    vi.spyOn(navigator, "language", "get").mockReturnValue(languages[0] ?? "");
    expect(run()[0]).toBe(files.catalogs[preferredLanguage()]);
  });

  it("preloads App on client pages and only the screen view on the live screen", () => {
    expect(run()).toEqual([files.catalogs[preferredLanguage()], ...files.shared, ...files.app]);
    window.history.pushState(null, "", "/screen/mac");
    try {
      expect(run()).toEqual([files.catalogs[preferredLanguage()], ...files.shared, ...files.screen]);
      window.history.pushState(null, "", "/persona/screen/mac");
      expect(run("/persona/")).toEqual([files.catalogs[preferredLanguage()], ...files.shared, ...files.screen]);
      window.history.pushState(null, "", "/persona/");
      expect(run("/persona/")).toEqual([files.catalogs[preferredLanguage()], ...files.shared, ...files.app]);
    } finally {
      window.history.pushState(null, "", "/");
    }
  });
});
