import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import {
  arrangeConversations, conversationPreview, filterConversations, getLarkHome, LarkChatsStore, listProblem, mergeConversations, setLarkHome, unreadBadge,
  type LarkChatsDeps, type LarkConversation, type LarkConversationPage,
} from "./lark-chats";

const c = (id: string, extra: Partial<LarkConversation> = {}): LarkConversation => ({
  id, name: `会话 ${id}`, is_group: false, unread: 0, at_me: false, muted: false, preview: "", is_mine: false, updated_at: "2026-01-01T10:00:00Z", ...extra,
});

function memoryStorage() {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) };
}

describe("首页显示消息", () => {
  beforeEach(() => vi.stubGlobal("localStorage", memoryStorage()));

  it("is off until this browser turns it on, and stays as set", () => {
    expect(getLarkHome()).toBe(false);
    setLarkHome(true);
    expect(getLarkHome()).toBe(true);
    setLarkHome(false);
    expect(getLarkHome()).toBe(false);
  });
});

describe("conversation helpers", () => {
  it("merges pages without repeating or moving a chat", () => {
    expect(mergeConversations([c("a"), c("b")], [c("b", { unread: 3 }), c("c")]).map((x) => x.id)).toEqual(["a", "b", "c"]);
  });

  it("searches names and the last message of what is loaded", () => {
    const list = [c("a", { name: "设计评审" }), c("b", { preview: "周六集合" }), c("c", { preview: "好的", preview_sender: "Tom" })];
    expect(filterConversations(list, "评审").map((x) => x.id)).toEqual(["a"]);
    expect(filterConversations(list, "集合").map((x) => x.id)).toEqual(["b"]);
    expect(filterConversations(list, "tom").map((x) => x.id)).toEqual(["c"]);
    expect(filterConversations(list, "  ")).toHaveLength(3);
  });

  it("names who wrote the last message", () => {
    expect(conversationPreview(c("a", { preview: "收到", is_mine: true }))).toBe("我：收到");
    expect(conversationPreview(c("a", { preview: "收到", is_group: true, preview_sender: "Tom" }))).toBe("Tom：收到");
    expect(conversationPreview(c("a", { preview: "收到", preview_sender: "Tom" }))).toBe("收到");
    expect(conversationPreview(c("a"))).toBe("");
  });

  it("badges unread counts, or a dot for 标为未读", () => {
    expect(unreadBadge(c("a", { unread: 4 }))).toEqual({ count: 4, muted: false });
    expect(unreadBadge(c("a", { marked_unread: true, muted: true }))).toEqual({ dot: true, muted: true });
    expect(unreadBadge(c("a"))).toEqual({ muted: false });
  });

  it("keeps pinned chats on top and out of the rest", () => {
    const r = arrangeConversations([c("b")], [c("a"), c("b"), c("c")]);
    expect(r.pinned.map((x) => x.id)).toEqual(["b"]);
    expect(r.rest.map((x) => x.id)).toEqual(["a", "c"]);
  });

  it("tells a signed-out Lark gateway and a hub without Feishu apart", () => {
    expect(listProblem(new ApiError(502, "x", { code: "lark_not_logged_in" })).kind).toBe("not_logged_in");
    expect(listProblem(new ApiError(404, "x", {})).kind).toBe("not_configured");
    expect(listProblem(new ApiError(502, "boom", {}))).toEqual({ kind: "other", text: "读不到飞书会话：boom" });
  });
});

function deps(pages: Record<string, LarkConversationPage | Error>, pinned: LarkConversationPage | Error = { account_id: "acc", conversations: [] }) {
  const calls: { cursor?: string; account?: string }[] = [];
  const d: LarkChatsDeps = {
    page: async (cursor, account) => {
      calls.push({ cursor, account });
      const r = pages[cursor ?? ""];
      if (r instanceof Error) throw r;
      return r;
    },
    pinned: async () => {
      if (pinned instanceof Error) throw pinned;
      return pinned;
    },
  };
  return { d, calls, pages };
}

