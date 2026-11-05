import { describe, expect, it } from "vitest";
import { dependencyText, dependentError, previewBlocked, previewConsent } from "./extension-model";

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
  it("reads the dependent conflict without assuming an API error class", () => {
    expect(dependentError({ body: { code: "has_dependents", dependents: [{ id: "notes", name: "笔记" }, { id: 123 }] } })).toEqual([{ id: "notes", name: "笔记" }]);
    expect(dependentError(new Error("no"))).toBeNull();
    expect(dependencyText({ id: "notes", state: "old", version: ">=2.0.0", installed: "1.0.0" })).toContain(">=2.0.0");
  });
});
