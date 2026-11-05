import { afterEach, describe, expect, it } from "vitest";
import { currentLanguage, formatList, initI18n, interpolate, resolveLanguage, t } from "./index";

describe("resolveLanguage (docs/i18n.md)", () => {
  it("maps any Chinese to zh-Hans, any Japanese to ja, the rest to en", () => {
    for (const tag of ["zh", "zh-CN", "zh-Hans", "zh-Hant", "zh-TW", "zh_HK", "ZH-mo"]) expect(resolveLanguage(tag), tag).toBe("zh-Hans");
    for (const tag of ["ja", "ja-JP", "JA_jp"]) expect(resolveLanguage(tag), tag).toBe("ja");
    for (const tag of ["en", "en-GB", "fr", "ko-KR", "de", "", null, undefined]) expect(resolveLanguage(tag), String(tag)).toBe("en");
  });
});

describe("t", () => {
  afterEach(() => initI18n("zh-Hans"));

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

  it("joins lists the language's way", async () => {
    await initI18n("en");
    expect(formatList(["a", "b", "c"])).toBe("a, b, and c");
  });
});
