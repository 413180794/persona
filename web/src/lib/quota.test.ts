import { describe, expect, it } from "vitest";
import { gatewayName, poolTrouble, quotaAmount, quotaCount, quotaFooter, quotaPercent, quotaReset, quotaTone, quotaUnlimited, quotaUsage } from "./quota";

describe("launcher quota", () => {
  it("names the gateway by the hub's name, else its id", () => {
    expect(gatewayName({ gateway: "example-gateway", gateway_name: "示例网关" })).toBe("示例网关");
    expect(gatewayName({ gateway: "example-gateway" })).toBe("example-gateway");
    expect(gatewayName({ gateway: "", gateway_name: " " })).toBe("gateway");
  });

  it("reads amounts by unit", () => {
    expect(quotaAmount({ label: "每周", used: 496, limit: 800, unit: "usd" })).toBe("$496 / $800");
    expect(quotaAmount({ label: "每周", used: 2.5, limit: 10, unit: "usd" })).toBe("$2.50 / $10.00");
    expect(quotaAmount({ label: "每月", used: 6401, limit: 10000, unit: "credits" })).toBe("6401 / 1 万");
    expect(quotaAmount({ label: "TPM", used: 8400, limit: 500000, unit: "tokens" })).toBe("8400 / 50 万 token");
    expect(quotaAmount({ label: "日", used: 3, limit: 100, unit: "requests" })).toBe("3 / 100 次");
    expect(quotaAmount({ label: "5 小时", used_ratio: 0.42, used: 42, limit: 100, unit: "percent" })).toBe("已用 42%");
    expect(quotaAmount({ label: "5 小时", used_ratio: 1, exhausted: true })).toBe("已用完");
    expect(quotaAmount({ label: "x" })).toBe("—");
  });

  it("counts in 万 and 亿", () => {
    expect(quotaCount(287)).toBe("287");
    expect(quotaCount(12000)).toBe("1.2 万");
    expect(quotaCount(210552323)).toBe("2.1 亿");
    expect(quotaUsage({ label: "今日", requests: 287, tokens: 210552323, cost_usd: 123.12 })).toBe("287 次 · 2.1 亿 token · $123");
    expect(quotaUsage({ label: "今日", requests: 1, tokens: 2 })).toBe("1 次 · 2 token");
  });

  it("tones a bar by how much is used", () => {
    expect(quotaTone(0.5)).toBe("accent");
    expect(quotaTone(0.7)).toBe("orange");
    expect(quotaTone(0.9)).toBe("red");
    expect(quotaTone(0.1, true)).toBe("red");
    expect(quotaPercent(1.4)).toBe(100);
    expect(quotaPercent(-1)).toBe(0);
  });

  it("tells trouble from a pool without a quota interface", () => {
    expect(poolTrouble({ name: "a" })).toBe(false);
    expect(poolTrouble({ name: "a", state: "unsupported" })).toBe(false);
    expect(poolTrouble({ name: "a", state: "cooldown" })).toBe(true);
  });

  it("reads resets and the footer", () => {
    const now = Date.parse("2026-11-04T08:00:00Z");
    expect(quotaReset("2026-11-04T07:00:00Z", now)).toBe("已重置");
    expect(quotaReset("2026-11-05T07:00:00Z", now)).toMatch(/重置$/);
    expect(quotaReset("nope", now)).toBe("");
    expect(quotaUnlimited({ gateway: "g" })).toBe(true);
    expect(quotaUnlimited({ gateway: "g", note: "n" })).toBe(false);
    const footer = quotaFooter({ machine: "m1", checked_at: "t", quota: { gateway: "g", key_name: "k" } }, () => "示例电脑", () => "刚刚");
    expect(footer).toBe("网关上的 Key 名：k；由 示例电脑 读取，Key 不离开那台电脑；刚刚读的，一分钟内再打开用这次的。");
    expect(quotaFooter({}, (x) => x, () => "")).toBe("");
  });
});
