import { beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  const kept = new Map<string, string>();
  const g = globalThis as Record<string, unknown>;
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

import { getPinnedConversations, unpinAllConversations } from "../lib/conversation-pins";
import type { Session } from "../lib/types";
import { conversationMenuItems, pinnedSectionItems, type ConversationMenuDeps } from "./ConversationMenu";

const conv = (o: Partial<Session> = {}) => ({ machine_id: "hub", session_id: "c1", title: "周报", status: "idle", assistant: true, ...o }) as Session;

function deps(over: Partial<ConversationMenuDeps> = {}) {
  const calls: string[] = [];
  const d: ConversationMenuDeps = {
    rename: async (_s, title) => void calls.push(`rename:${title}`),
    archive: async (s) => void calls.push(`archive:${s.session_id}`),
    end: async (s) => void calls.push(`end:${s.session_id}`),
    refresh: async () => void calls.push("refresh"),
    copy: (text) => void calls.push(`copy:${text}`),
    notify: (text, tone) => void calls.push(`${tone}:${text}`),
    ...over,
  };
  return { d, calls };
}

beforeEach(() => unpinAllConversations());

describe("a Persona conversation's sidebar menu (iOS AssistantSidebar)", () => {
  it("offers 置顶, 重命名…, 归档, 拷贝会话 ID and 结束… for a running conversation", () => {
    const items = conversationMenuItems(conv(), { main: false, pinned: false });
    expect(items.map((i) => i.label)).toEqual(["置顶", "重命名…", "归档", "拷贝会话 ID", "结束…"]);
    expect(items.find((i) => i.key === "end")).toMatchObject({ danger: true, confirm: "确认结束", inline: true });
  });

  it("gives the main conversation only 拷贝会话 ID and 结束…", () => {
    expect(conversationMenuItems(conv(), { main: true, pinned: false }).map((i) => i.key)).toEqual(["copy", "end"]);
  });

  it("has no 结束… once it has stopped", () => {
    expect(conversationMenuItems(conv({ status: "offline" }), { main: false, pinned: true }).map((i) => i.label)).toEqual(["取消置顶", "重命名…", "归档", "拷贝会话 ID"]);
  });

  it("pins in this browser", () => {
    const pin = conversationMenuItems(conv(), { main: false, pinned: false }).find((i) => i.key === "pin")!;
    void pin.onSelect();
    expect(getPinnedConversations()).toEqual(["c1"]);
    expect(pinnedSectionItems(1).map((i) => i.label)).toEqual(["全部取消置顶"]);
    void pinnedSectionItems(1)[0].onSelect();
    expect(getPinnedConversations()).toEqual([]);
    expect(pinnedSectionItems(0)).toEqual([]);
  });

  it("renames, archives, copies the id and ends through the hub, then reads the list again", async () => {
    const { d, calls } = deps();
    const items = conversationMenuItems(conv(), { main: false, pinned: false }, d);
    expect(items.find((i) => i.key === "rename")!.rename!.value).toBe("周报");
    await items.find((i) => i.key === "rename")!.rename!.commit("新标题");
    await items.find((i) => i.key === "archive")!.onSelect();
    await items.find((i) => i.key === "copy")!.onSelect();
    await items.find((i) => i.key === "end")!.onSelect();
    expect(calls).toEqual(["rename:新标题", "ok:已重命名", "refresh", "archive:c1", "ok:已归档「周报」", "refresh", "copy:c1", "end:c1", "ok:已结束对话，记录仍保留", "refresh"]);
  });

  it("keeps the menu open with the reason when ending fails", async () => {
    const { d, calls } = deps({ end: async () => Promise.reject(new Error("offline")) });
    const end = conversationMenuItems(conv(), { main: false, pinned: false }, d).find((i) => i.key === "end")!;
    await expect(end.onSelect()).rejects.toThrow("没能结束对话：offline");
    expect(calls).toEqual([]);
  });
});
