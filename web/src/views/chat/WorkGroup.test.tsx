import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Item, Subagent } from "../../lib/types";
import type { Step } from "../../lib/turns";
import type { ToolStep, WorkGroupNode } from "../../lib/work-groups";
import { ActionRow, DIFF_FOLD_LINES, diffStats, editDiffs, foldDiffs, lineDiff, presentAction, truncateParam } from "./ActionRow";
import { ChatContext } from "./chat-context";
import { emptyTaskIndex } from "../../lib/tasks";
import { PlanCard } from "./PlanCard";
import { Answer, bubbleLines, SegmentView, UserBubble } from "./SegmentView";
import { SubagentCard } from "./SubagentCard";
import { ThinkingCard } from "./ThinkingCard";
import { WorkGroup, type WorkProps } from "./WorkGroup";

const T0 = Date.parse("2026-11-04T10:00:00Z");
const at = (s: number) => new Date(T0 + s * 1000).toISOString();
const use = (id: string, name: string, input: Record<string, unknown>, ts = 0): Item => ({ id: `${id}#0`, seq: Number(id.replace(/\D/g, "")) || 1, role: "assistant", kind: "tool_use", tool_name: name, tool_use_id: `c-${id}`, tool_input: input, ts: at(ts) });
const res = (id: string, text = "ok", ts = 1): Item => ({ id: `${id}-r#0`, seq: 1, role: "user", kind: "tool_result", tool_use_id: `c-${id}`, text, ts: at(ts) });
const tool = (id: string, name: string, input: Record<string, unknown>, done = true): ToolStep => ({ kind: "tool", id: `${id}#0`, use: use(id, name, input), result: done ? res(id) : undefined, images: [] });
const props: WorkProps = { mid: "m", sid: "s", scope: "t:", expandFold: async () => {}, agentStatus: () => "completed", thinking: new Map() };
const group = (children: Step[], live: boolean, durationMs?: number): WorkGroupNode => ({ kind: "group", id: `work-${live}`, children: children.map((step) => ({ kind: "step", step })), live, durationMs, startedAt: T0 });

describe("WorkGroup", () => {
  it("its header, a subagent card's and a plan's are a finger tall on a touch screen (#1996)", () => {
    const coarse = "[@media(pointer:coarse)]:min-h-10";
    const head = renderToStaticMarkup(<WorkGroup g={group([tool("t1", "Grep", { pattern: "x" })], false, 3000)} p={props} />);
    // the margin gives back what the height adds, so the group does not move
    expect(head).toContain(`[@media(pointer:coarse)]:-my-[7.5px] ${coarse}`);
    expect(renderToStaticMarkup(<SubagentCard step={tool("a1", "Task", { description: "查", prompt: "p" })} status="completed" mid="m" />)).toContain(coarse);
    expect(renderToStaticMarkup(<PlanCard id="p" todos={[{ content: "读", status: "pending" }]} />)).toContain(coarse);
  });

  it("while live: a spinner, 正在工作… and a clock, its newest steps on a rail", () => {
    const html = renderToStaticMarkup(<WorkGroup g={group([tool("t1", "Bash", { command: "ls", description: "看目录" }, false)], true)} p={props} />);
    expect(html).toContain('data-work-group="live"');
    expect(html).toContain(">正在工作…<");
    expect(html).toMatch(/font-mono text-\[12px\][^>]*>[\ddhms ]+</);
    expect(html).toContain("data-live-preview");
    expect(html).toContain("border-l-2 border-tool-rail");
    expect(html).toContain(">看目录<");
    expect(html).not.toContain("data-work-detail"); // a lazy Collapse: nothing until it opens
  });

  it("done: layers and 已工作 Xs, folded", () => {
    const html = renderToStaticMarkup(<WorkGroup g={group([tool("t1", "Bash", { command: "ls" })], false, 65_000)} p={props} />);
    expect(html).toContain(">已工作 1m 5s<");
    expect(html).toContain('aria-expanded="false"');
    expect(html).not.toContain("data-live-preview");
  });

  it("a live group with five steps or fewer does not open: its preview is all of it", () => {
    const five = [1, 2, 3, 4, 5].map((n) => tool(`t${n}`, "Bash", { command: `echo ${n}` }));
    expect(renderToStaticMarkup(<WorkGroup g={group(five, true)} p={props} />)).toMatch(/<button[^>]*disabled=""/);
    const six = [...five, tool("t6", "Bash", { command: "echo 6" })];
    const html = renderToStaticMarkup(<WorkGroup g={group(six, true)} p={props} />);
    expect(html).not.toMatch(/^<div[^>]*><button[^>]*disabled/);
    expect(html).not.toContain("echo 1"); // only the newest five peek out
    expect(html).toContain("echo 6");
  });
});

