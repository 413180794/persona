// @vitest-environment node
// The catalogs and the code agree (docs/web.md「国际化」): the three languages have the same namespaces, keys and
// placeholders; every t("literal") key exists and gets a value for each placeholder its text uses; and no interface text is written into the code in Chinese. That last
// one is a ratchet while the strings move into catalogs: a file may only lose hard-coded strings, never gain them
// (hardcoded-baseline.json; a file that reaches zero leaves it). Comments and tests don't count; a line that has to
// keep CJK text (it matches what someone else writes, like a hub's output) says `i18n-ignore` in a comment.
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";
import ts from "typescript";
import { describe, expect, it } from "vitest";
import { adminCatalog } from "../admin/catalog";
import { FALLBACK_LANG, LANG_NAMES, LANGS, SOURCE_LANG } from "./index";

const clientFiles = import.meta.glob<Record<string, string>>("./locales/*/_meta.json", { query: "?catalog=client", eager: true, import: "default" });
const clients: Record<string, Record<string, string>> = Object.fromEntries(
  Object.entries(clientFiles).map(([p, c]) => [p.split("?")[0].split("/").slice(-2)[0], c]),
);

const SRC = join(__dirname, "..");
const LOCALES = join(__dirname, "locales");
const BASELINE = join(__dirname, "hardcoded-baseline.json");
const PLURAL = /_(zero|one|two|few|many|other)$/;
const CJK = /[　-〿぀-ヿ㐀-䶿一-鿿！-｠]/;

type Catalog = Record<string, string>;

function catalogs(): Record<string, Record<string, Catalog>> {
  const out: Record<string, Record<string, Catalog>> = {};
  for (const lang of LANGS) {
    out[lang] = {};
    for (const f of readdirSync(join(LOCALES, lang)).filter((f) => f.endsWith(".json") && !f.startsWith("_")).sort()) {
      out[lang][f.replace(/\.json$/, "")] = JSON.parse(readFileSync(join(LOCALES, lang, f), "utf8"));
    }
  }
  return out;
}

const placeholders = (s: string) => [...new Set([...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]))].sort();

/** key without its plural suffix → the placeholders of all its forms */
function shape(cat: Catalog): Map<string, string[]> {
  const m = new Map<string, Set<string>>();
  for (const [k, v] of Object.entries(cat)) {
    const base = k.replace(PLURAL, "");
    const set = m.get(base) ?? new Set<string>();
    for (const p of placeholders(v)) set.add(p);
    m.set(base, set);
  }
  return new Map([...m].map(([k, v]) => [k, [...v].sort()]));
}

function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) {
      if (p === __dirname) continue; // the catalogs themselves
      out.push(...sourceFiles(p));
    } else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) && !name.endsWith(".d.ts") && !/^mock[\w-]*\.ts$/.test(name)) {
      out.push(p);
    }
  }
  return out.sort();
}

interface Scan {
  hardcoded: { line: number; text: string }[];
  /** `vars`: the names a literal second argument gives, [] for none, undefined when the code can't tell (a spread, a computed name, a variable) */
  keys: { line: number; key: string; vars?: string[] }[];
}

/** The property names of t()/tRich()'s second argument, or undefined when they aren't all written out in the call. */
function varNames(arg: ts.Expression | undefined): string[] | undefined {
  if (!arg) return [];
  if (!ts.isObjectLiteralExpression(arg)) return undefined;
  const names: string[] = [];
  for (const p of arg.properties) {
    if (ts.isShorthandPropertyAssignment(p)) names.push(p.name.text);
    else if ((ts.isPropertyAssignment(p) || ts.isMethodDeclaration(p)) && (ts.isIdentifier(p.name) || ts.isStringLiteral(p.name) || ts.isNumericLiteral(p.name))) names.push(p.name.text);
    else return undefined;
  }
  return names;
}

