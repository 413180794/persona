// The web client's i18n (docs/i18n.md, docs/web.md"Internationalization"). The page runs in one language for its whole life: the
// catalog loads before any other module (main.tsx), so t() works anywhere, module scope included, and picking another
// language reloads the page.
//
// The languages are the directories under ./locales: each holds its namespace files and _meta.json (its own name, the language subtags it is picked for). Adding a language is adding a
// directory; nothing here lists them (docs/i18n.md "Adding a language").

/** A language code: a directory under ./locales (zh-Hans, en, ja…). */
export type Lang = string;
export type LangPreference = "system" | Lang;

/** The source language (catalogs are written in it first) and the fallback for anything else. */
export const SOURCE_LANG: Lang = "zh-Hans";
export const FALLBACK_LANG: Lang = "en";

/** A language's _meta.json. */
export interface LangMeta {
  /** Its own name: the picker shows this whatever the interface language is. */
  name: string;
  /** The BCP 47 language subtags (lowercase) that resolve to it: ["zh"] makes zh-TW show zh-Hans. */
  matches: string[];
}

const dirOf = (path: string) => path.split("?")[0].split("/").slice(-2)[0];
const metaFiles = import.meta.glob<LangMeta>("./locales/*/_meta.json", { eager: true, import: "default" });
const META: Record<Lang, LangMeta> = Object.fromEntries(Object.entries(metaFiles).map(([p, m]) => [dirOf(p), m]));

/** Every language, the source first, then the fallback, then the rest by code. */
export const LANGS: readonly Lang[] = Object.keys(META).sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
function rank(l: Lang) {
  return l === SOURCE_LANG ? 0 : l === FALLBACK_LANG ? 1 : 2;
}

/** Each language by its own name: the picker shows these whatever the interface language is. */
export const LANG_NAMES: Record<Lang, string> = Object.fromEntries(LANGS.map((l) => [l, META[l].name]));

/** Language subtag → language, from every _meta.json (startup-preload.ts gets the same table). */
export const LANG_MATCHES: Record<string, Lang> = Object.fromEntries(
  LANGS.flatMap((l) => META[l].matches.map((m) => [m.toLowerCase(), l] as const)),
);

const PREF_KEY = "persona.language";

/** docs/i18n.md: a tag's language subtag picks the language whose _meta.json lists it (any Chinese, Traditional too,
 *  is zh-Hans); a language the client has no catalog for falls back to en. */
export function resolveLanguage(tag: string | null | undefined): Lang {
  const primary = (tag ?? "").trim().replace(/_/g, "-").split("-")[0].toLowerCase();
  return Object.hasOwn(LANG_MATCHES, primary) ? LANG_MATCHES[primary] : FALLBACK_LANG;
}

/** The system's first preferred language, resolved. Only the first one counts (docs/i18n.md). */
export function systemLanguage(): Lang {
  if (typeof navigator === "undefined") return FALLBACK_LANG;
  return resolveLanguage(navigator.languages?.[0] ?? navigator.language);
}

export function languagePreference(): LangPreference {
  try {
    const v = localStorage.getItem(PREF_KEY);
    return v && LANGS.includes(v) ? v : "system";
  } catch {
    return "system";
  }
}

export function preferredLanguage(pref: LangPreference = languagePreference()): Lang {
  return pref === "system" ? systemLanguage() : pref;
}

type Catalog = Record<string, string>;

let lang: Lang = SOURCE_LANG;
let catalogs: Record<Lang, Catalog> = {};

/** One dynamic import per language, so each language stays an independent cached chunk. */
export type CatalogLoaders = Record<Lang, () => Promise<{ default: Catalog }>>;

/** Language → loader, from a glob's `./locales/<lang>/<file>` keys (admin/catalog.ts uses it for admin.ts too). */
export function byLanguage(glob: Record<string, () => Promise<{ default: Catalog }>>): CatalogLoaders {
  return Object.fromEntries(Object.entries(glob).map(([p, load]) => [dirOf(p), load]));
}

/** The web client's catalog. The admin UI's own namespaces (admin-*) are not in it: admin/main.tsx adds them. */
const loaders = byLanguage(import.meta.glob<{ default: Catalog }>("./locales/*/_meta.json", { query: "?catalog=client" }));

/**
 * Loads the client catalog of the preferred language (or `forced`, for tests), plus the `extra` catalogs of that
 * language (the admin UI's). Call before importing anything that says t().
 */
