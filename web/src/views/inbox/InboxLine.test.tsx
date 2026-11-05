import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem, InboxTree } from "../../lib/inbox";
import { lineRows, LineList } from "./InboxLine";

const apiCalls: string[] = [];
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string) => {
    apiCalls.push(path);
    return Promise.resolve({});
  },
}));

const item = (id: string, title: string, over: Partial<InboxItem> = {}): InboxItem => ({
  id, source: "examplechat", kind: "group", account: "examplechat", account_name: "Example Chat", title, preview: "", count: 1, ...over,
});

describe("收件箱：线索", () => {
  it("lists the chain farthest first, then the items under it, each opening where it lives", () => {
    const tree: InboxTree = {
      node: { kind: "inbox", id: "b" },
      item: item("b", "示例 B"),
      ancestors: [
        { kind: "inbox", id: "a", by: "continues", item: item("a", "示例 A") },
        { kind: "todo", id: "t1", by: "user" },
      ],
      children: [item("c", "示例 C")],
    };
    const rows = lineRows(tree);
    expect(rows.map((r) => [r.label, r.title])).toEqual([["待办", "t1"], ["事件", "示例 A"], ["下一步", "示例 C"]]);
    expect(rows.map((r) => r.route)).toEqual([{ name: "todos", id: "t1" }, { name: "inbox", id: "a" }, { name: "inbox", id: "c" }]);
  });
  it("opens a session at its machine and does not open an item that is gone", () => {
    const rows = lineRows({
      node: { kind: "inbox", id: "b" },
      ancestors: [{ kind: "inbox", id: "gone" }, { kind: "session", id: "m1/s1" }],
      children: [],
    });
    expect(rows[0]).toMatchObject({ label: "Agent 会话", route: { name: "session", mid: "m1", sid: "s1" } });
    expect(rows[1]).toMatchObject({ title: "已不在收件箱里", route: undefined });
  });
  it("draws nothing without a line, one-line buttons with one", () => {
    expect(renderToStaticMarkup(<LineList rows={[]} />)).toBe("");
    const html = renderToStaticMarkup(<LineList rows={[{ key: "k", label: "事件", title: "示例 A", route: { name: "inbox", id: "a" }, up: true }]} />);
    expect(html).toContain("data-inbox-line");
    expect(html).toContain("线索");
    expect(html).toContain("whitespace-nowrap");
    expect(html).toContain("示例 A");
  });
  it("asks the tree route with the id kept readable and a session's slash kept", async () => {
    const { fetchInboxTree, parentRoute } = await import("../../lib/inbox");
    await fetchInboxTree("inbox", "lark:F1:900");
    await fetchInboxTree("session", "m 1/s1");
    expect(apiCalls).toEqual(["/inbox/tree/inbox/lark:F1:900", "/inbox/tree/session/m%201/s1"]);
    expect(parentRoute({ kind: "session", id: "nosplit" })).toBeUndefined();
  });
});
