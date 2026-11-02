import { afterEach, describe, expect, it, vi } from "vitest";
import { applyDraft, onDraftEvent, replaceNeedsConfirm, sendDraft, withAppended, withQuote } from "./drafts";
import { browseTarget, byPrefix, chatFiles, displayTitle, mentionAt, nameMatches, otherSessions, relPath, replaceToken, sessionQuote } from "./mentions";
import { keepAllBut, sameMessage } from "./queue";
import { boxRect, burnWidth, fitRect, keepMark, toImage, toScreen } from "./image-edit";
import type { Item, Session } from "./types";

afterEach(() => vi.unstubAllGlobals());

describe("drafts", () => {
  it("quotes every line in front of the draft (ChatView.swift bindMenu)", () => {
    expect(withQuote("", "第一行\n第二行")).toBe("> 第一行\n> 第二行\n\n");
    expect(withQuote("我的看法", "原话")).toBe("> 原话\n\n我的看法");
  });

  it("puts a pulled-back queued message under the draft", () => {
    expect(withAppended("", "排队的")).toBe("排队的");
    expect(withAppended("先写的", "排队的")).toBe("先写的\n排队的");
  });

  it("applies text events and leaves the text alone for an image", () => {
    expect(applyDraft("x", { type: "replace", text: "旧消息" })).toBe("旧消息");
    expect(applyDraft("x", { type: "append", text: "y" })).toBe("x\ny");
    expect(applyDraft("x", { type: "image", image: { name: "a.png", mime: "image/png", data: "" } })).toBe("x");
  });

  it("asks before replacing only a different draft", () => {
    expect(replaceNeedsConfirm("", "a")).toBe(false);
    expect(replaceNeedsConfirm("  a ", "a")).toBe(false);
    expect(replaceNeedsConfirm("b", "a")).toBe(true);
  });

  it("hands events to the mounted composer, else keeps text in the stored draft", () => {
    const store = new Map<string, string>([["draft:m/s", "typed"]]);
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => store.set(k, v),
      removeItem: (k: string) => store.delete(k),
    });
    expect(sendDraft("m/s", { type: "quote", text: "q" })).toBe(true);
    expect(store.get("draft:m/s")).toBe("> q\n\ntyped");
    expect(sendDraft("m/s", { type: "image", image: { name: "a.png", mime: "image/png", data: "AA" } })).toBe(false);

    const got: string[] = [];
    const off = onDraftEvent("m/s", (e) => got.push(e.type));
    expect(sendDraft("m/s", { type: "replace", text: "r" })).toBe(true);
    expect(got).toEqual(["replace"]);
    expect(store.get("draft:m/s")).toBe("> q\n\ntyped"); // the composer has it, not the store
    off();
    sendDraft("m/s", { type: "replace", text: "" });
    expect(store.has("draft:m/s")).toBe(false);
  });
});

