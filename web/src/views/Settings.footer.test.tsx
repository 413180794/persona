// @vitest-environment jsdom
// 设置 › 通用's assistant line with no computer (#4480): not「每段对话在 的 里后台运行。」, and no folder path.
import { describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = (q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} });
});
import { assistantFooter } from "./Settings";

describe("assistantFooter", () => {
  it("says there is no computer yet when the hub has none, with no gaps", () => {
    const none = assistantFooter("", true)!;
    expect(none).toContain("还没接上电脑");
    expect(none).not.toMatch(/在\s+的\s+里/);
  });

  it("names the computer, and never a folder path", () => {
    const on = assistantFooter("我的 MacBook", true)!;
    expect(on).toContain("我的 MacBook");
    expect(on).not.toContain("~/");
  });

  it("says nothing before the assistant is read", () => {
    expect(assistantFooter(undefined, false)).toBeUndefined();
  });
});
