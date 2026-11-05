// The session header's model name and context bar (#2430): always shown, whatever 「Agent 行显示详情」 says.
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { setAgentDetails, setContextAsUsed } from "../../lib/context-usage";
import type { Session } from "../../lib/types";
import { ContextMeter } from "./ContextMeter";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

const s: Session = { machine_id: "m", session_id: "s", title: "t", cwd: "/code", status: "idle", model: "claude-opus-5-5", context_tokens: 78000, context_window: 200000 };

describe("ContextMeter", () => {
  afterEach(() => {
    setAgentDetails(false);
    setContextAsUsed(false);
  });
  it("shows even while the sidebar's details setting is off", () => {
    setAgentDetails(false);
    expect(renderToStaticMarkup(<ContextMeter session={s} />)).toContain("61%");
  });
  it("shows the model, a bar and the share left, with the tooltip", () => {
    setAgentDetails(true);
    const html = renderToStaticMarkup(<ContextMeter session={s} />);
    expect(html).toContain("opus 5.5");
    expect(html).toContain("61%");
    expect(html).toContain("还剩 61% 上下文");
    expect(html).toContain('aria-valuenow="61"');
  });
  it("shows what is used when asked, warning from 80%", () => {
    setAgentDetails(true);
    setContextAsUsed(true);
    expect(renderToStaticMarkup(<ContextMeter session={s} />)).toContain("已用 39%");
    const html = renderToStaticMarkup(<ContextMeter session={{ ...s, context_tokens: 170000 }} />);
    expect(html).toContain("已用 85%");
    expect(html).toContain("bg-orange");
    expect(html).toContain("text-orange");
    expect(html).not.toContain("red");
  });
  it("turns red from 90% used (10% left), on the bar and the number", () => {
    const html = renderToStaticMarkup(<ContextMeter session={{ ...s, context_tokens: 180000 }} />);
    expect(html).toContain("bg-red");
    expect(html).toContain("text-red");
    expect(html).not.toContain("orange");
    const left = renderToStaticMarkup(<ContextMeter session={{ ...s, context_tokens: 178000 }} />);
    expect(left).toContain("bg-orange"); // 89% used: still the warning
  });
  it("shows the model alone when the window is unknown, and nothing when neither is", () => {
    setAgentDetails(true);
    const alone = renderToStaticMarkup(<ContextMeter session={{ ...s, context_window: undefined }} />);
    expect(alone).toContain("opus 5.5");
    expect(alone).not.toContain("progressbar");
    expect(renderToStaticMarkup(<ContextMeter session={{ ...s, model: undefined, context_window: undefined }} />)).toBe("");
  });
  it("says what the share is from lg up, keeps the bare number below, and tells what to do near full (#3668)", () => {
    const html = renderToStaticMarkup(<ContextMeter session={s} />);
    expect(html).toContain("上下文剩 61%");
    expect(html).toMatch(/@min-\[1000px\]:hidden[^>]*>61%</);
    expect(html).toContain("自动压缩");
    expect(html).not.toContain("/compact");
    const near = renderToStaticMarkup(<ContextMeter session={{ ...s, context_tokens: 170000 }} />);
    expect(near).toContain("上下文剩 15%");
    expect(near).toContain("/compact");
    setContextAsUsed(true);
    expect(renderToStaticMarkup(<ContextMeter session={s} />)).toContain("上下文已用 39%");
  });
  it("is a button that opens the same words (#4054), keeping the hover tooltip; the model alone is not", () => {
    const html = renderToStaticMarkup(<ContextMeter session={s} />);
    expect(html).toMatch(/^<button type="button" aria-haspopup="dialog" aria-expanded="false"/);
    expect(html).toContain('aria-label="opus 5.5 · 上下文剩 61%"'); // holds the visible words, wide or narrow
    expect(html).toMatch(/title="opus 5.5 · 还剩 61% 上下文\n上下文是 Agent/);
    expect(html).toContain('aria-description="上下文是 Agent 在这段对话里能记住的量'); // read out while it is open, too
    expect(html).toContain("touch-hit");
    expect(html).not.toContain("<div"); // a button holds phrasing content only
    const alone = renderToStaticMarkup(<ContextMeter session={{ ...s, context_window: undefined }} />);
    expect(alone).not.toContain("<button");
  });
});