describe("ActionRow", () => {
  it("a command with a description is that sentence, no verb; the command is its title", () => {
    const p = presentAction(use("t1", "Bash", { command: "npm test", description: "跑单测" }));
    expect(p).toEqual({ param: { text: "跑单测", title: "npm test" } });
    const html = renderToStaticMarkup(<ActionRow step={tool("t1", "Bash", { command: "npm test", description: "跑单测" })} mid="m" sid="s" />);
    expect(html).toContain("py-[3px]");
    expect(html).toContain("rounded-[8px]");
    expect(html).not.toContain("运行命令");
  });

  it("one without: 运行命令 and the command on a quieter line under it", () => {
    const html = renderToStaticMarkup(<ActionRow step={tool("t1", "Bash", { command: "git status --short" })} mid="m" sid="s" />);
    expect(html).toContain(">运行命令<");
    expect(html).toMatch(/pl-\[30px\][^>]*>git status --short</);
  });

  it("a file is a chip with its name only; long parameters stop at 60 characters", () => {
    const html = renderToStaticMarkup(<ActionRow step={tool("t1", "Read", { file_path: "/Users/me/code/app/src/views/Home.tsx" })} mid="m" sid="s" />);
    expect(html).toContain(">读取<");
    expect(html).toContain("data-file-chip");
    expect(html).toContain(">Home.tsx<");
    expect(html).not.toContain(">/Users/me/code/app/src/views/Home.tsx<");
    expect(truncateParam("x".repeat(80))).toHaveLength(60);
    expect(truncateParam("x".repeat(80)).endsWith("...")).toBe(true);
    expect(presentAction(use("g", "Grep", { pattern: "y".repeat(100) })).param!.text).toHaveLength(60);
  });

  it("verbs follow cindy's table", () => {
    const verb = (name: string, input: Record<string, unknown> = {}) => presentAction(use("v", name, input)).verb;
    expect([verb("Edit", { file_path: "/a" }), verb("Write", { file_path: "/a" }), verb("Grep", { pattern: "x" }), verb("WebFetch", { url: "u" }), verb("mcp__github__create_issue")]).toEqual(["编辑", "创建", "搜索", "访问", "调用"]);
    expect(presentAction(use("v", "mcp__github__create_issue", {})).param!.text).toBe("github · create issue");
    expect(presentAction(use("v", "mcp__jarvis__agent_read", {})).param).toEqual({ text: "Persona · agent read", title: "mcp__jarvis__agent_read" });
  });

  it("an edit shows its +N −N from a line diff; a running call a spinner in its 16 px slot", () => {
    const edit = tool("e1", "Edit", { file_path: "/a.ts", old_string: "a\nb\nc", new_string: "a\nB\nc\nd" });
    expect(diffStats(editDiffs(edit.use)!)).toEqual({ add: 2, del: 1 });
    const html = renderToStaticMarkup(<ActionRow step={edit} mid="m" sid="s" />);
    expect(html).toContain(">+2<");
    expect(html).toContain(">−1<");
    expect(renderToStaticMarkup(<ActionRow step={tool("r", "Bash", { command: "sleep 9" }, false)} running mid="m" sid="s" />)).toMatch(/h-\[18px\] w-4[^>]*><span[^>]*animation:spin/);
  });

  it("lineDiff keeps what did not change and pairs what did", () => {
    const sign = (l: { kind: string; text: string }) => ({ same: " ", del: "-", add: "+" })[l.kind] + l.text;
    expect(lineDiff("a\nb\nc", "a\nx\nc").map(sign)).toEqual([" a", "-b", "+x", " c"]);
    expect(lineDiff("", "n1\nn2").map((l) => l.kind)).toEqual(["add", "add"]);
    expect(lineDiff("o", "").map((l) => l.kind)).toEqual(["del"]);
  });

  it("a long diff shows its first 40 lines on a phone, cut inside the change they end in (#1996)", () => {
    const lines = (n: number) => lineDiff("", Array.from({ length: n }, (_, i) => `l${i}`).join("\n"));
    const short = [lines(10), lines(30)];
    expect(foldDiffs(short)).toEqual({ shown: short, hidden: 0 });
    const { shown, hidden } = foldDiffs([lines(25), lines(25), lines(5)]);
    expect(shown.map((d) => d.length)).toEqual([25, 15]);
    expect(hidden).toBe(15);
    expect(DIFF_FOLD_LINES).toBe(40);
  });

  it("is a finger tall on a touch screen, in the chat's secondary size (#1996)", () => {
    const html = renderToStaticMarkup(<ActionRow step={tool("t1", "Grep", { pattern: "x" })} mid="m" sid="s" />);
    expect(html).toContain("[@media(pointer:coarse)]:min-h-10");
    expect(html).toContain("text-[length:var(--chat-secondary)]");
  });
});

