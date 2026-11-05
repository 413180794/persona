// A row's right-click menu (iOS InboxView.rowMenu): 已处理 or 移回 first, then what the item can be muted by.
import { describe, expect, it, vi } from "vitest";
import type { InboxItem } from "../../lib/inbox";

const calls = vi.hoisted(() => ({ done: [] as unknown[][], mute: [] as unknown[][] }));
vi.mock("../../lib/inbox", async (original) => ({
  ...(await original<typeof import("../../lib/inbox")>()),
  setDone: (...a: unknown[]) => (calls.done.push(a), Promise.resolve()),
  muteItem: (...a: unknown[]) => (calls.mute.push(a), Promise.resolve()),
}));
vi.mock("../../lib/translate", () => ({ useTranslatePrefs: () => ({ enabled: false }), hubTranslation: () => undefined }));
import { rowMenu } from "./InboxList";

const sender = { tag: "sender", value: "u1", name: "示例甲" };
const chat = { tag: "chat", value: "c1", name: "示例群" };
const item = (over: Partial<InboxItem> = {}): InboxItem => ({
  id: "lark:f1:1", source: "lark", kind: "dm", account: "lark", account_name: "飞书", title: "示例甲", preview: "", count: 1, ...over,
});

describe("收件箱的行菜单", () => {
  it("offers 已处理 alone for an open item with nothing to mute by", () => {
    const m = rowMenu(item(), false);
    expect(m.map((x) => [x.key, x.label])).toEqual([["done", "已处理"]]);
    m[0].onSelect();
    expect(calls.done.at(-1)).toEqual([expect.objectContaining({ id: "lark:f1:1" }), true]);
  });

  it("offers 移回 for a done item", () => {
    const m = rowMenu(item({ done_at: "2026-11-01T09:00:00Z" }), true);
    expect(m.map((x) => x.key)).toEqual(["undone"]);
    m[0].onSelect();
    expect(calls.done.at(-1)?.[1]).toBe(false);
  });

  it("puts the mutes under a divider on the first, each muting with its own option", () => {
    const it0 = item({ mutes: [sender, chat] });
    const m = rowMenu(it0, false);
    expect(m.map((x) => x.key)).toEqual(["done", "mute:sender:u1", "mute:chat:c1"]);
    expect(m.map((x) => !!x.divider)).toEqual([false, true, false]);
    expect(m[1].hint).toBeTruthy();
    m[2].onSelect();
    expect(calls.mute.at(-1)).toEqual([it0, chat]);
  });
});
