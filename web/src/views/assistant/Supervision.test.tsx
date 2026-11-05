import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { FeedItem } from "../../lib/feed";
import type { Todo } from "../../lib/todos";
import { FeedLineView } from "./Tasks";
import { ReviewBadge, ReviewChecklist } from "./Supervision";

const todo: Todo = {
  id: "todo_1",
  title: "部署 hub",
  created_at: "2026-11-05T08:00:00Z",
  updated_at: "2026-11-05T08:00:00Z",
  supervision: {
    supervisor: "main",
    done_when: ["PR 合并到 master", "浅色 / 深色截图贴进 PR"],
    expect_minutes: 30,
    state: "rework",
    round: 1,
    reviews: [{ at: "2026-11-05T09:00:00Z", verdict: "rework", round: 1, note: "补深色截图",
      checks: [{ criterion: "PR 合并到 master", met: true, evidence: "pull/901 merged" }, { criterion: "浅色 / 深色截图贴进 PR", met: false, evidence: "只有浅色" }] }],
  },
};

describe("a supervised task", () => {
  it("shows its review state and the criteria met on its card", () => {
    const html = renderToStaticMarkup(<ReviewBadge todo={todo} />);
    expect(html).toContain("返工第 1 轮");
    expect(html).toContain("1/2");
    expect(renderToStaticMarkup(<ReviewBadge todo={{ ...todo, supervision: undefined }} />)).toBe("");
  });

  it("lists each criterion with its evidence or gap on its page", () => {
    const html = renderToStaticMarkup(<ReviewChecklist todo={todo} />);
    expect(html).toContain("验收标准");
    expect(html).toContain("预计 30 分钟");
    expect(html).toContain('aria-label="达成"');
    expect(html).toContain('aria-label="缺口"');
    expect(html).toContain("只有浅色");
    expect(html).toContain("督办说明：补深色截图");
  });

  it("says when no criteria were given", () => {
    const html = renderToStaticMarkup(<ReviewChecklist todo={{ ...todo, supervision: { supervisor: "main", done_when: [] } }} />);
    expect(html).toContain("派活时没写验收标准");
  });

  it("draws a review in the conversation as a line", () => {
    const item: FeedItem = { id: "f1", conversation: "main", at: "2026-11-05T09:00:00Z", kind: "review", ref: "todo_1",
      body: { title: "部署 hub", verdict: "accepted", note: "已部署，/healthz ok" } };
    const html = renderToStaticMarkup(<FeedLineView item={item} />);
    expect(html).toContain("已验收");
    expect(html).toContain("已部署，/healthz ok");
  });
});