describe("ThinkingCard", () => {
  it("an empty thought only says how long it took and does not open", () => {
    const html = renderToStaticMarkup(<ThinkingCard item={{ id: "th#0", seq: 1, role: "assistant", kind: "thinking", text: "" }} durationMs={3000} />);
    expect(html).toContain(">思考 3s<");
    expect(html).toContain('disabled=""');
    expect(html).not.toContain("aria-expanded");
  });

  it("one with words opens to them, 14 px italic on a rail", () => {
    const html = renderToStaticMarkup(<ThinkingCard item={{ id: "th#1", seq: 1, role: "assistant", kind: "thinking", text: "先看测试" }} durationMs={12_000} />);
    expect(html).toContain(">思考 12s<");
    expect(html).toContain('aria-expanded="false"');
  });

  it("live: Thinking with three dots while the status line says it thinks, nothing otherwise", () => {
    const html = renderToStaticMarkup(<ThinkingCard live={{ thinking: true }} />);
    expect(html).toContain(">Thinking<");
    expect(html.match(/size-1 rounded-full/g)).toHaveLength(3);
    expect(renderToStaticMarkup(<ThinkingCard live={{ thinking: false }} />)).toBe("");
    expect(renderToStaticMarkup(<ThinkingCard live={null} />)).toBe("");
  });
});

describe("SubagentCard", () => {
  const sub: Subagent = { id: "sa1", session_id: "s@sa1", type: "general-purpose", tool_use_id: "c-a1", running: true };
  const card = (status: "running" | "completed" | "failed") =>
    renderToStaticMarkup(
      <ChatContext.Provider value={{ tasks: emptyTaskIndex(), subagents: [sub, { id: "sa2", session_id: "s@sa2", parent_id: "sa1" }], scope: "" }}>
        <SubagentCard step={tool("a1", "Agent", { description: "回归：结算", prompt: "跑结算回归", subagent_type: "general-purpose" })} status={status} mid="m" />
      </ChatContext.Provider>,
    );

  it("says what it is and how it is going, folded", () => {
    const html = card("running");
    expect(html).toContain('data-subagent-card="running"');
    expect(html).toContain(">回归：结算<");
    expect(html).toContain(">运行中");
    expect(html).toContain(">general-purpose<");
    expect(html).toContain('aria-expanded="false"');
    expect(html).not.toContain("查看完整过程");
    expect(card("failed")).toContain(">失败");
  });
});

