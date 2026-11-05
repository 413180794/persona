import { describe, expect, it } from "vitest";
import { CLOCK_SLACK_MS, echoed, isEcho, sameWords, taggedSeen } from "./echo";
import { segments } from "./turns";
import type { Item } from "./types";

const at = Date.parse("2026-11-03T09:00:00Z");
const user = (text: string, secondsAfter = 0, images: unknown[] = []) => ({ id: `u${secondsAfter}`, text, images, ts: new Date(at + secondsAfter * 1000).toISOString() });

describe("finding a sent message in the transcript", () => {
  it("compares the words without whitespace and Claude's placeholders", () => {
    expect(isEcho({ text: "看看现在有多少\n已经合入的", images: [], at }, user("[Image #3] 看看现在有多少 已经合入的"))).toBe(true);
    expect(sameWords("好", "好的，删掉吧")).toBe(false);
    expect(sameWords("一段比较长的粘贴内容", "<pasted_content>一段比较长的粘贴内容</pasted_content>")).toBe(true);
  });

  it("allows the computer's clock to be behind, not an older message", () => {
    expect(isEcho({ text: "继续", images: [], at }, user("继续", -100))).toBe(true);
    expect(isEcho({ text: "继续", images: [], at }, user("继续", -(CLOCK_SLACK_MS / 1000) - 60))).toBe(false);
  });

  it("matches a cut long text by its start, and pictures only by pictures", () => {
    const whole = "这是一条很长很长的消息".repeat(50);
    expect(isEcho({ text: whole, images: [], at }, user(whole.slice(0, 40) + "\n…（已截断，共 9000 字节）"))).toBe(true);
    expect(isEcho({ text: "", images: [{}], at }, user("", 1, [{}]))).toBe(true);
    expect(isEcho({ text: "", images: [{}], at }, user("别的话", 1, [{}]))).toBe(false);
    expect(isEcho({ text: "看图", images: [{}], at }, user("看图", 1))).toBe(false);
    // a picture sent from here comes back as a path the node saved it to (<attached_images>), not an image item
    expect(isEcho({ text: "看图", images: [{}], at }, { ...user("看图", 1), photos: ["/tmp/persona/a.jpg"] })).toBe(true);
  });

  it("answers one sent message per copy", () => {
    const sent = [{ k: "a", at }, { k: "b", at: at + 1000 }];
    const claimed = new Set<string>();
    const describe = (s: { at: number }) => ({ text: "继续", images: [], at: s.at });
    expect([...echoed(sent, [user("继续", 2)], describe, claimed)].map((s) => s.k)).toEqual(["a"]);
    expect([...echoed(sent.slice(1), [user("继续", 2)], describe, claimed)]).toEqual([]);
    expect([...echoed(sent.slice(1), [user("继续", 2), user("继续", 3)], describe, claimed)].map((s) => s.k)).toEqual(["b"]);
  });

  it("takes the hub's tie over the words: other words, a notice, or images still settle the message with that id", () => {
    expect(isEcho({ id: "a", text: "/compact", images: [], at }, { id: "n1", client_msg_id: "a", text: "压缩上下文", images: [] })).toBe(true);
    expect(isEcho({ id: "a", text: "继续", images: [{}], at }, { id: "u1", client_msg_id: "a", text: "别的话", images: [], ts: new Date(at - 3_600_000).toISOString() })).toBe(true);
    const items: Item[] = [
      { id: "x#0", seq: 1, role: "user", kind: "text", text: "继续" },
      { id: "n#0", seq: 2, role: "system", kind: "notice", text: "压缩上下文", client_msg_id: "a" },
      { id: "i#0", seq: 3, role: "user", kind: "image", image: { mime: "image/png" }, client_msg_id: "b" },
      { id: "i#1", seq: 3, role: "user", kind: "image", image: { mime: "image/png" }, client_msg_id: "b" },
    ];
    expect(taggedSeen(items).map((s) => [s.id, s.client_msg_id])).toEqual([["n#0", "a"], ["i#0", "b"]]);
    // a user message carries its tie (from any of its blocks), so the bubbles that pass user messages get it too
    expect(segments([{ id: "y#0", seq: 1, role: "user", kind: "text", text: "看图" }, { id: "y#1", seq: 1, role: "user", kind: "image", image: { mime: "image/png" }, client_msg_id: "b" }])[0].user?.client_msg_id).toBe("b");
    const sent = [{ k: "a", at }, { k: "b", at: at + 1000 }];
    const describe = (s: { k: string; at: number }) => ({ id: s.k, text: s.k === "a" ? "/compact" : "看图", images: [], at: s.at });
    expect([...echoed(sent, taggedSeen(items), describe, new Set())].map((s) => s.k).sort()).toEqual(["a", "b"]);
  });

  it("never gives one message's copy to another with the same words", () => {
    const sent = [{ k: "a", at }, { k: "b", at: at + 1000 }];
    const describe = (s: { k: string; at: number }) => ({ id: s.k, text: "继续", images: [], at: s.at });
    // the copy is tied to b: a (older, same words) is not it
    expect(isEcho(describe(sent[0]), { ...user("继续", 2), client_msg_id: "b" })).toBe(false);
    const claimed = new Set<string>();
    expect([...echoed(sent, [{ ...user("继续", 2), client_msg_id: "b" }], describe, claimed)].map((s) => s.k)).toEqual(["b"]);
    // a tie to a message sent elsewhere answers none of these, a tie to one already settled no other
    expect([...echoed(sent.slice(0, 1), [{ ...user("继续", 3), client_msg_id: "other" }, { ...user("继续", 2), client_msg_id: "b" }], describe, claimed)]).toEqual([]);
    // untagged (an older hub): words, the oldest first
    expect([...echoed(sent.slice(0, 1), [user("继续", 4)], describe, claimed)].map((s) => s.k)).toEqual(["a"]);
  });

  it("finds one that went in without an id by its words, even when the hub tied it to an id of its own", () => {
    expect(isEcho({ text: "你好呀", images: [], at }, { ...user("你好呀", 1), client_msg_id: "hub-made" })).toBe(true);
    expect([...echoed([{ at }], [{ ...user("你好呀", 1), client_msg_id: "hub-made" }], (s) => ({ text: "你好呀", images: [], at: s.at }), new Set())]).toHaveLength(1);
  });
});
