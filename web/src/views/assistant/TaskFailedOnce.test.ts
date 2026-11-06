// A task that did not start says why once (#4145): on its full card when the conversation draws one, so the plan and
// the receipt only repeat it when there is no such card (an older day's task is a one-line row without the reason).
import { describe, expect, it } from "vitest";
import type { FeedItem } from "../../lib/feed";
import { taskCardDrawn } from "./Tasks";

const now = new Date("2026-11-06T12:00:00");
const item = (o: Partial<FeedItem>): FeedItem => ({ id: "f", conversation: "c", at: "2026-11-06T10:00:00", kind: "task", ref: "t1", ...o });

describe("taskCardDrawn", () => {
  it("is drawn for today's task line", () => {
    expect(taskCardDrawn([item({})], "t1", now)).toBe(true);
  });
  it("is not drawn for an earlier day's task line", () => {
    expect(taskCardDrawn([item({ at: "2026-11-05T10:00:00" })], "t1", now)).toBe(false);
  });
  it("needs a task line for that todo", () => {
    expect(taskCardDrawn([item({ ref: "t2" }), item({ kind: "receipt" })], "t1", now)).toBe(false);
    expect(taskCardDrawn(undefined, "t1", now)).toBe(false);
    expect(taskCardDrawn([item({})], undefined, now)).toBe(false);
  });
});

describe("PlanCard without the task's card", () => {
  it("still says why the task did not start", async () => {
    const { renderToStaticMarkup } = await import("react-dom/server");
    const { createElement } = await import("react");
    const { PlanCard } = await import("./Tasks");
    const plan = { id: "p", conversation: "c", state: "started" as const, tasks: [{ title: "示例任务", prompt: "", machine: "m", cwd: "/", todo_id: "t1", error: "rpc: connection closed" }] };
    expect(renderToStaticMarkup(createElement(PlanCard, { plan }))).toContain("「示例任务」没开起来：rpc: connection closed");
  });
});

describe("PlanCard with a blank reason (#4809)", () => {
  it("says the reason is unknown instead of leaving it empty", async () => {
    const { renderToStaticMarkup } = await import("react-dom/server");
    const { createElement } = await import("react");
    const { PlanCard } = await import("./Tasks");
    const plan = { id: "p", conversation: "c", state: "started" as const, tasks: [{ title: "示例任务", prompt: "", machine: "m", cwd: "/", todo_id: "t1", error: "  \n " }] };
    expect(renderToStaticMarkup(createElement(PlanCard, { plan }))).toContain("「示例任务」没开起来：原因不明");
  });
});
