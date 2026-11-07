// @vitest-environment node
// The page loads one language's catalog module; the en and zh-Hans entries t() can still fall back to come with it
// (web/i18n-catalogs.ts). A key the language lacks still shows in en, then zh-Hans (docs/protocol/i18n.md).
import { copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, afterEach, describe, expect, it } from "vitest";
import { FALLBACK_LANGS, fallbackEntries } from "../../i18n-catalogs";
import { type CatalogModule, FALLBACK_LANG, initI18n, SOURCE_LANG, t } from "./index";

const fixture: Record<string, Record<string, string>> = {
  "zh-Hans": { both: "中文", only_zh: "只有中文", en_and_zh: "中英", items_other: "{count} 项", plain: "源" },
  en: { both: "English", en_and_zh: "English and Chinese", items_one: "{count} item", items_other: "{count} items", plain_one: "one", plain_other: "many" },
  ja: { both: "日本語", items_other: "{count} 件", plain: "日本" },
};

const root = mkdtempSync(join(tmpdir(), "persona-i18n-fallbacks-"));
copyFileSync(join(__dirname, "locales", "flatten.ts"), join(root, "flatten.ts"));
for (const [lang, entries] of Object.entries(fixture)) {
  mkdirSync(join(root, lang));
  writeFileSync(join(root, lang, "_meta.json"), JSON.stringify({ name: lang, matches: [] }));
  writeFileSync(join(root, lang, "fixture.json"), JSON.stringify(entries));
}
const load = (lang: string) => () => import(/* @vite-ignore */ `${join(root, lang, "_meta.json").replace(/\\/g, "/")}?catalog=client`) as Promise<CatalogModule>;

afterAll(() => rmSync(root, { recursive: true, force: true }));

describe("one catalog per language", () => {
  afterEach(() => initI18n("zh-Hans"), 30_000);

  it("falls back in the order of src/i18n/index.ts", () => {
    expect(FALLBACK_LANGS).toEqual([FALLBACK_LANG, SOURCE_LANG]);
  });

  it("brings only the fallback entries the language cannot answer", async () => {
    const ja = await load("ja")();
    expect(ja.default).toEqual({ "fixture.both": "日本語", "fixture.items_other": "{count} 件", "fixture.plain": "日本" });
    expect(ja.fallbacks).toEqual({
      en: { "fixture.en_and_zh": "English and Chinese" },
      "zh-Hans": { "fixture.only_zh": "只有中文", "fixture.en_and_zh": "中英" },
    });
    expect(Object.keys((await load("en")()).fallbacks)).toEqual(["zh-Hans"]);
  });

  it("t() still falls back to en, then zh-Hans", async () => {
    await initI18n("ja", { ja: load("ja") });
    expect(t("fixture.both")).toBe("日本語");
    expect(t("fixture.en_and_zh")).toBe("English and Chinese");
    expect(t("fixture.only_zh")).toBe("只有中文");
    expect(t("fixture.items", { count: 1 })).toBe("1 件");
    expect(t("fixture.plain", { count: 1 })).toBe("日本");

    await initI18n("en", { en: load("en") });
    expect(t("fixture.only_zh")).toBe("只有中文");
    expect(t("fixture.items", { count: 1 })).toBe("1 item");
    expect(t("fixture.plain")).toBe("源");
  });

  it("keeps a plural form only when the language has neither the base nor its _other", () => {
    expect(fallbackEntries({ "a.n": "x" }, { "a.n_one": "1", "a.n_other": "n", "a.m_one": "1" })).toEqual({ "a.m_one": "1" });
    expect(fallbackEntries({ "a.n_other": "x" }, { "a.n": "plain", "a.n_one": "1" })).toEqual({ "a.n": "plain" });
  });
});
