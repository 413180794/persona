import { describe, expect, it } from "vitest";
import { AdminError } from "./api";
import type { UsageSource } from "./types";
import { hubWords, needsKeyAgain, normalUrl, reasonText, sourceStatus, testText, validUrl } from "./usage-sources";

const NOW = Date.parse("2026-11-05T10:00:00Z");
const source = (o: Partial<UsageSource> = {}): UsageSource => ({ id: "s1", name: "Relay", type: "relay", url: "https://gateway.example.com", has_key: true, enabled: true, status: {}, ...o });

describe("how a source is doing", () => {
  it("is off, paused, not read yet, working or failing", () => {
    expect(sourceStatus(source({ enabled: false }), true, NOW)).toMatchObject({ tone: "gray", label: "已停用" });
    expect(sourceStatus(source(), false, NOW)).toMatchObject({ tone: "gray", label: "读取已关闭" });
    expect(sourceStatus(source(), true, NOW)).toMatchObject({ tone: "gray", label: "还没读过" });
    const ok = sourceStatus(source({ status: { ok_at: "2026-11-05T09:58:00Z", seen: { accounts: 12, windows: 31, listed: 14, providers: { codex: 12 } } } }), true, NOW);
    expect(ok.tone).toBe("green");
    expect(ok.detail).toContain("12 个账号");
    expect(ok.detail).toContain("31 个窗口");
    const failing = sourceStatus(source({ status: { ok_at: "2026-11-05T08:00:00Z", error: "unauthorized" } }), true, NOW);
    expect(failing.tone).toBe("red");
    expect(failing.detail).toContain("Key 不对或已过期");
    expect(failing.detail).toContain("上次成功");
    expect(sourceStatus(source({ status: { error: "timeout" } }), true, NOW).detail).toBe("等了太久没有回应");
  });
  it("words every code the hub can give, and falls back to unreachable", () => {
    for (const code of ["unauthorized", "forbidden", "not_found", "unreachable", "timeout", "bad_response", "too_large", "redirect", "server_error", "bad_config"]) {
      expect(reasonText(code)).not.toBe("");
    }
    expect(reasonText("something new")).toBe(reasonText("unreachable"));
  });
});

describe("a test of the connection", () => {
  it("says how many accounts and windows it saw, by provider", () => {
    const r = testText({ ok: true, accounts: 12, windows: 31, listed: 14, providers: { claude: 4, codex: 8 } });
    expect(r.ok).toBe(true);
    expect(r.text).toBe("连上了：12 个账号、31 个窗口 (codex 8 · claude 4)");
  });
  it("says so when it connected but nothing can be drawn", () => {
    expect(testText({ ok: true, accounts: 0, windows: 0, listed: 3, providers: {} })).toEqual({ ok: true, text: "连上了，但没有能显示的额度（网关一共列出 3 个账号）" });
  });
  it("says why it did not", () => {
    expect(testText({ ok: false, code: "unauthorized" })).toEqual({ ok: false, text: "没连上：Key 不对或已过期" });
  });
});

describe("the form", () => {
  it("takes an http(s) address without credentials", () => {
    expect(validUrl("https://gateway.example.com")).toBe(true);
    expect(validUrl(" http://192.0.2.5:8080/base ")).toBe(true);
    for (const bad of ["", "gateway.example.com", "ftp://gateway.example.com", "https://me:pw@gateway.example.com", "https://"]) expect(validUrl(bad)).toBe(false);
  });
  it("compares addresses as the hub keeps them", () => {
    expect(normalUrl("https://gateway.example.com/?a=1#x")).toBe("https://gateway.example.com");
    expect(needsKeyAgain({ url: "https://gateway.example.com", type: "relay" }, { url: "https://gateway.example.com/", type: "relay" })).toBe(false);
    expect(needsKeyAgain({ url: "https://gateway.example.com", type: "relay" }, { url: "https://other.example.com", type: "relay" })).toBe(true);
    expect(needsKeyAgain({ url: "https://gateway.example.com", type: "relay" }, { url: "https://gateway.example.com", type: "generic" })).toBe(true);
  });
  it("shows the hub's own words for the errors it words itself, and no other", () => {
    expect(hubWords(new AdminError(400, "地址不对", { code: "invalid_url" }))).toBe("地址不对");
    expect(hubWords(new AdminError(500, "boom", { code: "internal" }))).toBeNull();
    expect(hubWords(new Error("x"))).toBeNull();
  });
});