describe("PlanCard", () => {
  it("open by default: done / total · the current step, rows at least 30 px", () => {
    const html = renderToStaticMarkup(
      <PlanCard id="p" todos={[{ content: "读代码", status: "completed" }, { content: "改实现", status: "in_progress" }, { content: "跑测试", status: "pending" }]} />,
    );
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain(">1/3<");
    expect(html).toContain(">改实现<");
    expect(html.match(/min-h-\[30px\]/g)).toHaveLength(3);
    expect(html).not.toContain("data-plan-progress"); // the bar is for the folded card
  });
});

describe("the user's bubble and the answer", () => {
  const msg = (text: string) => ({ id: "u#0", text, images: [], contexts: [] });

  it("on the right, at most 488 wide, 12 px corners", () => {
    const html = renderToStaticMarkup(<UserBubble msg={msg("你好")} mid="m" sid="s" />);
    expect(html).toContain("ml-auto");
    expect(html).toContain("max-w-[488px]");
    expect(html).toContain("rounded-[12px]");
    expect(html).not.toContain("展开");
    expect(html).not.toContain("data-fold-scroll");
  });

  it("past 14 lines it folds to 10 that scroll inside, faded at the bottom, with 展开", () => {
    const long = Array.from({ length: 20 }, (_, i) => `第 ${i} 行`).join("\n");
    expect(bubbleLines(long)).toBe(20);
    const html = renderToStaticMarkup(<UserBubble msg={msg(long)} mid="m" sid="s" />);
    expect(html).toContain("max-h-[10lh] overflow-y-auto");
    expect(html).toContain("data-fold-scroll");
    expect(html).toContain("mask-image:linear-gradient(180deg");
    expect(html).not.toContain("line-clamp");
    expect(html).not.toContain("overscroll");
    // the fade is on the words, not the bubble behind them
    expect(html).toMatch(/bg-user-bubble[^>]*>\s*<div[^>]*data-fold-scroll/);
    expect(html).toContain(">展开<");
    expect(bubbleLines("x".repeat(31))).toBe(2);
  });

  it("past 14 lines it folds while it is still being sent too, dimmed (#1996)", () => {
    const long = Array.from({ length: 20 }, (_, i) => `第 ${i} 行`).join("\n");
    const html = renderToStaticMarkup(<UserBubble msg={msg(long)} mid="m" sid="s" pending />);
    expect(html).toContain("max-h-[10lh] overflow-y-auto");
    expect(html).toContain(">展开<");
    expect(html).toContain("opacity-60");
    expect(html).toContain(">发送中…<");
  });

  it("leaves 12% on its left like a merged bubble, so it never runs the whole width (#1996)", () => {
    expect(renderToStaticMarkup(<UserBubble msg={msg("你好")} mid="m" sid="s" />)).toContain("pl-[12%]");
  });

  it("a streaming answer has the message menu too, at the chat's body size (#1996)", () => {
    const html = renderToStaticMarkup(<Answer streaming="写到一半" mid="m" sid="s" />);
    expect(html).toContain("group/msg");
    expect(html).toContain('data-answer="streaming"');
    expect(html).toContain("[&amp;_.prose-j]:text-[length:var(--chat-body)]!");
  });

  it("an exchange: the answer without a bubble at 15 / 1.6, the work before it folded", () => {
    const user: Item = { id: "u1#0", seq: 1, role: "user", kind: "text", text: "看一下", ts: at(0) };
    const seg = { id: "s", user: { id: "u1#0", text: "看一下", images: [], contexts: [], ts: at(0) }, steps: [tool("t2", "Read", { file_path: "/a.ts" })], answer: { id: "a3#0", seq: 3, role: "assistant", kind: "text", text: "看完了。", ts: at(9) } as Item, tools: 1, thoughts: 0, replies: 0, folds: [], start: user.ts };
    const html = renderToStaticMarkup(<SegmentView seg={seg} live={false} mid="m" sid="s" expandFold={async () => {}} />);
    expect(html).toContain("[&amp;_.prose-j]:text-[length:var(--chat-body)]!");
    expect(html).toContain(">已工作 9s · 读取 1 个文件<");
    expect(html.indexOf("已工作")).toBeLessThan(html.indexOf("看完了。"));
  });
});
