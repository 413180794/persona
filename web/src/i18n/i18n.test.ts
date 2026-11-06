import { afterEach, describe, expect, it, vi } from "vitest";
import { currentLanguage, formatList, formatLocale, LANG_MATCHES, LANG_NAMES, LANGS, initI18n, interpolate, resolveLanguage, t, type CatalogLoaders, type CatalogModule } from "./index";

describe("resolveLanguage (docs/i18n.md)", () => {
  it("maps any Chinese to zh-Hans, any Japanese to ja, the rest to en", () => {
    for (const tag of ["zh", "zh-CN", "zh-Hans", "zh-Hant", "zh-TW", "zh_HK", "ZH-mo"]) expect(resolveLanguage(tag), tag).toBe("zh-Hans");
    for (const tag of ["ja", "ja-JP", "JA_jp"]) expect(resolveLanguage(tag), tag).toBe("ja");
    for (const tag of ["en", "en-GB", "fr", "ko-KR", "de", "", null, undefined]) expect(resolveLanguage(tag), String(tag)).toBe("en");
  });
});

// Loading a whole language's catalogs (hundreds of JSON files) can take seconds on a busy machine.
describe("t", { timeout: 30_000 }, () => {
  afterEach(async () => {
    vi.restoreAllMocks();
    await initI18n("zh-Hans");
  }, 30_000);

  it("reads the loaded language and fills placeholders", async () => {
    expect(currentLanguage()).toBe("zh-Hans");
    expect(t("common.cancel")).toBe("取消");
    await initI18n("en");
    expect(t("common.cancel")).toBe("Cancel");
    expect(t("language.system_detail", { language: "English" })).toBe("Match system (English)");
    await initI18n("ja");
    expect(t("common.cancel")).toBe("キャンセル");
  });

  it("returns an unknown key as itself", () => {
    expect(t("nope.missing")).toBe("nope.missing");
  });

  it("leaves a placeholder with no value visible", () => {
    expect(interpolate("{a} and {b}", { a: 1 })).toBe("1 and {b}");
  });

  // formatList keeps the system's region when the system is in the interface language, and Node's navigator reports the
  // machine's locale: on an en-SG machine an unpinned check got "a, b and c" on every run (#4955).
  it("joins lists the language's way", async () => {
    const system = vi.spyOn(navigator, "languages", "get").mockReturnValue(["en-US"]);
    await initI18n("en");
    expect(formatList(["a", "b", "c"])).toBe("a, b, and c");
    system.mockReturnValue(["en-GB"]);
    expect(formatList(["a", "b", "c"])).toBe("a, b and c");
    system.mockReturnValue(["ja-JP"]);
    expect(formatList(["a", "b", "c"])).toBe("a, b, and c");
  });

  // A test that timed out while switching to en kept loading and switched the next test back (#4955).
  it("keeps the last call's language when an earlier load finishes after it", async () => {
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    const empty: CatalogModule = { default: {}, fallbacks: {} };
    const slow: CatalogLoaders = { "zh-Hans": async () => empty, en: () => held.then(() => empty), ja: async () => empty };
    const first = initI18n("en", slow);
    await initI18n("ja");
    release();
    await first;
    expect(currentLanguage()).toBe("ja");
    expect(t("common.cancel")).toBe("キャンセル");
  });
});


describe("resource-driven languages", () => {
  afterEach(async () => {
    delete LANG_MATCHES.ko;
    vi.restoreAllMocks();
    await initI18n("zh-Hans");
  });

  it("resolves a newly registered subtag without another switch case", () => {
    LANG_MATCHES.ko = "ko";
    expect(resolveLanguage("KO_kr")).toBe("ko");
    for (const lang of LANGS) expect(LANG_NAMES[lang]).not.toBe("");
    expect(resolveLanguage("constructor")).toBe("en");
  });

  it("formats an unsupported system language using the English fallback, not that system language", async () => {
    vi.spyOn(navigator, "languages", "get").mockReturnValue(["fr-FR"]);
    await initI18n("en");
    expect(formatLocale()).toBe("en");
  });

  it("falls back when a forced language has no catalog", async () => {
    await initI18n("xx");
    expect(currentLanguage()).toBe("en");
    expect(t("common.cancel")).toBe("Cancel");
  });
});