export async function initI18n(forced?: Lang, ...extra: CatalogLoaders[]): Promise<Lang> {
  const requested = forced ?? preferredLanguage();
  const selected = loaders[requested] ? requested : FALLBACK_LANG;
  // t() is synchronous, so the fallback catalog must be ready before the page starts. The selected language wins.
  const languages = [...new Set([SOURCE_LANG, FALLBACK_LANG])].filter((l) => l !== selected).concat(selected);
  const parts = await Promise.all(languages.map(async (language) => {
    const pieces = await Promise.all([loaders, ...extra].map((l) => l[language]()));
    return [language, Object.assign({}, ...pieces.map((p) => p.default))] as const;
  }));
  catalogs = Object.fromEntries(parts);
  lang = selected;
  if (typeof document !== "undefined") document.documentElement.lang = lang;
  return lang;
}

/** Saves the choice; when it changes the language the page reloads in it (no t() result is cached past a reload). */
export function setLanguagePreference(pref: LangPreference) {
  try {
    if (pref === "system") localStorage.removeItem(PREF_KEY);
    else localStorage.setItem(PREF_KEY, pref);
  } catch {
    // storage refused (private mode): the choice lasts this page only
  }
  if (preferredLanguage(pref) !== lang && typeof location !== "undefined") location.reload();
}

export function currentLanguage(): Lang {
  return lang;
}

/** The locale for Intl: the system's when it is in the interface language (keeps its region, en-GB dates), else the
 *  interface language itself. */
export function formatLocale(): string {
  const sys = typeof navigator === "undefined" ? "" : (navigator.languages?.[0] ?? navigator.language ?? "");
  const primary = sys.replace(/_/g, "-").split("-")[0].toLowerCase();
  if (sys && LANG_MATCHES[primary] === lang) {
    // a Traditional Chinese system still formats as Simplified: the interface is zh-Hans
    if (lang === SOURCE_LANG && /-(hant|tw|hk|mo)\b/i.test(sys)) return SOURCE_LANG;
    try {
      return Intl.getCanonicalLocales(sys)[0] ?? lang;
    } catch {
      return lang;
    }
  }
  return lang;
}

export type Vars = Record<string, string | number>;

const pluralRules = new Map<Lang, Intl.PluralRules>();

function lookup(language: Lang, key: string, vars?: Vars): string | undefined {
  const catalog = catalogs[language] ?? {};
  if (vars && typeof vars.count === "number") {
    let rules = pluralRules.get(language);
    if (!rules) {
      rules = new Intl.PluralRules(language);
      pluralRules.set(language, rules);
    }
    const text = catalog[`${key}_${rules.select(vars.count)}`] ?? catalog[`${key}_other`];
    if (text !== undefined) return text;
  }
  return catalog[key];
}

/** Fills `{name}` placeholders; a missing value leaves the placeholder visible so the gap is obvious. */
export function interpolate(text: string, vars?: Vars): string {
  if (!vars) return text;
  return text.replace(/\{(\w+)\}/g, (m, name: string) => (name in vars ? String(vars[name]) : m));
}

/**
 * The text of `key` (`<namespace>.<name>`) in the page's language. A numeric `count` picks `<key>_one` / `<key>_other`
 * by the language's plural rules when those exist. An unknown key comes back as itself (the catalog test catches it).
 */
export function t(key: string, vars?: Vars): string {
  // Each catalog chooses its own plural form before fallback: merging English suffixes into a CJK catalog
  // would shadow that language's unsuffixed entry (count_items vs the English count_items_other).
  const text = lookup(lang, key, vars) ?? lookup(FALLBACK_LANG, key, vars) ?? lookup(SOURCE_LANG, key, vars);
  return text === undefined ? key : interpolate(text, vars);
}

/** Whether the page's catalog has `key` (plural forms count). For text that is optional per language. */
export function hasKey(key: string): boolean {
  return [lang, FALLBACK_LANG, SOURCE_LANG].some((l) => key in (catalogs[l] ?? {}) || `${key}_other` in (catalogs[l] ?? {}));
}

// Formatting: always Intl with formatLocale(), never hand-built month / day strings.

export function formatNumber(n: number, opts?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(formatLocale(), opts).format(n);
}

export function formatDate(d: Date | number | string, opts: Intl.DateTimeFormatOptions = { dateStyle: "medium" }): string {
  const date = d instanceof Date ? d : new Date(d);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(formatLocale(), opts).format(date);
}

export function formatRelative(value: number, unit: Intl.RelativeTimeFormatUnit, opts: Intl.RelativeTimeFormatOptions = { numeric: "auto" }): string {
  return new Intl.RelativeTimeFormat(formatLocale(), opts).format(value, unit);
}

/** A list joined the way the language does it: Chinese with an ideographic comma and "and", "A, B, and C", Japanese with ideographic commas only. */
export function formatList(items: string[], opts: Intl.ListFormatOptions = { type: "conjunction" }): string {
  return new Intl.ListFormat(formatLocale(), opts).format(items);
}
