import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useExtensionPlace } from "../../lib/extension-slots";
import { useUsageWindows, useUsageWindowsStatus, type UsageAccount, type UsageWindow } from "../../lib/usage-windows";
import { SidebarUsage } from "../UsageBars";
import { SubscriptionUsage, SubscriptionUsageCards } from "./SubscriptionUsage";

vi.mock("../../lib/extension-slots", () => ({ useExtensionPlace: vi.fn() }));
vi.mock("../../lib/usage-windows", async (load) => ({
  ...await load<typeof import("../../lib/usage-windows")>(),
  useUsageWindows: vi.fn(),
  useUsageWindowsStatus: vi.fn(),
}));

const now = Date.parse("2026-11-20T07:20:00Z");
const window5 = (used: number, extra: Partial<UsageWindow> = {}): UsageWindow => ({
  label: "5h", window_minutes: 300, used_percent: used, ...extra,
});
const claude = (windows: UsageWindow[], extra: Partial<UsageAccount> = {}): UsageAccount => ({
  id: "claude", provider: "claude", name: "Claude", sampled_at: "2026-11-20T07:15:00Z", windows, ...extra,
});
const page = { extension: "subscription-usage", id: "subscription-usage", title: "订阅用量", placement: "nav_page" } as const;
const cards = (accounts: UsageAccount[]) => renderToStaticMarkup(<SubscriptionUsageCards accounts={accounts} now={now} />);

describe("subscription usage cards", () => {
  it("rounds the reading and shows a Beijing reset countdown and sample age", () => {
    const html = cards([claude([window5(28.000000000000004, {
      sampled_at: "2026-11-20T07:15:00Z", resets_at: "2026-11-20T10:40:00Z",
    })], { machines: ["Example computer"] })]);
    expect(html).toContain("28%");
    expect(html).toContain("3 小时 20 分钟后重置（今天 18:40）");
    expect(html).toMatch(/采样于 5\s*分钟前/);
    expect(html).not.toContain("28.000000");
    expect(html).toContain("设备 · Example computer");
    expect(html).toContain("7d");
  });

  it("handles the next Beijing day and the end of a window", () => {
    const html = cards([claude([
      window5(57, { resets_at: "2026-11-20T16:10:00Z" }),
      { label: "7d", window_minutes: 10080, used_percent: 81, resets_at: "2026-11-20T07:20:00Z" },
    ])]);
    expect(html).toContain("8 小时 50 分钟后重置（明天 00:10）");
    expect(html).toContain("重置时间已过（今天 15:20）");
    expect(html).toContain("这是上一周期的用量，正在等新读数。");
    expect(html).toMatch(/bg-orange[^>]*width:57%/);
    expect(html).toMatch(/bg-red[^>]*width:81%/);
  });

  it("keeps real zeroes but treats the older Claude placeholder as missing", () => {
    expect(cards([claude([window5(0)])])).not.toContain('role="progressbar"');
    const html = cards([claude([window5(0, { resets_at: "2026-11-20T10:40:00Z" })])]);
    expect(html).toContain('aria-valuenow="0"');
    expect(html).toContain("来源未提供采样时间。");
    expect(html).not.toContain("采样于");
  });

  it("uses the shared legacy AGY observation, keeps model groups and marks stale values", () => {
    const account = claude([window5(20.38511, { model: "Gemini" }), window5(0, { model: "Claude / GPT-OSS" })], {
      id: "agy", provider: "agy", name: "AGY", plan: "Google AI Ultra", sources: ["AGY local"], stale_at: "2026-11-20T07:19:00Z",
    });
    const html = cards([account]);
    expect(html).toContain("Google AI Ultra");
    expect(html).toContain("5h · Gemini");
    expect(html).toContain("5h · Claude / GPT-OSS");
    expect(html).toContain("注册来源 · AGY local");
    expect(html).toContain("用量有一段时间没更新了。");
    expect(html).toMatch(/采样于 5\s*分钟前/);
    expect(html).toMatch(/bg-green[^>]*width:20.4%/);
    expect(cards([{ ...account, sample_time_estimated: true }])).not.toContain("采样于");
    expect(cards([{ ...account, sources: ["Another source"] }])).not.toContain("采样于");
  });

  it("shows the retained reading when the source failed and omits an implausible future sample", () => {
    const html = cards([claude([window5(23, { sampled_at: "2026-11-20T07:21:00Z", problem: "unreachable" })])]);
    expect(html).toContain("23%");
    expect(html).toContain("暂时读不到来源，显示上次的用量。");
    expect(html).not.toContain("采样于");
  });
});

describe("usage page and sidebar availability", () => {
  beforeEach(() => {
    vi.mocked(useExtensionPlace).mockReturnValue(page);
    vi.mocked(useUsageWindows).mockReturnValue([]);
    vi.mocked(useUsageWindowsStatus).mockReturnValue("ready");
  });

  it("shows the page's own empty states and falls back to the extension if the feed is unavailable", () => {
    expect(renderToStaticMarkup(<SubscriptionUsage fallback="Extension view" />)).toContain("还没收到这项用量。");
    vi.mocked(useUsageWindowsStatus).mockReturnValue("loading");
    expect(renderToStaticMarkup(<SubscriptionUsage fallback="Extension view" />)).not.toContain("Extension view");
    vi.mocked(useUsageWindowsStatus).mockReturnValue("unavailable");
    expect(renderToStaticMarkup(<SubscriptionUsage fallback="Extension view" />)).toBe("Extension view");
  });

  it("removes the previous sidebar control only when the registered page has measurements", () => {
    vi.mocked(useUsageWindows).mockReturnValue([claude([window5(40)], { sampled_at: new Date().toISOString() })]);
    expect(renderToStaticMarkup(<SidebarUsage />)).toBe("");
    vi.mocked(useExtensionPlace).mockReturnValue(undefined);
    expect(renderToStaticMarkup(<SidebarUsage />)).toContain("data-usage-bars");
    vi.mocked(useExtensionPlace).mockReturnValue(page);
    vi.mocked(useUsageWindows).mockReturnValue([claude([window5(0)], { sampled_at: new Date().toISOString() })]);
    expect(renderToStaticMarkup(<SidebarUsage />)).toContain("data-usage-bars");
  });
});
