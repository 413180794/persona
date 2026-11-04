import { describe, expect, it } from "vitest";
import { chatLines, freshWords, isLoopTick, mergeItems, parseUserText, segments, openRecap, toolChip, toolLabel, wakeDrawer, workSegment, formatDuration } from "./turns";
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
    expect([s[0].tools, s[0].thoughts, s[0].replies]).toEqual([1, 0, 1]); // the counts the old work row read (workSummary is gone with it)
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

describe("freshWords", () => {
  it("is the agent's words in a push the chat did not have yet", () => {
    const have = [u(1, "hi"), a(2, "earlier")];
    const pushed = [a(2, "earlier"), tool(3, "x"), a(4, "new words"), u(5, "next")];
    expect(freshWords(have, pushed)).toEqual([pushed[2].id]);
    expect(freshWords(have, [])).toEqual([]);
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

  it("leaves only the request of a Feishu /agent prompt as the text", () => {
    const raw =
      '<jarvis-context kind="instructions" title="给 Agent 的说明">\n示例用户在飞书「发布群」里用 /agent 叫你。\n\n## 怎么做\n\n- 做完回复一次。\n</jarvis-context>\n' +
      '<jarvis-context kind="context" title="请求的位置">\n位置：群聊「发布群」\n</jarvis-context>\n' +
      '<jarvis-context kind="context" title="项目 · 发布">\n</jarvis-context>\n看下构建为什么挂了';
    const p = parseUserText(raw);
    expect(p.text).toBe("看下构建为什么挂了");
    expect(p.contexts.map((c) => [c.kind, c.title])).toEqual([["instructions", "给 Agent 的说明"], ["context", "请求的位置"], ["context", "项目 · 发布"]]);
    expect(p.contexts[2].text).toBe("");
  });
});

it("toolChip shows the first line of a command", () => {
  expect(toolChip(tool(1, "x"))).toBe("ls -la");
});

describe("chatLines", () => {
  const notice = (seq: number, text: string, extra: Partial<Item> = {}): Item => ({ id: `n${seq}#0`, seq, role: "system", kind: "notice", text, ...extra });
  const recap = (seq: number, text: string): Item => ({ id: `rc${seq}#0`, seq, role: "system", kind: "recap", text });
  const wake = (seq: number, title: string, status = "completed"): Item =>
    notice(seq, `后台命令完成 · ${title}`, { wake: true, task: { kind: "shell", status, title, task_id: `bk${seq}` } });
  const kinds = (ls: ReturnType<typeof chatLines>) => ls.map((l) => l.kind);

  it("draws one segment per message, the live one last", () => {
    const ls = chatLines([u(1, "hi"), a(2, "ok"), u(3, "next"), tool(4, "x")], { live: true });
    expect(kinds(ls)).toEqual(["segment", "segment"]);
    expect(ls.map((l) => l.kind === "segment" && l.live)).toEqual([false, true]);
  });

  it("merges a finished exchange of several messages into one, and splits it when asked", () => {
    const items = [u(1, "first"), tool(2, "x"), res(3, "x"), u(4, "also this", { queued: true }), a(5, "partial"), tool(6, "y"), res(7, "y"), a(8, "all done")];
    const ls = chatLines(items);
    expect(kinds(ls)).toEqual(["segment"]);
    const m = ls[0].kind === "segment" ? ls[0] : null;
    expect(m?.group).toBe("wt-u1#0");
    expect(m?.seg.user?.parts).toEqual(["first", "also this"]);
    expect(m?.seg.answer?.text).toBe("all done");
    expect(m?.seg.tools).toBe(2);
    expect(m?.seg.steps.map((s) => s.kind)).toEqual(["tool", "text", "tool"]);
    const open = chatLines(items, { split: new Set(["wt-u1#0"]) });
    expect(kinds(open)).toEqual(["split", "segment", "segment"]);
    expect(open[0].kind === "split" && open[0].count).toBe(2);
    // the live exchange is never merged
    expect(kinds(chatLines(items, { live: true }))).toEqual(["segment", "segment"]);
  });

  it("keeps an interrupted notice after the exchange", () => {
    const ls = chatLines([u(1, "go"), tool(2, "x"), notice(3, "已中断")]);
    expect(kinds(ls)).toEqual(["segment", "notice"]);
  });

  it("folds recaps in a row into one line", () => {
    const ls = chatLines([u(1, "go"), a(2, "done"), recap(3, "one"), recap(4, "two"), u(5, "back"), a(6, "ok"), recap(7, "three")]);
    expect(kinds(ls)).toEqual(["segment", "recaps", "segment", "recaps"]);
    expect(ls[1].kind === "recaps" && ls[1].items.map((i) => i.text)).toEqual(["one", "two"]);
    expect(ls[1].id).toBe("rc3#0");
  });

  it("opens the newest recap as the chat's last line, in the flow", () => {
    const one = chatLines([u(1, "go"), a(2, "done"), recap(3, "one")]);
    const last = one[one.length - 1];
    const shown = last.kind === "recaps" ? last.items[0] : undefined;
    expect(openRecap(one, shown)).toEqual({ line: last.id, earlier: [], newest: shown });
    expect(openRecap(one, undefined)).toBeUndefined();
    // folded with older ones: the newest opens, the rest stay a chip in the same line
    const two = chatLines([u(1, "go"), a(2, "done"), recap(3, "one"), recap(4, "two")]);
    const l2 = two[two.length - 1];
    const open = openRecap(two, l2.kind === "recaps" ? l2.items[1] : undefined);
    expect(open?.line).toBe(l2.id);
    expect(open?.earlier.map((i) => i.text)).toEqual(["one"]);
    expect(open?.newest.text).toBe("two");
    expect(openRecap(two, l2.kind === "recaps" ? l2.items[0] : undefined)).toBeUndefined();
    // a recap that is not the newest thing is not open
    const mid = chatLines([u(1, "go"), a(2, "done"), recap(3, "one"), u(4, "back"), a(5, "ok")]);
    expect(openRecap(mid, mid[1].kind === "recaps" ? mid[1].items[0] : undefined)).toBeUndefined();
  });

  it("keeps work a background task woke the agent for in the exchange, behind a wakes chip", () => {
    const items = [
      u(1, "run the tests in the background"),
      a(2, "started, I will tell you when it ends — this is the long one"),
      wake(3, "tests"),
      tool(4, "x"),
      res(5, "x"),
      a(6, "all green"),
    ];
    const ls = chatLines(items);
    expect(kinds(ls)).toEqual(["segment", "wakes"]);
    const seg = ls[0].kind === "segment" ? ls[0].seg : null;
    // the longest run's words are the answer; what came after it is only in the drawer
    expect(seg?.answer?.text).toContain("long one");
    const w = ls[1].kind === "wakes" ? ls[1].turn : null;
    expect(w?.wakes).toHaveLength(1);
    const d = wakeDrawer(w!);
    expect(d.groups.map((g) => g.answer?.text)).toEqual(["all green"]);
    expect(d.groups[0].notices.map((n) => n.id)).toEqual(["n3#0"]);
    expect(d.process.map((p) => p.kind)).toEqual(["tool_use", "tool_result"]);
  });

  it("puts the latest wake first in the drawer", () => {
    const items = [u(1, "go"), a(2, "ok"), wake(3, "a"), a(4, "a done"), wake(5, "b"), a(6, "b done")];
    const ls = chatLines(items);
    const w = ls.find((l) => l.kind === "wakes");
    const d = wakeDrawer(w!.kind === "wakes" ? w!.turn : (null as never));
    expect(d.groups.map((g) => g.answer?.text)).toEqual(["b done", "a done"]);
  });

  it("reopens the exchange a recap closed when a background task reports back", () => {
    const ls = chatLines([u(1, "go"), a(2, "started the build, waiting for it"), recap(3, "waiting for the build"), wake(4, "build"), a(5, "ok")]);
    expect(kinds(ls)).toEqual(["segment", "wakes", "recaps"]);
  });

  it("opens work a wake started on its own with the wake notice", () => {
    const ls = chatLines([wake(1, "build"), tool(2, "x"), res(3, "x"), a(4, "build is up")]);
    expect(kinds(ls)).toEqual(["notice", "segment"]);
    expect(ls[1].kind === "segment" && ls[1].seg.answer?.text).toBe("build is up");
  });

  it("turns API errors into cards and folds retries of the same one", () => {
    const err = "API Error: 502 upstream error, try again";
    const ls = chatLines([u(1, "plan the day"), a(2, err), u(3, "继续"), a(4, err), u(5, "继续"), a(6, err)]);
    expect(kinds(ls)).toEqual(["segment", "apiError"]);
    const e = ls[1].kind === "apiError" ? ls[1] : null;
    expect(e?.count).toBe(3);
    expect(e?.info.status).toBe(502);
    expect(e?.info.transient).toBe(true);
    // the message before it stays, without the error as its answer
    expect(ls[0].kind === "segment" && ls[0].seg.answer).toBeUndefined();
  });

  it("does not fold a different error, nor one after real work", () => {
    const ls = chatLines([u(1, "a"), a(2, "API Error: 502 bad gateway"), u(3, "继续"), a(4, "API Error: 401 authentication_error")]);
    expect(kinds(ls)).toEqual(["segment", "apiError", "segment", "apiError"]);
    const ls2 = chatLines([u(1, "a"), a(2, "API Error: 502 x"), u(3, "do something else"), tool(4, "x"), res(5, "x"), a(6, "API Error: 502 x")]);
    expect(kinds(ls2)).toEqual(["segment", "apiError", "segment", "apiError"]);
  });

  it("marks cross-session messages and loop ticks", () => {
    const ls = chatLines([u(1, "hello from the other side", { from: "build-bot" }), a(2, "ok"), u(3, "/loop check CI"), a(4, "CI is green")]);
    const segs = ls.flatMap((l) => (l.kind === "segment" ? [l.seg] : []));
    expect(segs[0].user?.from).toBe("build-bot");
    expect(isLoopTick(segs[1].user!)).toBe(true);
  });

  it("shows the work row while working with nothing of the turn yet", () => {
    const ls = chatLines([u(1, "go"), a(2, "done"), recap(3, "r")], { live: true });
    expect(kinds(ls)).toEqual(["segment", "recaps", "segment"]);
    expect(ls[2].kind === "segment" && ls[2].live).toBe(true);
  });
});

describe("workSegment", () => {
  it("keeps every step, words included, without an answer", () => {
    const s = workSegment("w", [tool(1, "x"), res(2, "x"), a(3, "between"), tool(4, "y"), a(5, "end")]);
    expect(s.answer).toBeUndefined();
    expect(s.steps.map((x) => x.kind)).toEqual(["tool", "text", "tool", "text"]);
    expect(s.replies).toBe(2);
  });
});

it("toolLabel names an MCP tool by its own name, whatever the server is called", () => {
  expect(toolLabel("mcp__github__create_issue")).toBe("create issue");
  expect(toolLabel("mcp__plugin_lark_docs__fetch_doc")).toBe("fetch doc");
  expect(toolLabel("mcp__x")).toBe("x");
  expect(toolLabel("Bash")).toBe("运行");
});

describe("jarvis-file tags", () => {
  it("become files on the message, not text", () => {
    const p = parseUserText('上次的录屏，你看下按钮那里\n\n<jarvis-file kind="video" path="/tmp/录屏 1.mov" name="录屏.mov" size="25270000" duration="6.0"/>\n<jarvis-file kind="file" path="/tmp/a&amp;b.pdf"/>');
    expect(p.text).toBe("上次的录屏，你看下按钮那里");
    expect(p.files).toEqual([
      { kind: "video", path: "/tmp/录屏 1.mov", name: "录屏.mov", size: 25270000, duration: 6 },
      { kind: "file", path: "/tmp/a&b.pdf", name: "a&b.pdf", size: undefined, duration: undefined },
    ]);
    expect(parseUserText('<jarvis-file kind="video" name="x"/>').files).toEqual([]); // no path: nothing to play
  });

  it("formats durations", () => {
    expect([formatDuration(6), formatDuration(65.4), formatDuration(3723)]).toEqual(["0:06", "1:05", "1:02:03"]);
  });
});

describe("paths in answers", () => {
  it("picks absolute paths to media and documents, not code or prose", async () => {
    const { replyPaths, mediaKindOf } = await import("./turns");
    expect(
      replyPaths("视频已生成：`/Users/me/code/weather/media/preview.mp4`。报告在 ~/Desktop/季度 报告.pdf 和 (/tmp/a.png)，代码改了 /Users/me/x/main.swift，网址 https://example.com/x.pdf。"),
    ).toEqual(["/Users/me/code/weather/media/preview.mp4", "/tmp/a.png"]);
    expect(replyPaths("见 ~/notes/plan.docx, 以及 /data/clip.MOV")).toEqual(["~/notes/plan.docx", "/data/clip.MOV"]);
    expect([mediaKindOf("/a/b.MOV"), mediaKindOf("x.m4a"), mediaKindOf("p.heic"), mediaKindOf("r.pdf")]).toEqual(["video", "audio", "image", "file"]);
  });
});