describe("LarkChatsStore", () => {
  it("loads the first page and the pinned chats, then more", async () => {
    const { d } = deps({ "": { account_id: "acc", conversations: [c("a"), c("b")], next_cursor: "2" }, "2": { account_id: "acc", conversations: [c("c")] } },
      { account_id: "acc", conversations: [c("p")] });
    const s = new LarkChatsStore(d);
    await s.refresh();
    expect(s.state.rows.map((x) => x.id)).toEqual(["a", "b"]);
    expect(s.state.pinned.map((x) => x.id)).toEqual(["p"]);
    expect(s.state.account).toBe("acc");
    expect(s.state.cursor).toBe("2");
    await s.more();
    expect(s.state.rows.map((x) => x.id)).toEqual(["a", "b", "c"]);
    expect(s.state.cursor).toBeUndefined();
    expect(s.state.pages).toBe(2);
  });

  it("re-reads the loaded window and drops chats that left; a failed refresh keeps the list", async () => {
    const { d, calls, pages } = deps({ "": { account_id: "acc", conversations: [c("a"), c("b")], next_cursor: "2" }, "2": { account_id: "acc", conversations: [c("c")], next_cursor: "3" } });
    const s = new LarkChatsStore(d);
    await s.refresh();
    await s.more();
    pages[""] = { account_id: "acc", conversations: [c("b")], next_cursor: "2" };
    pages["2"] = { account_id: "acc", conversations: [c("c")] };
    calls.length = 0;
    await s.refresh();
    expect(calls.map((x) => x.cursor)).toEqual([undefined, "2"]);
    expect(calls.every((x) => x.account === "acc")).toBe(true);
    expect(s.state.rows.map((x) => x.id)).toEqual(["b", "c"]);
    pages[""] = new ApiError(502, "down", {});
    await s.refresh();
    expect(s.state.rows.map((x) => x.id)).toEqual(["b", "c"]);
    expect(s.state.problem?.kind).toBe("other");
  });

  it("drops the old account's rows when the account changed and reads again without it", async () => {
    const { d, calls, pages } = deps({ "": { account_id: "one", conversations: [c("a")] } }, { account_id: "two", conversations: [] });
    const s = new LarkChatsStore(d);
    await s.refresh();
    pages[""] = new ApiError(409, "changed", { code: "lark_account_changed" });
    const seen: string[][] = [];
    s.subscribe(() => seen.push(s.state.rows.map((x) => x.id)));
    // the retry (no account) answers for the new account
    const page = d.page;
    d.page = async (cursor, account) => (account ? page(cursor, account) : { account_id: "two", conversations: [c("z")] });
    await s.refresh();
    expect(seen.some((rows) => rows.length === 0)).toBe(true);
    expect(s.state.account).toBe("two");
    expect(s.state.rows.map((x) => x.id)).toEqual(["z"]);
    expect(calls.at(-1)?.account).toBe("one");
  });

  it("stops at a repeated cursor instead of looping", async () => {
    const { d } = deps({ "": { account_id: "acc", conversations: [c("a")], next_cursor: "x" }, x: { account_id: "acc", conversations: [c("b")], next_cursor: "x" } });
    const s = new LarkChatsStore(d);
    await s.refresh();
    await s.more();
    expect(s.state.cursor).toBeUndefined();
    expect(s.state.rows.map((x) => x.id)).toEqual(["a", "b"]);
  });

  it("forgets the list when Feishu signed out", async () => {
    const { d, pages } = deps({ "": { account_id: "acc", conversations: [c("a")] } });
    const s = new LarkChatsStore(d);
    await s.refresh();
    pages[""] = new ApiError(502, "x", { code: "lark_not_logged_in" });
    await s.refresh();
    expect(s.state.rows).toEqual([]);
    expect(s.state.problem?.kind).toBe("not_logged_in");
  });
});
