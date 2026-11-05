import { describe, expect, it } from "vitest";
import { buildTaskIndex } from "./tasks";
import { sameValue, stabilizeLines, stableValue } from "./stable-lines";
import { chatLines, type ChatLine } from "./turns";
import type { Item } from "./types";

const u = (seq: number, text: string, extra: Partial<Item> = {}): Item => ({ id: `u${seq}#0`, seq, role: "user", kind: "text", text, ...extra });
const a = (seq: number, text: string): Item => ({ id: `a${seq}#0`, seq, role: "assistant", kind: "text", text });
const tool = (seq: number, id: string): Item => ({ id: `t${seq}#0`, seq, role: "assistant", kind: "tool_use", tool_use_id: id, tool_name: "Bash", tool_input: { command: "ls" } });
const res = (seq: number, id: string, text = "ok"): Item => ({ id: `r${seq}#0`, seq, role: "user", kind: "tool_result", tool_use_id: id, text });

/** chatLines as ChatView runs it: each run shares what did not change with the run before. */
function run(prev: ChatLine[] | undefined, items: Item[], live = false) {
  return stabilizeLines(prev, chatLines(items, { live }));
}

const history = [u(1, "hi"), tool(2, "x"), res(3, "x"), a(4, "done"), u(5, "next"), a(6, "sure")];

describe("stabilizeLines", () => {
  it("keeps every finished line's object when an item is appended", () => {
    const before = run(undefined, history);
    const after = run(before, [...history, u(7, "more")]);
    expect(after).toHaveLength(before.length + 1);
    before.forEach((l, i) => expect(after[i]).toBe(l));
  });

  it("gives back the same array when nothing changed", () => {
    const before = run(undefined, history);
    expect(run(before, [...history])).toBe(before);
  });

  it("counts an item pushed again as a new object with the same fields as the same", () => {
    const before = run(undefined, history);
    const after = run(before, history.map((i) => ({ ...i })));
    expect(after).toBe(before);
  });

  it("gives a new line for the exchange an item changed in, and only for it", () => {
    const before = run(undefined, history);
    const after = run(before, [...history.slice(0, 2), res(3, "x", "changed"), ...history.slice(3)]);
    expect(after[0]).not.toBe(before[0]);
    expect(after[0].kind === "segment" && after[0].seg.steps[0].kind === "tool" && after[0].seg.steps[0].result?.text).toBe("changed");
    expect(after[1]).toBe(before[1]);
  });

  it("sees a message joined from several blocks change in its later block", () => {
    // segments() appends each block's text to the message in place
    const two = (second: string) => [u(1, "first"), { ...u(1, second), id: "u1#1" }, a(2, "ok")];
    const before = run(undefined, two("second"));
    const after = run(before, two("other"));
    expect(after[0]).not.toBe(before[0]);
    expect(after[0].kind === "segment" && after[0].seg.user?.text).toBe("first\nother");
  });

  it("sees the start a wake gives work that has no message", () => {
    // turnLines sets seg.start from the wake in place
    const wake = (ts: string): Item => ({ id: "n1#0", seq: 1, role: "system", kind: "notice", text: "后台命令完成", wake: true, ts }); // i18n-ignore
    const items = (ts: string) => [wake(ts), tool(2, "x"), res(3, "x"), a(4, "done")];
    const before = run(undefined, items("2026-01-01T00:00:00Z"));
    const after = run(before, items("2026-01-01T00:05:00Z"));
    const seg = (ls: ChatLine[]) => ls.find((l) => l.kind === "segment") as Extract<ChatLine, { kind: "segment" }>;
    expect(seg(after)).not.toBe(seg(before));
    expect(seg(after).seg.start).toBe("2026-01-01T00:05:00Z");
  });

  it("keeps updating the live line while the agent works", () => {
    const working = [...history, u(7, "go"), tool(8, "y")];
    const before = run(undefined, working, true);
    const after = run(before, [...working, res(9, "y"), a(10, "half way")], true);
    const last = (ls: ChatLine[]) => ls[ls.length - 1] as Extract<ChatLine, { kind: "segment" }>;
    expect(last(after)).not.toBe(last(before));
    expect(last(after).live).toBe(true);
    expect(after[0]).toBe(before[0]);
    // the turn ends: the line is no longer live, so it is a new object again
    const done = run(after, [...working, res(9, "y"), a(10, "half way")], false);
    expect(last(done)).not.toBe(last(after));
    expect(last(done).kind).toBe("segment");
    expect(last(done).live).toBe(false);
  });
});

describe("sameValue", () => {
  it("compares Maps, Sets and nested objects by what they hold", () => {
    expect(sameValue(new Map([["a", [1]]]), new Map([["a", [1]]]))).toBe(true);
    expect(sameValue(new Map([["a", [1]]]), new Map([["a", [2]]]))).toBe(false);
    expect(sameValue(new Set(["a"]), new Set(["a"]))).toBe(true);
    expect(sameValue(new Set(["a"]), new Set(["b"]))).toBe(false);
    expect(sameValue({ a: { b: 1 } }, { a: { b: 1 } })).toBe(true);
    expect(sameValue({ a: 1 }, { a: 1, b: undefined })).toBe(false);
    expect(sameValue([1], { 0: 1 })).toBe(false);
  });

  it("keeps the task index while no task changed", () => {
    const prev = buildTaskIndex(history);
    expect(stableValue(prev, buildTaskIndex([...history, a(7, "more")]))).toBe(prev);
    const started = [...history, tool(7, "bg"), res(8, "bg", "Command running in background with ID: b1")];
    expect(stableValue(prev, buildTaskIndex(started))).not.toBe(prev);
  });
});
