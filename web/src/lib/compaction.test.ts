import { describe, expect, it } from "vitest";
import {
  archivedMainTitle, compactBody, compactionLabel, compactionView, compactPath, DONE_RECENT_MS, FAILED_RECENT_MS, foldCompactions, formatTokens,
  isCompaction, newMainBody, NEW_MAIN_PATH,
} from "./compaction";
import { chatLines } from "./turns";
import type { Compaction, Item } from "./types";

const u = (seq: number, text: string): Item => ({ id: `u${seq}#0`, seq, role: "user", kind: "text", text });
const a = (seq: number, text: string): Item => ({ id: `a${seq}#0`, seq, role: "assistant", kind: "text", text });
const tool = (seq: number, id: string): Item => ({ id: `t${seq}#0`, seq, role: "assistant", kind: "tool_use", tool_use_id: id, tool_name: "Bash", tool_input: { command: "ls" } });
const res = (seq: number, id: string): Item => ({ id: `r${seq}#0`, seq, role: "user", kind: "tool_result", tool_use_id: id, text: "ok" });
const notice = (seq: number, text: string, extra: Partial<Item> = {}): Item => ({ id: `n${seq}#0`, seq, role: "system", kind: "notice", text, ...extra });
const boundary = (seq: number, compact: Item["compact"] = {}) => notice(seq, "上下文已压缩", { compact });

describe("the requests", () => {
  it("compacts the conversation, with its machine when known", () => {
    expect(compactPath("s/1")).toBe("/assistant/conversations/s%2F1/compact");
    expect(compactBody()).toEqual({});
    expect(compactBody("m1")).toEqual({ machine_id: "m1" });
  });

  it("starts a new main conversation carrying the summary or nothing, expecting the current one", () => {
    expect(NEW_MAIN_PATH).toBe("/assistant/main/new");
    expect(newMainBody(true, "old")).toEqual({ carry: "summary", expect: "old" });
    expect(newMainBody(false, "old")).toEqual({ carry: "none", expect: "old" });
  });

  it("names the archived conversation by the day", () => {
    expect(archivedMainTitle(new Date(2026, 10, 4, 9))).toBe("旧主对话 · 至 11月4日");
  });
});

describe("compactionView", () => {
  const now = Date.parse("2026-11-04T10:00:00Z");
  const at = (ms: number) => new Date(now - ms).toISOString();
  const c = (state: Compaction["state"], finishedAgo?: number, error?: string): Compaction => ({
    state,
    started_at: at((finishedAgo ?? 0) + 30_000),
    finished_at: finishedAgo === undefined ? undefined : at(finishedAgo),
    error,
  });

  it("shows a running one, and nothing without one", () => {
    expect(compactionView(undefined, undefined, now)).toBeNull();
    expect(compactionView(c("running"), undefined, now)).toEqual({ kind: "running" });
  });

  it("says done when it saw it finish or it just finished, not for an old one", () => {
    expect(compactionView(c("done", 2 * DONE_RECENT_MS), "running", now)?.kind).toBe("done");
    expect(compactionView(c("done", 5_000), undefined, now)?.kind).toBe("done");
    expect(compactionView(c("done", 2 * DONE_RECENT_MS), undefined, now)).toBeNull();
    expect(compactionView(c("done", 2 * DONE_RECENT_MS), "done", now)).toBeNull();
  });

  it("keeps a recent failure until it is dismissed", () => {
    const f = c("failed", FAILED_RECENT_MS / 2, "上下文太短");
    const v = compactionView(f, undefined, now);
    expect(v).toMatchObject({ kind: "failed", error: "上下文太短" });
    expect(compactionView(f, undefined, now, v && v.kind === "failed" ? v.key : undefined)).toBeNull();
    expect(compactionView(c("failed", 2 * FAILED_RECENT_MS, "x"), undefined, now)).toBeNull();
    expect(compactionView(c("failed", 2 * FAILED_RECENT_MS, "x"), "running", now)?.kind).toBe("failed");
    expect(compactionView(c("failed", 0, ""), "running", now)).toMatchObject({ error: "未知错误" });
  });
});

