import { describe, expect, it } from "vitest";
import type { Interaction } from "../../lib/types";
import { cardFold, cardLine, DOCK_MAX, INPUT_MAX, PLAN_MAX, turnStart, visibleShare, showsAnswered } from "./dock";

describe("visibleShare", () => {
  it("is a share of the visible height, capped when asked", () => {
    expect(visibleShare(0.55)).toBe("calc(var(--kb-h) * 0.55)");
    expect(visibleShare(0.35, 240)).toBe("min(240px, calc(var(--kb-h) * 0.35))");
  });

  it("keeps the dock to about half, the text box to 240px or a third of what is visible", () => {
    expect(DOCK_MAX).toBe("calc(var(--kb-h) * 0.55)");
    expect(INPUT_MAX).toBe("min(240px, calc(var(--kb-h) * 0.35))");
    expect(PLAN_MAX).toBe("calc(var(--kb-h) * 0.4)");
  });
});

describe("cardFold", () => {
  const wide = { narrow: false, keyboard: false, open: false };
  const narrow = { narrow: true, keyboard: false, open: false };

  it("shows every card on a wide screen, the keyboard or not", () => {
    expect(cardFold(3, wide)).toEqual({ full: 3, summary: null });
    expect(cardFold(3, { ...wide, keyboard: true })).toEqual({ full: 3, summary: null });
  });

  it("shows the first in full and 还有 N 件 for the rest on a narrow one", () => {
    expect(cardFold(3, narrow)).toEqual({ full: 1, summary: { kind: "more", count: 2 } });
    expect(cardFold(1, narrow)).toEqual({ full: 1, summary: null });
    expect(cardFold(0, narrow)).toEqual({ full: 0, summary: null });
  });

  it("shows them all once 还有 N 件 is tapped", () => {
    expect(cardFold(3, { ...narrow, open: true })).toEqual({ full: 3, summary: null });
  });

  it("folds them all into one line on a phone on its side, until tapped", () => {
    expect(cardFold(3, { ...narrow, short: true })).toEqual({ full: 0, summary: { kind: "short", count: 3 } });
    expect(cardFold(1, { ...narrow, short: true })).toEqual({ full: 0, summary: { kind: "short", count: 1 } });
    expect(cardFold(3, { ...narrow, short: true, open: true })).toEqual({ full: 3, summary: null });
    expect(cardFold(3, { ...narrow, short: true, keyboard: true })).toEqual({ full: 0, summary: { kind: "typing", count: 3 } });
  });

  it("folds them into one line while the keyboard is up, even opened", () => {
    expect(cardFold(3, { ...narrow, keyboard: true })).toEqual({ full: 0, summary: { kind: "typing", count: 3 } });
    expect(cardFold(1, { ...narrow, keyboard: true, open: true })).toEqual({ full: 0, summary: { kind: "typing", count: 1 } });
    expect(cardFold(0, { ...narrow, keyboard: true })).toEqual({ full: 0, summary: null });
  });
});

describe("cardLine", () => {
  const ix = (x: Partial<Interaction>) => ({ id: "i", machine_id: "m", session_id: "s", kind: "question", ...x }) as Interaction;
  it("says what each card asks, else what it waits for", () => {
    expect(cardLine(ix({ questions: [{ question: "用哪个框架？", options: [] }] as Interaction["questions"] }))).toBe("用哪个框架？");
    expect(cardLine(ix({ kind: "permission", tool_name: "Bash", tool_input: { description: "跑测试" } }))).toBe("跑测试");
    expect(cardLine(ix({ kind: "plan" }))).toBe("计划等你审批");
    expect(cardLine(ix({ kind: "screen" }))).toBe("终端等你处理");
  });
});

describe("turnStart", () => {
  it("is the turn's message, else its first item", () => {
    expect(turnStart({ user: { ts: "2026-11-05T01:00:00Z" } as never, start: "2026-11-05T01:00:02Z" })).toBe("2026-11-05T01:00:00Z");
    expect(turnStart({ start: "2026-11-05T01:00:02Z" })).toBe("2026-11-05T01:00:02Z");
    expect(turnStart(undefined)).toBeUndefined();
  });

  it("answered cards fold away with the rest while typing or on a short screen (#1996)", () => {
    expect(showsAnswered({ narrow: true, keyboard: true, open: false })).toBe(false);
    expect(showsAnswered({ narrow: true, keyboard: false, open: false, short: true })).toBe(false);
    expect(showsAnswered({ narrow: true, keyboard: false, open: true, short: true })).toBe(true);
    expect(showsAnswered({ narrow: true, keyboard: false, open: false })).toBe(true);
    expect(showsAnswered({ narrow: false, keyboard: true, open: false })).toBe(true);
  });
});
