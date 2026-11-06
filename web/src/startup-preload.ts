// The inline <head> script vite.config.ts's startup-preload plugin writes into index.html. It lives here, with no
// imports of the client code, so the config can load it and a test can check it against src/i18n/index.ts: the
// languages it has catalogs for (LANGS) and how it picks one (preferredLanguage) must stay the same as the client's.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

/** The languages under `dir` (src/i18n/locales): code → the language subtags its _meta.json matches. Build time only. */
export function localeDirs(dir: string): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const d of readdirSync(dir, { withFileTypes: true })) {
    if (!d.isDirectory()) continue;
    try {
      out[d.name] = (JSON.parse(readFileSync(join(dir, d.name, "_meta.json"), "utf8")) as { matches: string[] }).matches;
    } catch {
      // not a language directory
    }
  }
  return out;
}

export interface PreloadFiles {
  shared: string[];
  app: string[];
  screen: string[];
  catalogs: Record<string, string>;
  /** language subtag → language code (each _meta.json's matches) */
  matches: Record<string, string>;
}

/** The script: it adds a modulepreload (or style preload) link for the preferred language's catalog and the page's files. */
export function preloadScript(files: PreloadFiles): string {
  return `(function () {
  var files = ${JSON.stringify(files)};
  function add(href) {
    var l = document.createElement("link");
    if (/\\.css$/.test(href)) {
      l.rel = "preload";
      l.as = "style";
    } else l.rel = "modulepreload";
    l.crossOrigin = "";
    l.href = "./" + href;
    document.head.appendChild(l);
  }
  var lang = null;
  try { lang = localStorage.getItem("persona.language"); } catch (e) {}
  if (!Object.prototype.hasOwnProperty.call(files.catalogs, lang)) {
    var tag = (navigator.languages && navigator.languages[0]) || navigator.language || "";
    var primary = tag.trim().replace(/_/g, "-").split("-")[0].toLowerCase();
    lang = Object.prototype.hasOwnProperty.call(files.matches, primary) ? files.matches[primary] : "en";
  }
  var base = new URL(document.baseURI).pathname;
  var path = location.pathname.indexOf(base) === 0 ? location.pathname.slice(base.length - 1) : location.pathname;
  var screen = /^\\/screen\\/[^/]/.test(path);
  [files.catalogs[lang]].concat(files.shared, screen ? files.screen : files.app).forEach(add);
})();`;
}
