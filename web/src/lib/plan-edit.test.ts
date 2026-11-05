import { describe, expect, it } from "vitest";
import { planEditsChanged, planEditsOf, planEditsToSend, type TaskPlan } from "./feed";

const plan: TaskPlan = {
  id: "p1",
  conversation: "c1",
  state: "open",
  tasks: [
    { title: "补测试", prompt: "给登录补测试", machine: "m1", cwd: "/work/app" },
    { title: "改文档", prompt: "更新文档", machine: "m2", cwd: "/work/docs", model: "opus", permission: "acceptEdits" },
  ],
};

describe("改一下 on a plan card", () => {
  it("starts from every task's fields, an unset permission as 每步都问", () => {
    expect(planEditsOf(plan)).toEqual([
      { title: "补测试", machine: "m1", cwd: "/work/app", model: "", permission: "default", prompt: "给登录补测试" },
      { title: "改文档", machine: "m2", cwd: "/work/docs", model: "opus", permission: "acceptEdits", prompt: "更新文档" },
    ]);
  });

  it("knows when something changed", () => {
    const edits = planEditsOf(plan);
    expect(planEditsChanged(plan, edits)).toBe(false);
    edits[1] = { ...edits[1], cwd: "/work/site" };
    expect(planEditsChanged(plan, edits)).toBe(true);
  });

  it("sends the edits, leaving out a 每步都问 the plan never set", () => {
    const edits = planEditsOf(plan);
    edits[0] = { ...edits[0], title: "补登录测试" };
    const sent = planEditsToSend(plan, edits);
    expect(sent[0]).toEqual({ title: "补登录测试", machine: "m1", cwd: "/work/app", model: "", prompt: "给登录补测试" });
    expect(sent[1].permission).toBe("acceptEdits");
    edits[0] = { ...edits[0], permission: "plan" };
    expect(planEditsToSend(plan, edits)[0].permission).toBe("plan");
  });
});
