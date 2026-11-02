import { describe, expect, it } from "vitest";
import type { Session } from "../../lib/types";
import { arrange, conversationTitle, filterConversations, groupByDay, mergeConversations, splitTitle, type Conversation } from "./conversations";

const now = new Date(2026, 10, 2, 10, 0);
const at = (d: number, h = 9) => new Date(2026, 10, d, h).toISOString();
const conv = (sid: string, p: Partial<Conversation> = {}): Conversation => ({
  machine_id: "server", session_id: sid, title: sid, cwd: "/home/me/.jarvis/assistant", status: "idle", assistant: true, ...p,
});

describe("titles", () => {
  it("splits the hub's titles", () => {
    expect(splitTitle("1101 | 修复 | 搜索框和顶栏重叠")).toEqual({ head: "1101 | 修复 | ", topic: "搜索框和顶栏重叠" });
    expect(splitTitle("早报 · 周三")).toBeNull();
    expect(splitTitle("1101 | 修复")).toBeNull();
  });

  it("calls an untitled one 新对话", () => {
    expect(conversationTitle({ title: "" })).toBe("新对话");
    expect(conversationTitle({ title: "Claude Code" })).toBe("新对话");
    expect(conversationTitle({ title: " 早报 " })).toBe("早报");
  });
});

describe("search", () => {
  const list = [conv("a", { title: "约周五吃饭", last_text: "已经在日历里加好了。" }), conv("b", { title: "Weekly Report", last_text: "" }), conv("c", { title: "" })];
  it("matches the title or the last message, ignoring case and width", () => {
    expect(filterConversations(list, "日历").map((s) => s.session_id)).toEqual(["a"]);
    expect(filterConversations(list, "ｗｅｅｋｌｙ").map((s) => s.session_id)).toEqual(["b"]);
    expect(filterConversations(list, "新对话").map((s) => s.session_id)).toEqual(["c"]);
    expect(filterConversations(list, "  ")).toHaveLength(3);
  });
});

describe("arrange", () => {
  it("groups by day, newest first", () => {
    const g = groupByDay([conv("a", { updated_at: at(1) }), conv("b", { updated_at: at(2) }), conv("c", { updated_at: new Date(2026, 9, 10).toISOString() }), conv("d"), conv("e", { updated_at: new Date(2026, 9, 30).toISOString() })], now);
    expect(g.map((x) => [x.title, x.items.map((s) => s.session_id)])).toEqual([["今天", ["b"]], ["昨天", ["a"]], ["最近 7 天", ["e"]], ["更早", ["c", "d"]]]);
  });

  it("puts the main one first and the archived apart", () => {
    const list = [conv("main", { updated_at: at(1) }), conv("x", { updated_at: at(2) }), conv("old", { updated_at: at(2, 8), archived: true })];
    const a = arrange(list, "main", now);
    expect(a.main?.session_id).toBe("main");
    expect(a.days.flatMap((d) => d.items.map((s) => s.session_id))).toEqual(["x"]);
    expect(a.archived.map((s) => s.session_id)).toEqual(["old"]);
    expect(arrange(list, undefined, now).main).toBeUndefined();
  });

  it("merges what was fetched with the store and the live sessions", () => {
    const fetched = [conv("a", { archived: true, title: "旧标题" }), conv("b")];
    const listed: Session[] = [conv("b", { title: "新的 b" }), conv("n", { title: "刚开的" })];
    const live: Session[] = [conv("a", { title: "新标题", state: "unread" }), conv("other", { assistant: false })];
    const m = mergeConversations(fetched, listed, live);
    expect(m.map((s) => [s.session_id, s.title, s.archived, s.state])).toEqual([
      ["a", "新标题", true, "unread"],
      ["b", "新的 b", undefined, undefined],
      ["n", "刚开的", undefined, undefined],
    ]);
  });
});