function scan(file: string): Scan {
  const text = readFileSync(file, "utf8");
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const lines = text.split("\n");
  const ignored = (line: number) => /i18n-ignore/.test(lines[line] ?? "") || /^\s*\/\/.*i18n-ignore/.test(lines[line - 1] ?? "");
  const out: Scan = { hardcoded: [], keys: [] };
  // t / tRich are ours only where the file imports them from src/i18n (some files have a local t)
  const ours = new Set<string>();
  for (const m of text.matchAll(/import\s*\{([^}]*)\}\s*from\s*["'][./]*i18n(?:\/rich)?["']/g)) {
    for (const name of m[1].split(",").map((x) => x.trim())) if (name === "t" || name === "tRich") ours.add(name);
  }
  const lineOf = (n: ts.Node) => sf.getLineAndCharacterOfPosition(n.getStart(sf)).line;
  const visit = (n: ts.Node) => {
    let literal: string | undefined;
    if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n) || ts.isTemplateHead(n) || ts.isTemplateMiddle(n) || ts.isTemplateTail(n)) literal = n.text;
    else if (ts.isJsxText(n)) literal = n.text;
    if (literal !== undefined && CJK.test(literal)) {
      const line = lineOf(n);
      if (!ignored(line)) out.hardcoded.push({ line: line + 1, text: literal.trim().slice(0, 40) });
    }
    if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && ours.has(n.expression.text)) {
      const a = n.arguments[0];
      if (a && (ts.isStringLiteral(a) || ts.isNoSubstitutionTemplateLiteral(a))) out.keys.push({ line: lineOf(n) + 1, key: a.text, vars: varNames(n.arguments[1]) });
    }
    ts.forEachChild(n, visit);
  };
  visit(sf);
  return out;
}

describe("languages (resource-only additions)", () => {
  it("are the directories under locales, each complete", () => {
    const dirs = readdirSync(LOCALES).filter((d) => statSync(join(LOCALES, d)).isDirectory()).sort();
    expect([...LANGS].sort()).toEqual(dirs);
    expect(LANGS).toContain(SOURCE_LANG);
    expect(LANGS).toContain(FALLBACK_LANG);
    const claimed = new Map<string, string>();
    for (const lang of dirs) {
      const files = readdirSync(join(LOCALES, lang));
      for (const f of ["_meta.json"]) expect(files, `${lang}/${f}`).toContain(f);
      const meta = JSON.parse(readFileSync(join(LOCALES, lang, "_meta.json"), "utf8")) as { name?: unknown; matches?: unknown };
      expect(typeof meta.name === "string" && meta.name.trim() !== "", `${lang}/_meta.json name`).toBe(true);
      expect(Array.isArray(meta.matches) && meta.matches.length > 0, `${lang}/_meta.json matches`).toBe(true);
      expect(LANG_NAMES[lang]).toBe(meta.name);
      for (const m of meta.matches as string[]) {
        expect(m, `${lang}/_meta.json matches`).toMatch(/^[a-z]{2,3}$/);
        expect(claimed.get(m), `${m} is matched by both ${claimed.get(m)} and ${lang}`).toBeUndefined();
        claimed.set(m, lang);
      }
      expect(meta.matches, `${lang}/_meta.json matches its own language subtag`).toContain(lang.split("-")[0].toLowerCase());
    }
  });
});

