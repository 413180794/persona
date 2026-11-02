import { describe, expect, it, vi } from "vitest";

// inbox.ts (comparePosition) pulls in the store and the toasts: neither is needed here.
vi.mock("./store", () => ({ onHubEvent: () => () => {} }));
vi.mock("../ui/atoms", () => ({ toast: vi.fn() }));

import { before, initialCursor, LarkReadTracker } from "./lark-read";
import type { LarkMessage } from "./inbox";

const msg = (pos: number, patch: Partial<LarkMessage> = {}): LarkMessage => ({ id: `m${pos}`, position: String(pos), sender_id: "u1", text: "x", ...patch });
const page = [
  msg(98, { is_read: true }),
  msg(99, { is_mine: true }),
  msg(100, { badge_count: 1 }),
  msg(101, { type: "SYSTEM" }),
  msg(102, { badge_count: 2 }),
  msg(103, { badge_count: 3 }),
];

describe("LarkReadTracker", () => {
  it("reports up to the newest message seen past the cursor, with others' unread ids", () => {
    const t = new LarkReadTracker(before("100"));
    expect(t.see("98")).toBe(true);
    expect(t.see("100")).toBe(true);
    expect(t.see("102")).toBe(true);
    expect(t.see("102")).toBe(false);
    expect(t.report(page)).toEqual({ position: "102", badge_count: 2, message_ids: ["m100", "m102"] });
    expect(t.cursor).toBe("102");
  });

  it("only goes forward and never reports twice", () => {
    const t = new LarkReadTracker("99");
    t.see("100");
    expect(t.report(page)?.position).toBe("100");
    expect(t.report(page)).toBeNull();
    t.see("98"); // scrolled back up: older than the cursor
    expect(t.report(page)).toBeNull();
    t.see("103");
    expect(t.report(page)).toEqual({ position: "103", badge_count: 3, message_ids: ["m102", "m103"] });
  });

  it("leaves out the badge and the ids when there are none", () => {
    const t = new LarkReadTracker("98");
    t.see("99");
    expect(t.report(page)).toEqual({ position: "99" });
  });

  it("goes back after a failed report so the next look sends it again", () => {
    const t = new LarkReadTracker("99");
    t.see("100");
    const r = t.report(page)!;
    t.undo(r, "99");
    expect(t.cursor).toBe("99");
    expect(t.report(page)?.position).toBe("100");
  });

  it("reports nothing without a cursor (a topic group)", () => {
    const t = new LarkReadTracker(null);
    t.see("100");
    expect(t.report(page)).toBeNull();
  });
});

describe("initialCursor", () => {
  it("counts the first screen of an open inbox item from its first new message", () => {
    expect(initialCursor("100", page, false)).toBe("99");
  });

  it("otherwise reports only what arrives while the chat is open", () => {
    expect(initialCursor(undefined, page, false)).toBe("103");
    expect(initialCursor(undefined, [], false)).toBe("0");
  });

  it("does not report a topic group", () => {
    expect(initialCursor("100", page, true)).toBeNull();
  });

  it("steps back from big positions without losing precision", () => {
    expect(before("7400000000000000001")).toBe("7400000000000000000");
    expect(before("abc")).toBe("0");
  });
});
