// The web client's i18n (docs/i18n.md, docs/web.md「国际化」). Three languages; the page runs in one of them for its
// whole life: the catalog loads before any other module (main.tsx), so t() works anywhere, module scope included, and
// picking another language reloads the page.

export type Lang = "zh-Hans" | "en" | "ja";
export const LANGS: readonly Lang[] = ["zh-Hans", "en", "ja"];
export type LangPreference = "system" | Lang;

/** Each language by its own name: the picker shows these whatever the interface language is. */
export const LANG_NAMES: Record<Lang, string> = { "zh-Hans": "简体中文", en: "English", ja: "日本語" };

const PREF_KEY = "persona.language";

/** docs/i18n.md: any Chinese (Traditional too) is zh-Hans, any Japanese is ja, everything else falls back to en. */
export function resolveLanguage(tag: string | null | undefined): Lang {
  const primary = (tag ?? "").trim().replace(/_/g, "-").split("-")[0].toLowerCase();
  if (primary === "zh") return "zh-Hans";
  if (primary === "ja") return "ja";
  return "en";
}

/** The system's first preferred language, resolved. Only the first one counts (docs/i18n.md). */
export function systemLanguage(): Lang {
  if (typeof navigator === "undefined") return "en";
  return resolveLanguage(navigator.languages?.[0] ?? navigator.language);
}

export function languagePreference(): LangPreference {
  try {
    const v = localStorage.getItem(PREF_KEY);
    return v && (LANGS as readonly string[]).includes(v) ? (v as Lang) : "system";
  } catch {
    return "system";
  }
}

export function preferredLanguage(pref: LangPreference = languagePreference()): Lang {
  return pref === "system" ? systemLanguage() : pref;
}

type Catalog = Record<string, string>;

let lang: Lang = "zh-Hans";
let catalog: Catalog = {};

/** One dynamic import per language, so a page downloads only the language it runs in. */
export type CatalogLoaders = Record<Lang, () => Promise<{ default: Catalog }>>;

/** The web client's catalog. The admin UI's own namespaces (admin-*) are not in it: admin/main.tsx adds them. */
const loaders: CatalogLoaders = {
  "zh-Hans": () => import("./locales/zh-Hans/index"),
  en: () => import("./locales/en/index"),
  ja: () => import("./locales/ja/index"),
};

/**
 * Loads the client catalog of the preferred language (or `forced`, for tests), plus the `extra` catalogs of that
 * language (the admin UI's). Call before importing anything that says t().
 */
export async function initI18n(forced?: Lang, ...extra: CatalogLoaders[]): Promise<Lang> {
  lang = forced ?? preferredLanguage();
  const parts = await Promise.all([loaders, ...extra].map((l) => l[lang]()));
  catalog = Object.assign({}, ...parts.map((p) => p.default));
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
  if (sys && resolveLanguage(sys) === lang) {
    // a Traditional Chinese system still formats as Simplified: the interface is zh-Hans
    if (lang === "zh-Hans" && /-(hant|tw|hk|mo)\b/i.test(sys)) return "zh-Hans";
    try {
      return Intl.getCanonicalLocales(sys)[0] ?? lang;
    } catch {
      return lang;
    }
  }
  return lang;
}

export type Vars = Record<string, string | number>;

let plural: Intl.PluralRules | null = null;
let pluralLang: Lang | null = null;

function pluralKey(key: string, count: number): string | undefined {
  if (pluralLang !== lang) {
    plural = new Intl.PluralRules(lang);
    pluralLang = lang;
  }
  const form = plural!.select(count);
  return catalog[`${key}_${form}`] ?? catalog[`${key}_other`];
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
  let text: string | undefined;
  if (vars && typeof vars.count === "number") text = pluralKey(key, vars.count);
  text ??= catalog[key];
  if (text === undefined) return key;
  return interpolate(text, vars);
}

/** Whether the page's catalog has `key` (plural forms count). For text that is optional per language. */
export function hasKey(key: string): boolean {
  return key in catalog || `${key}_other` in catalog;
}

// Formatting: always Intl with formatLocale(), never hand-built 月 / 日 strings.

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

/** A list joined the way the language does it: 「A、B 和 C」, "A, B, and C", 「A、B、C」. */
export function formatList(items: string[], opts: Intl.ListFormatOptions = { type: "conjunction" }): string {
  return new Intl.ListFormat(formatLocale(), opts).format(items);
}
