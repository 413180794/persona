// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { join } from "node:path";
import { LANG_MATCHES, LANGS, preferredLanguage } from "./i18n";
import { localeDirs, preloadScript } from "./startup-preload";

const langs = localeDirs(join(__dirname, "i18n/locales"));

const files = {
  shared: ["assets/react.js"],
  app: ["assets/App.js", "assets/App.css"],
  screen: ["assets/ScreenView.js"],
  catalogs: Object.fromEntries(Object.keys(langs).map((lang) => [lang, `assets/${lang}.js`])),
  matches: Object.fromEntries(Object.entries(langs).flatMap(([lang, ms]) => ms.map((m) => [m.toLowerCase(), lang]))),
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
    expect(Object.keys(langs).sort()).toEqual([...LANGS].sort());
    expect(files.matches).toEqual(LANG_MATCHES);
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
