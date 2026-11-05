import { describe, expect, it } from "vitest";
import { contextLabel, contextTooltip, contextUsage, contextWarn, modelShort, rowDetail } from "./context-usage";
import type { Session } from "./types";

const base: Session = { machine_id: "m", session_id: "s", title: "t", cwd: "/code/weather", status: "idle" };

describe("contextUsage", () => {
  it("is null while the node cannot tell", () => {
    expect(contextUsage(undefined)).toBeNull();
    expect(contextUsage(base)).toBeNull();
    expect(contextUsage({ context_tokens: 5000 })).toBeNull(); // a model whose window is unknown
    expect(contextUsage({ context_window: 200000 })).toBeNull();
  });
  it("rounds to whole percents and clamps", () => {
    expect(contextUsage({ context_tokens: 78000, context_window: 200000 })).toMatchObject({ used: 39, left: 61 });
    expect(contextUsage({ context_tokens: 250000, context_window: 200000 })).toMatchObject({ used: 100, left: 0 });
  });
});

describe("labels", () => {
  const u = contextUsage({ context_tokens: 78000, context_window: 200000 })!;
  it("says what is left by default and what is used on request", () => {
    expect(contextLabel(u, false)).toBe("61%");
    expect(contextTooltip(u, false)).toBe("还剩 61% 上下文");
    expect(contextLabel(u, true)).toBe("39%");
    expect(contextTooltip(u, true)).toBe("已用 39%");
  });
  it("warns from 80% used", () => {
    expect(contextWarn(contextUsage({ context_tokens: 158000, context_window: 200000 })!)).toBe(false);
    expect(contextWarn(contextUsage({ context_tokens: 160000, context_window: 200000 })!)).toBe(true);
  });
});

describe("modelShort", () => {
  it("shortens Claude ids and keeps the rest", () => {
    expect(modelShort("claude-opus-5-5")).toBe("opus 5.5");
    expect(modelShort("claude-sonnet-4-5-20250929")).toBe("sonnet 4.5");
    expect(modelShort("claude-opus-5-5[1m]")).toBe("opus 5.5 1M");
    expect(modelShort("opus")).toBe("opus");
    expect(modelShort("gpt-5.5")).toBe("gpt-5.5");
    expect(modelShort(undefined)).toBe("");
  });
});

describe("rowDetail", () => {
  const now = Date.parse("2026-11-05T12:00:00Z");
  it("lists folder, model, context and age, leaving out what is unknown", () => {
    const s = { ...base, model: "claude-opus-5-5", context_tokens: 78000, context_window: 200000, updated_at: "2026-11-05T11:55:00Z" };
    expect(rowDetail(s, false, now).map((p) => p.text)).toEqual(["opus 5.5", "61%", "5分钟前", "weather"]);
    expect(rowDetail(s, true, now)[1]).toMatchObject({ text: "39%", warn: false, title: "已用 39%" });
    expect(rowDetail(base, false, now).map((p) => p.text)).toEqual(["weather"]);
  });
  it("marks a nearly full window", () => {
    const s = { ...base, context_tokens: 190000, context_window: 200000 };
    expect(rowDetail(s, false, now).find((p) => p.warn)?.text).toBe("5%");
  });
});
