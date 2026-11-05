import { describe, expect, it, vi } from "vitest";
import { initI18n } from "../i18n";
import { dependencyText, dependentError, previewBlocked, previewConsent } from "./extension-model";

// Use the real dependency catalogs and translator without loading every unrelated page's catalog in this unit test.
const { dependencyCatalog } = vi.hoisted(() => ({
  dependencyCatalog: (values: Record<string, string>) => Object.fromEntries(Object.entries(values).map(([key, value]) => [`lib-extension-model.${key}`, value])),
}));
vi.mock("../i18n/locales/zh-Hans/index", async () => ({ default: dependencyCatalog((await import("../i18n/locales/zh-Hans/lib-extension-model.json")).default) }));
vi.mock("../i18n/locales/en/index", async () => ({ default: dependencyCatalog((await import("../i18n/locales/en/lib-extension-model.json")).default) }));
vi.mock("../i18n/locales/ja/index", async () => ({ default: dependencyCatalog((await import("../i18n/locales/ja/lib-extension-model.json")).default) }));

describe("extension model consent", () => {
  const pv = { preview_id: "main", permissions_digest: "main-digest", expires_at: "2026-11-05T12:00:00Z" };
  const b = { ...pv, preview_id: "member", permissions_digest: "member-digest" };
  it("confirms every bundled preview, in order, without extra manifest fields", () => {
    expect(previewConsent(pv)).toEqual({ preview_id: "main", permissions_digest: "main-digest" });
    expect(previewConsent({ ...pv, bundle: [b] })).toEqual({ preview_id: "main", permissions_digest: "main-digest", bundle: [{ preview_id: "member", permissions_digest: "member-digest" }] });
  });
  it("blocks missing dependencies and any expired or malformed preview", () => {
    const now = Date.parse("2026-11-05T11:00:00Z");
    expect(previewBlocked(pv, now)).toBe(false);
    for (const p of [{ ...pv, unresolved: ["missing"] }, { ...pv, expires_at: "" }, { ...pv, bundle: [{ ...b, expires_at: "2026-11-05T10:00:00Z" }] }]) expect(previewBlocked(p, now)).toBe(true);
  });
  it("does not label an unready or unknown dependency as satisfied", async () => {
    await initI18n("zh-Hans");
    expect(dependencyText({ id: "base", state: "not_ready", installed: "1.0.0" })).toBe("依赖未就绪");
    expect(dependencyText({ id: "base", state: "future_state" })).toBe("状态待确认");
    expect(dependencyText({ id: "base", state: "ok", installed: "1.0.0" })).toBe("已满足 · 1.0.0");
  });
  it("translates the new dependency states in every supported language", async () => {
    for (const [lang, notReady, unknown] of [
      ["en", "Dependency not ready", "Status not confirmed"],
      ["ja", "依存先の設定待ち", "状態の確認待ち"],
    ] as const) {
      await initI18n(lang);
      expect(dependencyText({ id: "base", state: "not_ready" })).toBe(notReady);
      expect(dependencyText({ id: "base", state: "future_state" })).toBe(unknown);
    }
    await initI18n("zh-Hans");
  });
  it("reads the dependent conflict without assuming an API error class", () => {
    expect(dependentError({ body: { code: "has_dependents", dependents: [{ id: "notes", name: "笔记" }, { id: 123 }] } })).toEqual([{ id: "notes", name: "笔记" }]);
    expect(dependentError(new Error("no"))).toBeNull();
    expect(dependencyText({ id: "notes", state: "old", version: ">=2.0.0", installed: "1.0.0" })).toContain(">=2.0.0");
  });
});
