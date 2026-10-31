import { describe, expect, it } from "vitest";
import { mergeItems, parseUserText, segments, toolChip, workSummary } from "./turns";
import type { Item } from "./types";

const u = (seq: number, text: string, extra: Partial<Item> = {}): Item => ({ id: `u${seq}#0`, seq, role: "user", kind: "text", text, ...extra });
const a = (seq: number, text: string): Item => ({ id: `a${seq}#0`, seq, role: "assistant", kind: "text", text });
const tool = (seq: number, id: string): Item => ({ id: `t${seq}#0`, seq, role: "assistant", kind: "tool_use", tool_use_id: id, tool_name: "Bash", tool_input: { command: "ls -la\npwd" } });
const res = (seq: number, id: string): Item => ({ id: `r${seq}#0`, seq, role: "user", kind: "tool_result", tool_use_id: id, text: "ok" });

describe("segments", () => {
  it("splits at user messages and takes the last words as the answer", () => {
    const s = segments([u(1, "hi"), a(2, "looking"), tool(3, "x"), res(4, "x"), a(5, "done"), u(6, "next"), a(7, "sure")]);
    expect(s).toHaveLength(2);
    expect(s[0].user?.text).toBe("hi");
    expect(s[0].answer?.text).toBe("done");
    expect(s[0].steps.map((x) => x.kind)).toEqual(["text", "tool"]);
    expect(s[0].tools).toBe(1);
    expect(s[0].replies).toBe(1);
    expect(workSummary(s[0])).toBe("工具 1 · 回复 1");
    const st = s[0].steps[1];
    expect(st.kind === "tool" && st.result?.text).toBe("ok");
    expect(s[1].answer?.text).toBe("sure");
  });

  it("starts with a segment without a message when the page begins mid-turn", () => {
    const s = segments([tool(1, "x"), res(2, "x"), a(3, "end")]);
    expect(s[0].user).toBeUndefined();
    expect(s[0].answer?.text).toBe("end");
  });

  it("keeps the blocks of one line (text + image) as one message", () => {
    const img: Item = { id: "u1#1", seq: 1, role: "user", kind: "image", image: { mime: "image/png", lazy: true } };
    const s = segments([u(1, "look [Image #1]"), img, a(2, "nice")]);
    expect(s).toHaveLength(1);
    expect(s[0].user?.text).toBe("look");
    expect(s[0].user?.images).toHaveLength(1);
  });

  it("counts folded work", () => {
    const f: Item = { id: "fold:t2#0", seq: 2, role: "assistant", kind: "fold", fold: { items: 10, tools: 5, thoughts: 2, through_seq: 9 } };
    const s = segments([u(1, "go"), f, a(10, "ok")]);
    expect(s[0].tools).toBe(5);
    expect(s[0].thoughts).toBe(2);
    expect(s[0].folds).toHaveLength(1);
  });
});

describe("mergeItems", () => {
  it("drops a fold once the steps of its range arrive", () => {
    const f: Item = { id: "fold:t3#0", seq: 3, role: "assistant", kind: "fold", fold: { items: 2, tools: 1, thoughts: 0, through_seq: 4 } };
    const have = mergeItems([], [u(1, "go"), f, a(5, "ok")]);
    expect(have.map((i) => i.id)).toEqual(["u1#0", "fold:t3#0", "a5#0"]);
    const next = mergeItems(have, [tool(3, "x"), res(4, "x")]);
    expect(next.map((i) => i.id)).toEqual(["u1#0", "t3#0", "r4#0", "a5#0"]);
  });
});

describe("parseUserText", () => {
  it("drops the attached_images block and image markers", () => {
    expect(parseUserText("[Image #1] 像这样合并。\n<attached_images>\n/tmp/a.jpg\n</attached_images>").text).toBe("像这样合并。");
    expect(parseUserText("像这样合并。\n<attached_images>\n</attached_images>").text).toBe("像这样合并。");
  });

  it("pulls out jarvis-context tags", () => {
    const p = parseUserText('<jarvis-context kind="edit" title="Doc &amp; co" url="https://x">sel</jarvis-context>fix it');
    expect(p.text).toBe("fix it");
    expect(p.contexts).toEqual([{ kind: "edit", title: "Doc & co", url: "https://x", text: "sel" }]);
  });
});

it("toolChip shows the first line of a command", () => {
  expect(toolChip(tool(1, "x"))).toBe("ls -la");
});
