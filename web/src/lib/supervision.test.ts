import { describe, expect, it } from "vitest";
import { checkCount, checklist, latestReview, progressLine, reportLine, reviewBadge, reviewState, type Supervision } from "./supervision";
import { todoStatusLabel, type Todo } from "./todos";

const base: Todo = { id: "todo_1", title: "部署 hub", created_at: "2026-11-05T08:00:00Z", updated_at: "2026-11-05T08:00:00Z" };
const sup = (s: Partial<Supervision>): Supervision => ({ supervisor: "main", done_when: ["PR 合并到 master", "截图贴进 PR"], ...s });

describe("supervision", () => {
  it("says the agent's latest report in a line", () => {
    expect(progressLine(undefined)).toBeUndefined();
    expect(progressLine(sup({}))).toBeUndefined();
    expect(progressLine(sup({ progress: { kind: "progress", step: 3, steps: 5, text: "在跑迁移" } }))).toBe("第 3/5 步 · 在跑迁移");
    expect(progressLine(sup({ progress: { kind: "progress", step: 2, text: "在跑迁移" } }))).toBe("第 2 步 · 在跑迁移");
    expect(progressLine(sup({ progress: { kind: "progress", text: "在跑迁移" } }))).toBe("在跑迁移");
    expect(progressLine(sup({ progress: { kind: "question", step: 2, steps: 5, text: "要不要停写？" } }))).toBe("问 Persona：要不要停写？");
    expect(reportLine("blocked", "等 CI")).toBe("卡住了：等 CI");
    expect(reportLine("done", "PR 已合并")).toBe("说做完了：PR 已合并");
  });

  it("has no badge for a task that is not supervised (an older hub)", () => {
    expect(reviewBadge(base)).toBeUndefined();
    expect(checklist(undefined)).toEqual([]);
  });

  it("names each state", () => {
    expect(reviewBadge({ ...base, supervision: sup({}) })?.label).toBe("督办中");
    expect(reviewBadge({ ...base, supervision: sup({ state: "pending_review" }) })).toEqual({ label: "待验收", tone: "accent" });
    expect(reviewBadge({ ...base, supervision: sup({ state: "rework", round: 2 }) })?.label).toBe("返工第 2 轮");
    expect(reviewBadge({ ...base, supervision: sup({ state: "needs_user" }) })?.tone).toBe("orange");
    expect(reviewBadge({ ...base, done_at: "2026-11-05T09:00:00Z", supervision: sup({ state: "accepted" }) })?.label).toBe("已验收");
  });

  it("goes on again when the user reopens an accepted task", () => {
    expect(reviewState({ done_at: null, supervision: sup({ state: "accepted" }) })).toBe("");
    expect(reviewState({ done_at: "2026-11-05T09:00:00Z", supervision: sup({ state: "accepted" }) })).toBe("accepted");
  });

  it("marks each criterion as its latest review found it", () => {
    const s = sup({
      reviews: [
        { at: "1", verdict: "rework", round: 1, checks: [{ criterion: "PR 合并到 master", met: true }, { criterion: "截图贴进 PR", met: false, evidence: "只有浅色" }] },
        { at: "2", verdict: "rework", round: 2, checks: [{ criterion: "截图贴进 PR", met: false, evidence: "深色的也糊了" }, { criterion: "另外的", met: true }] },
      ],
    });
    expect(checklist(s)).toEqual([
      { criterion: "PR 合并到 master", mark: "met", evidence: undefined },
      { criterion: "截图贴进 PR", mark: "missing", evidence: "深色的也糊了" },
      { criterion: "另外的", mark: "met", evidence: undefined },
    ]);
    expect(checkCount(s)).toBe("2/3");
    expect(latestReview(s)?.round).toBe(2);
    expect(checklist(sup({}))[0].mark).toBe("open");
    expect(checkCount(sup({ done_when: [] }))).toBe("");
  });

  it("labels a todo its supervisor closed", () => {
    const done = { ...base, done_at: "2026-11-05T09:00:00Z", done_by: "review" };
    expect(todoStatusLabel({ ...done, supervision: sup({ state: "accepted" }) }, "done")).toBe("已验收");
    expect(todoStatusLabel({ ...done, supervision: sup({ state: "cancelled" }) }, "done")).toBe("已取消");
    expect(todoStatusLabel({ ...base, done_at: "2026-11-05T09:00:00Z", done_by: "auto" }, "done")).toBe("自动完成");
  });
});