describe("@ mentions", () => {
  it("finds the token before the caret, not inside an e-mail address", () => {
    expect(mentionAt("@", 1)).toEqual({ start: 0, end: 1, query: "" });
    expect(mentionAt("看看 @src/ma", 10)).toEqual({ start: 3, end: 10, query: "src/ma" });
    expect(mentionAt("me@example.com", 14)).toBeNull();
    expect(mentionAt("@a b", 4)).toBeNull();
    // the caret in the middle: only what is before it counts
    expect(mentionAt("x @ab y", 4)).toEqual({ start: 2, end: 4, query: "a" });
  });

  it("replaces the token and does not double the space after a picked file", () => {
    const t = mentionAt("看 @Rea 呢", 6)!;
    expect(replaceToken("看 @Rea 呢", t, "@README.md ")).toEqual({ text: "看 @README.md 呢", caret: 13 });
    const d = mentionAt("@sr", 3)!;
    expect(replaceToken("@sr", d, "@src/")).toEqual({ text: "@src/", caret: 5 });
  });

  it("lists the files the chat's tools touched, newest first and once each", () => {
    const tool = (seq: number, input: Record<string, unknown>): Item => ({ id: `t${seq}`, seq, role: "assistant", kind: "tool_use", tool_input: input });
    const items: Item[] = [
      tool(1, { file_path: "/w/a.ts" }),
      tool(2, { command: "ls" }),
      tool(3, { notebook_path: "/w/n.ipynb" }),
      tool(4, { file_path: "/w/a.ts" }),
      { id: "x", seq: 5, role: "assistant", kind: "text", text: "/w/b.ts" },
    ];
    expect(chatFiles(items)).toEqual(["/w/a.ts", "/w/n.ipynb"]);
    expect(chatFiles(items, 1)).toEqual(["/w/a.ts"]);
    expect(relPath("/w/src/a.ts", "/w")).toBe("src/a.ts");
    expect(relPath("/other/a.ts", "/w/")).toBe("/other/a.ts");
  });

  it("browses a folder once the query has a slash", () => {
    expect(browseTarget("src", "/w")).toBeNull();
    expect(browseTarget("src/ma", "/w/")).toEqual({ dir: "/w/src", typed: "src/", filter: "ma" });
    expect(browseTarget("/etc/ho", "/w")).toEqual({ dir: "/etc", typed: "/etc/", filter: "ho" });
    expect(browseTarget("/", "/w")).toEqual({ dir: "/", typed: "/", filter: "" });
    expect(browseTarget("~/", undefined)).toEqual({ dir: "~", typed: "~/", filter: "" });
    expect(browseTarget("src/", undefined)).toBeNull();
  });

  it("matches names anywhere, hidden ones only when asked, prefix matches first", () => {
    expect(nameMatches("README.md", "read")).toBe(true);
    expect(nameMatches(".env", "env")).toBe(false);
    expect(nameMatches(".env", ".e")).toBe(true);
    expect(byPrefix(["xmain", "main", "amain"], (s) => s, "ma")).toEqual(["main", "xmain", "amain"]);
  });

  const sess = (id: string, extra: Partial<Session> = {}): Session => ({ machine_id: "m", session_id: id, title: `t-${id}`, cwd: `/code/${id}`, status: "idle", ...extra });

  it("offers other sessions, newest first, not assistants or this one", () => {
    const all = [
      sess("a", { updated_at: "2026-11-02T10:00:00Z" }),
      sess("b", { updated_at: "2026-11-02T12:00:00Z", agent: "codex" }),
      sess("c", { assistant: true, updated_at: "2026-11-02T13:00:00Z" }),
      sess("self", { updated_at: "2026-11-02T14:00:00Z" }),
    ];
    expect(otherSessions(all, "m/self", "").map((s) => s.session_id)).toEqual(["b", "a"]);
    expect(otherSessions(all, "m/self", "codex").map((s) => s.session_id)).toEqual(["b"]);
    expect(displayTitle({ title: "Claude Code", cwd: "/x/weather" })).toBe("weather");
  });

  it("quotes a session with its folder and last words", () => {
    const q = sessionQuote(sess("a", { title: "修登录", cwd: "/code/app", last_text: "  改好了  " }));
    expect(q.url).toBe("jarvis://session/m/a");
    expect(q.title).toBe("修登录");
    expect(q.text).toBe("另一个会话：修登录（Claude）\n目录：/code/app\n它最近说：改好了");
  });
});

describe("queue", () => {
  it("re-queues every other message in order", () => {
    expect(keepAllBut(3, 1)).toEqual([0, 2]);
    expect(keepAllBut(1, 0)).toEqual([]);
  });

  it("matches a sent message to its queued copy, short ones only exactly", () => {
    expect(sameMessage("好", "好的，先这样")).toBe(false);
    expect(sameMessage(" 好 ", "好")).toBe(true);
    expect(sameMessage("把测试也跑一遍再提交", "把测试也跑一遍再提交 [Image #1]")).toBe(true);
    expect(sameMessage("", "")).toBe(false);
  });
});

describe("image markup geometry", () => {
  const img = { width: 400, height: 200 };
  it("fits the image in the box and maps points both ways", () => {
    const fit = fitRect(img, { width: 200, height: 200 });
    expect(fit).toEqual({ x: 0, y: 50, width: 200, height: 100 });
    expect(toImage({ x: 100, y: 100 }, fit, img)).toEqual({ x: 200, y: 100 });
    expect(toImage({ x: -10, y: 500 }, fit, img)).toEqual({ x: 0, y: 200 }); // held inside the image
    expect(toScreen({ x: 200, y: 100 }, fit, img)).toEqual({ x: 100, y: 100 });
    expect(fitRect({ width: 0, height: 10 }, { width: 10, height: 10 }).width).toBe(0);
  });

  it("keeps boxes dragged either way and drops taps", () => {
    expect(boxRect({ x: 10, y: 20 }, { x: 4, y: 2 })).toEqual({ x: 4, y: 2, width: 6, height: 18 });
    expect(keepMark({ tool: "box", color: "red", points: [{ x: 1, y: 1 }] })).toBe(false);
    expect(keepMark({ tool: "box", color: "red", points: [{ x: 1, y: 1 }, { x: 1.5, y: 1 }] })).toBe(false);
    expect(keepMark({ tool: "pen", color: "red", points: [{ x: 1, y: 1 }, { x: 1, y: 2 }] })).toBe(true);
    expect(burnWidth({ width: 2200, height: 1000 })).toBe(10);
    expect(burnWidth({ width: 48, height: 32 })).toBe(2);
  });
});
