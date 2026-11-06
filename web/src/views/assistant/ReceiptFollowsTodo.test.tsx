// A done receipt ends a stretch of work, not the task (#6153): while its todo is still open the card says the agent stopped
// with it unfinished, not "done"; once the todo is done, or when the list no longer has it, the card is the green "done".
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it } from "vitest";
import type { FeedItem } from "../../lib/feed";
import { getState } from "../../lib/store";
import type { Todo } from "../../lib/todos";
import { FeedLineView } from "./Tasks";

const now = new Date().toISOString();
const todo = (done_at: string | null): Todo => ({ id: "todo_1", title: "修连接", origin: "c1", created_at: now, updated_at: now, done_at, agents: [] }) as unknown as Todo;
const receipt: FeedItem = { id: "f1", conversation: "c1", at: now, kind: "receipt", ref: "todo_1", body: { title: "修连接", state: "done", summary: "还没修完：授权过期了。" } };

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };
afterEach(() => seed(before));

describe("a done receipt", () => {
  it("says stopped, not done, while its todo is open", () => {
    seed({ todos: [todo(null)] });
    const html = renderToStaticMarkup(<FeedLineView item={receipt} />);
    expect(html).toContain("修连接 停下了，还没完成");
    expect(html).not.toContain("修连接 完成");
    expect(html).not.toContain("bg-green-tint");
  });

  it("is done once its todo is done, or when the list no longer has it", () => {
    for (const todos of [[todo(now)], []]) {
      seed({ todos });
      const html = renderToStaticMarkup(<FeedLineView item={receipt} />);
      expect(html).toContain("修连接 完成");
      expect(html).toContain("bg-green-tint");
    }
  });
});
