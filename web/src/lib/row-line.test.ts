// The sidebar row's second line (#3195): what an agent that needs you asks, what one that finished unseen did.
import { describe, expect, it } from "vitest";
import { draftLine, interactionKicker, rowLine } from "./row-line";
import type { Interaction, Session } from "./types";

const s = (o: Partial<Session>): Session => ({ machine_id: "mac", session_id: "s1", title: "补测试", cwd: "/code", status: "idle", ...o });
const ix = (o: Partial<Interaction>): Interaction => ({ id: "i1", machine_id: "mac", session_id: "s1", kind: "question", ...o });

describe("rowLine", () => {
  it("says what a card asks, after its kind", () => {
    const q = ix({ questions: [{ question: "网络层的测试用哪个框架？" } as NonNullable<Interaction["questions"]>[number]] });
    expect(rowLine(s({ state: "needs_you" }), q)).toEqual({ lead: "在问你：", text: "网络层的测试用哪个框架？" });
    expect(rowLine(s({ state: "needs_you" }), ix({ kind: "permission", summary: "swift test" }))).toEqual({ lead: "需要授权：", text: "swift test" });
  });

  it("names a card with nothing to quote by its kind alone", () => {
    expect(rowLine(s({ state: "needs_you" }), ix({ kind: "plan" }))).toEqual({ lead: "计划待批准", text: "" });
  });

  it("says why an agent waits when it has no card", () => {
    expect(rowLine(s({ state: "needs_you", attention_reason: "等你在飞书里确认发布" }))).toEqual({ text: "等你在飞书里确认发布" });
    expect(rowLine(s({ state: "needs_you" }))).toBeUndefined();
  });

  it("says what a finished agent did, as plain text", () => {
    expect(rowLine(s({ state: "unread", last_text: "**发布说明**整理好了：\n- 中文\n- 英文" }))).toEqual({ text: "发布说明整理好了： 中文 英文" });
    expect(rowLine(s({ state: "unread", last_text: "旧的话", work: { phase: "done", line: "三段都写好了" } as Session["work"] }))).toEqual({ text: "✓ 三段都写好了" });
  });

  it("says nothing for the other states", () => {
    for (const state of ["working", "idle", "offline", "unknown"] as const) expect(rowLine(s({ state, last_text: "在跑测试" }))).toBeUndefined();
  });

  it("keeps one row's worth of a long answer", () => {
    expect(rowLine(s({ state: "unread", last_text: "很长".repeat(200) }))!.text).toHaveLength(160);
  });
});

describe("interactionKicker", () => {
  it("is the 待办 page's word for each kind", () => {
    expect(["question", "plan", "permission", "screen", "elicitation"].map((kind) => interactionKicker({ kind } as Interaction))).toEqual(["在问你", "计划待批准", "需要授权", "终端对话框", "MCP 请求"]);
  });
});

describe("draftLine (#5568)", () => {
  it("leads with 草稿 and keeps the words on one line", () => {
    expect(draftLine({ text: "  先跑一遍测试\n\n再看  覆盖率 ", quoted: false })).toEqual({ lead: "草稿：", text: "先跑一遍测试 再看 覆盖率" });
  });
  it("is nothing for whitespace alone, or nothing at all", () => {
    expect(draftLine({ text: " \n\t ", quoted: false })).toBeUndefined();
    expect(draftLine({ text: "", quoted: false })).toBeUndefined();
  });
  it("is just 草稿 when only quotes wait", () => {
    expect(draftLine({ text: " ", quoted: true })).toEqual({ lead: "草稿", text: "" });
  });
  it("keeps one line's worth", () => {
    expect(draftLine({ text: "长".repeat(500), quoted: false })!.text).toHaveLength(160);
  });
});
