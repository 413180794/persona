// An agent's own task list as task cards and agent rows show it (#5302, docs/integrations/agent-goal.md "Plan").
import { describe, expect, it } from "vitest";
import { planInRow, planLabel, planLine, planShown } from "./plan";

const plan = { done: 3, total: 5, current: "Reproducing the crash", at: "2026-11-06T10:00:00Z" };

describe("an agent's plan", () => {
  it("is done of total and the step in progress", () => {
    expect(planLine({ plan })).toBe("3/5 · Reproducing the crash");
    expect(planLine({ plan: { done: 2, total: 2 } })).toBe("2/2");
    expect(planLabel({ plan })).toContain("Reproducing the crash");
    expect(planLine({})).toBe("");
    expect(planLine({ plan: { done: 0, total: 0 } })).toBe("");
  });

  it("gives way on a task card to a report that came after it", () => {
    const at = (iso: string) => ({ supervisor: "c1", progress: { kind: "progress" as const, text: "half done", at: iso } });
    expect(planShown({ plan }, undefined)).toBe(true);
    expect(planShown({ plan }, at("2026-11-06T09:59:00Z"))).toBe(true);
    expect(planShown({ plan }, at("2026-11-06T10:01:00Z"))).toBe(false);
    expect(planShown({}, undefined)).toBe(false);
  });

  it("shows in a row while the agent works or items are left", () => {
    expect(planInRow({ plan }, false)).toBe(true);
    expect(planInRow({ plan: { done: 2, total: 2 } }, false)).toBe(false);
    expect(planInRow({ plan: { done: 2, total: 2 } }, true)).toBe(true);
    expect(planInRow({}, true)).toBe(false);
  });
});
