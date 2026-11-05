import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { UsageAccount } from "../lib/usage-windows";
import { UsageBars, UsagePopover } from "./UsageBars";

const NOW = new Date(2026, 10, 5, 10, 0, 0).getTime();
const H = 3600_000;
const iso = (ms: number) => new Date(NOW + ms).toISOString();
const account = (id: string, used5: number, used7: number, o: Partial<UsageAccount> = {}): UsageAccount => ({
  id,
  provider: "codex",
  name: `账号 ${id}`,
  sampled_at: iso(-12_000),
  windows: [
    { label: "5h", window_minutes: 300, used_percent: used5, resets_at: iso(3 * H) },
    { label: "7d", window_minutes: 10080, used_percent: used7, resets_at: iso(2 * 24 * H + 3 * H) },
  ],
  ...o,
});

describe("the sidebar's usage strip", () => {
  it("is not drawn without data", () => {
    expect(renderToStaticMarkup(<UsageBars accounts={[]} now={NOW} />)).toBe("");
    // all samples older than three days
    expect(renderToStaticMarkup(<UsageBars accounts={[account("a", 10, 10, { sampled_at: iso(-80 * H) })]} now={NOW} />)).toBe("");
  });
  it("shows the two fullest accounts as bars, the caution colour from 80%", () => {
    const html = renderToStaticMarkup(<UsageBars accounts={[account("a", 10, 20), account("b", 85, 30), account("c", 50, 60)]} now={NOW} />);
    expect(html.match(/role="progressbar"/g)).toHaveLength(2);
    expect(html).toContain("账号 b");
    expect(html).toContain("账号 c");
    expect(html).not.toContain("账号 a");
    expect(html).toMatch(/bg-orange[^>]*style="width:85%/);
    expect(html).toMatch(/bg-accent[^>]*style="width:60%/);
    expect(html).toContain('aria-expanded="false"');
  });
  it("marks a stale account", () => {
    expect(renderToStaticMarkup(<UsageBars accounts={[account("a", 10, 20, { stale: true })]} now={NOW} />)).toContain("数据已过期");
  });
});

describe("the popover", () => {
  it("lists every window with percent, countdown, sample age and the pace", () => {
    // 5h window, 2h elapsed = 40% expected: 70% is over; 7d window, 5 days elapsed (~71%): 10% is under
    const html = renderToStaticMarkup(<UsagePopover accounts={[account("a", 70, 10)]} now={NOW} />);
    expect(html).toContain("12秒钟前");
    expect(html).toContain("5h");
    expect(html).toContain("7d");
    expect(html).toContain("70%");
    expect(html).toContain("13:00 重置");
    expect(html).toContain("2d 3h 后重置");
    expect(html).toContain("用得偏快");
    expect(html).toContain("用得偏慢");
    expect(html.match(/data-pace-tick/g)).toHaveLength(2);
  });
  it("draws no pace hint in the first 5% of a window, nor a tick", () => {
    const a = account("a", 90, 90);
    a.windows[0].resets_at = iso(4.9 * H + 0.05 * H); // 5h window just started
    const html = renderToStaticMarkup(<UsagePopover accounts={[a]} now={NOW} />);
    expect(html.match(/data-pace-tick/g)).toHaveLength(1);
    expect(html.match(/用得偏快/g)).toHaveLength(1);
  });
  it("is a labelled dialog", () => {
    expect(renderToStaticMarkup(<UsagePopover accounts={[account("a", 1, 1)]} now={NOW} />)).toContain('role="dialog" aria-label="订阅额度"');
  });
});