describe("catalogs", () => {
  const all = catalogs();
  const source = all[SOURCE_LANG];

  it("has the same namespaces in every language", () => {
    for (const lang of LANGS) expect(Object.keys(all[lang]), lang).toEqual(Object.keys(source));
  });

  it("has the same keys and placeholders in every language", () => {
    for (const ns of Object.keys(source)) {
      const want = shape(source[ns]);
      for (const lang of LANGS) {
        const got = shape(all[lang][ns] ?? {});
        expect([...got.keys()].sort(), `${lang}/${ns}.json keys`).toEqual([...want.keys()].sort());
        for (const [k, ps] of want) expect(got.get(k), `${lang}/${ns}.json "${k}" placeholders`).toEqual(ps);
      }
    }
  });

  it("gives every plural an _other form and no empty text", () => {
    for (const lang of LANGS) {
      for (const [ns, cat] of Object.entries(all[lang])) {
        for (const [k, v] of Object.entries(cat)) {
          expect(v.trim(), `${lang}/${ns}.json "${k}" is empty`).not.toBe("");
          if (PLURAL.test(k)) expect(cat[k.replace(PLURAL, "_other")], `${lang}/${ns}.json "${k}" has no _other`).toBeDefined();
        }
      }
    }
  });

  it("keeps Persona untranslated", () => {
    for (const [ns, cat] of Object.entries(source)) {
      for (const [k, v] of Object.entries(cat)) {
        if (!v.includes("Persona")) continue;
        for (const lang of LANGS) expect(all[lang][ns][k], `${lang}/${ns}.json "${k}"`).toContain("Persona");
      }
    }
  });

  // A file in this repository means nothing to someone using the app and is nowhere they can open it (#3597): say what
  // to do or link to the page. The ones still left may only go away.
  it("names no file of the repository's docs", () => {
    const LEFT = new Set(["admin-pages-usage.url_hint_generic", "settings-connectors.lark_footer_unconfigured"]);
    const found: string[] = [];
    for (const [ns, cat] of Object.entries(source)) {
      for (const k of Object.keys(cat)) {
        if (LANGS.some((lang) => /\bdocs\//.test(all[lang][ns]?.[k] ?? ""))) found.push(`${ns}.${k}`);
      }
    }
    expect(found.sort()).toEqual([...LEFT].sort());
  });
});

describe("client and admin catalogs", () => {
  const ADMIN = /^admin-/;

  it("leaves the admin UI's namespaces out of the client's and puts each namespace in exactly one", async () => {
    const all = catalogs();
    for (const lang of LANGS) {
      const admin = (await adminCatalog[lang]()).default;
      const client = clients[lang];
      expect(Object.keys(client).filter((k) => ADMIN.test(k)), `${lang} client`).toEqual([]);
      expect(Object.keys(admin).filter((k) => !ADMIN.test(k)), `${lang} admin`).toEqual([]);
      const files = Object.entries(all[lang]).flatMap(([ns, cat]) => Object.keys(cat).map((k) => `${ns}.${k}`));
      expect([...Object.keys(client), ...Object.keys(admin)].sort(), lang).toEqual(files.sort());
    }
  });
});

describe("code", () => {
  const files = sourceFiles(SRC);
  const scans = new Map(files.map((f) => [relative(SRC, f), scan(f)]));

  it("uses admin-* keys only in the admin UI, whose page is the only one that loads them", () => {
    const outside = [...scans]
      .filter(([f]) => !f.startsWith("admin/"))
      .flatMap(([f, s]) => s.keys.filter((k) => /^admin-/.test(k.key)).map((k) => `${f}:${k.line} ${k.key}`));
    expect(outside).toEqual([]);
  });

  it("only uses keys that exist", () => {
    const flat = new Set<string>();
    for (const [ns, cat] of Object.entries(catalogs()[SOURCE_LANG])) for (const k of Object.keys(cat)) flat.add(`${ns}.${k.replace(PLURAL, "")}`);
    const missing = [...scans].flatMap(([f, s]) => s.keys.filter((k) => !flat.has(k.key)).map((k) => `${f}:${k.line} ${k.key}`));
    expect(missing).toEqual([]);
  });

  // interpolate() and tRich() leave a placeholder with no value as a literal `{name}` on the page
  it("passes every placeholder a key's text uses", () => {
    const want = new Map<string, string[]>();
    for (const [ns, cat] of Object.entries(catalogs()[SOURCE_LANG])) for (const [k, ps] of shape(cat)) want.set(`${ns}.${k}`, ps);
    const missing = [...scans].flatMap(([f, s]) =>
      s.keys.flatMap(({ line, key, vars }) => {
        if (!vars) return [];
        const lacks = (want.get(key) ?? []).filter((p) => !vars.includes(p));
        return lacks.length ? [`${f}:${line} ${key} lacks ${lacks.join(", ")}`] : [];
      }),
    );
    expect(missing).toEqual([]);
  });

  it("writes no more interface text in Chinese than before", () => {
    const counts: Record<string, number> = {};
    for (const [f, s] of scans) if (s.hardcoded.length) counts[f] = s.hardcoded.length;
    if (process.env.UPDATE_I18N_BASELINE) {
      writeFileSync(BASELINE, JSON.stringify(counts, null, 2) + "\n");
      return;
    }
    const baseline: Record<string, number> = JSON.parse(readFileSync(BASELINE, "utf8"));
    const grew = Object.entries(counts)
      .filter(([f, n]) => n > (baseline[f] ?? 0))
      .map(([f, n]) => `${f}: ${n} (baseline ${baseline[f] ?? 0})\n` + scans.get(f)!.hardcoded.map((h) => `  ${f}:${h.line} ${h.text}`).join("\n"));
    expect(grew, "move these into src/i18n/locales (or mark data with i18n-ignore)").toEqual([]);
    const stale = Object.entries(baseline).filter(([f, n]) => (counts[f] ?? 0) < n).map(([f]) => f);
    expect(stale, "fewer hard-coded strings now: run UPDATE_I18N_BASELINE=1 npx vitest run src/i18n/catalog.test.ts").toEqual([]);
  });
});