describe("the divider", () => {
  it("labels what is known", () => {
    expect(compactionLabel({ trigger: "manual", pre_tokens: 181_000 })).toBe("以上内容已压缩 · 手动 · 压缩前 18.1 万 token");
    expect(compactionLabel({ trigger: "auto", pre_tokens: 150_000 })).toBe("以上内容已压缩 · 自动 · 压缩前 15 万 token");
    expect(compactionLabel({ trigger: "idle" })).toBe("以上内容已压缩 · 空闲时");
    expect(compactionLabel({ trigger: "weird" })).toBe("以上内容已压缩");
    expect(compactionLabel(undefined)).toBe("以上内容已压缩");
    expect(formatTokens(8500)).toBe("8,500");
  });

  it("folds the command, the boundary and the summary into one", () => {
    const items = [u(1, "hi"), a(2, "ok"), notice(3, "压缩上下文"), boundary(4, { trigger: "manual", pre_tokens: 181_000 }), notice(5, "已从压缩的上下文继续"), u(6, "next")];
    const out = foldCompactions(items);
    expect(out.map((i) => i.id)).toEqual(["u1#0", "a2#0", "n4#0", "u6#0"]);
    expect(out[2].compact).toEqual({ trigger: "manual", pre_tokens: 181_000, notes: ["压缩上下文", "上下文已压缩", "已从压缩的上下文继续"] });
  });

  it("marks an idle compaction, stands a lone summary in for its boundary, leaves a pending command alone", () => {
    const idle = foldCompactions([a(1, "ok"), notice(2, "上下文已压缩"), notice(3, "已从压缩的上下文继续"), notice(4, "Compacted while idle, before the cache expired")]);
    expect(idle).toHaveLength(2);
    expect(idle[1].compact?.trigger).toBe("idle");
    const alone = foldCompactions([notice(1, "已从压缩的上下文继续"), u(2, "go")]);
    expect(isCompaction(alone[0])).toBe(true);
    const pending = [u(1, "hi"), notice(2, "压缩上下文")];
    expect(foldCompactions(pending)).toBe(pending);
    // a command not followed by its boundary stays a notice
    const later = foldCompactions([notice(1, "压缩上下文"), u(2, "x"), boundary(3)]);
    expect(later.map((i) => i.id)).toEqual(["n1#0", "u2#0", "n3#0"]);
    expect(later[0].compact).toBeUndefined();
  });

  it("is a line of its own after the exchange", () => {
    const ls = chatLines([u(1, "hi"), tool(2, "x"), res(3, "x"), a(4, "done"), notice(5, "压缩上下文"), boundary(6, { trigger: "manual" }), notice(7, "已从压缩的上下文继续"), u(8, "next"), a(9, "ok")]);
    expect(ls.map((l) => l.kind)).toEqual(["segment", "compact", "segment"]);
    expect(ls[0].kind === "segment" && ls[0].seg.answer?.text).toBe("done");
    expect(ls[1].kind === "compact" && ls[1].item.id).toBe("n6#0");
  });

  it("takes in the command claude -p writes after the boundary (#1865)", () => {
    // The order a /compact sent to an idle claude -p left in a real transcript: boundary, summary, command, note.
    const ls = chatLines([u(1, "hi"), a(2, "done"), boundary(3, { trigger: "manual", pre_tokens: 89119 }), notice(4, "已从压缩的上下文继续"),
      notice(5, "压缩上下文"), notice(6, "Compacted")]);
    expect(ls.map((l) => l.kind)).toEqual(["segment", "compact"]);
    expect(ls[1].kind === "compact" && ls[1].item.compact?.notes).toEqual(["上下文已压缩", "已从压缩的上下文继续", "压缩上下文", "Compacted"]);
    expect(ls[1].kind === "compact" && compactionLabel(ls[1].item.compact)).toBe("以上内容已压缩 · 手动 · 压缩前 8.9 万 token");
  });

  it("stays a step of the work when compacted in the middle of it", () => {
    const ls = chatLines([u(1, "do"), tool(2, "x"), res(3, "x"), boundary(4, { trigger: "auto" }), notice(5, "已从压缩的上下文继续"), tool(6, "y"), res(7, "y"), a(8, "done")]);
    expect(ls.map((l) => l.kind)).toEqual(["segment"]);
    const seg = ls[0].kind === "segment" ? ls[0].seg : undefined;
    expect(seg?.steps.filter((s) => s.kind === "notice").map((s) => s.id)).toEqual(["n4#0"]);
    expect(seg?.answer?.text).toBe("done");
  });

  it("opens the chat when the page starts with it", () => {
    const ls = chatLines([notice(1, "已从压缩的上下文继续"), u(2, "go"), a(3, "ok")]);
    expect(ls.map((l) => l.kind)).toEqual(["compact", "segment"]);
  });
});
