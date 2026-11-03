import { describe, expect, it } from "vitest";
import { CLOCK_SLACK_MS, echoed, isEcho, sameWords } from "./echo";

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
  });

  it("answers one sent message per copy", () => {
    const sent = [{ k: "a", at }, { k: "b", at: at + 1000 }];
    const claimed = new Set<string>();
    const describe = (s: { at: number }) => ({ text: "继续", images: [], at: s.at });
    expect([...echoed(sent, [user("继续", 2)], describe, claimed)].map((s) => s.k)).toEqual(["a"]);
    expect([...echoed(sent.slice(1), [user("继续", 2)], describe, claimed)]).toEqual([]);
    expect([...echoed(sent.slice(1), [user("继续", 2), user("继续", 3)], describe, claimed)].map((s) => s.k)).toEqual(["b"]);
  });
});
