import { describe, expect, it } from "vitest";
import { commentBlock, parseCommentBlock } from "./comments";
import { checkCounts, mergeReady, threadQuote, type GitPR } from "./git-pr";
import { t } from "../i18n";

const pr = (over: Partial<GitPR> = {}): GitPR => ({
  number: 12,
  url: "https://github.com/example/repo/pull/12",
  title: "Handle the empty password",
  state: "OPEN",
  base: "main",
  head: "fix/login",
  merge_state: "CLEAN",
  checks: [],
  threads: [],
  ...over,
});

describe("git-pr", () => {
  it("counts checks by state", () => {
    expect(checkCounts([{ name: "a", state: "pass" }, { name: "b", state: "fail" }, { name: "c", state: "pass" }])).toEqual({ pass: 2, fail: 1, pending: 0, skipped: 0 });
  });

  it("says when it can be merged", () => {
    expect(mergeReady(pr())).toBe("now");
    expect(mergeReady(pr({ merge_state: "BLOCKED" }))).toBe("later");
    expect(mergeReady(pr({ mergeable: "CONFLICTING", merge_state: "DIRTY" }))).toBe("conflict");
    expect(mergeReady(pr({ draft: true }))).toBe("no");
    expect(mergeReady(pr({ state: "MERGED" }))).toBe("no");
  });

  it("makes a review thread the comment block a picked-words comment makes", () => {
    const th = { path: "src/login.ts", line: 42, comments: [{ author: "reviewer", body: "Check for an empty string here.", at: "2026-11-07T00:00:00Z" }, { author: "me", body: "Will do", at: "2026-11-07T01:00:00Z" }] };
    const q = threadQuote(pr(), th);
    expect(q.text).toBe("@reviewer: Check for an empty string here.\n\n@me: Will do");
    expect(q.comment?.body).toBe(t("session-gitpr.thread_note", { number: 12, where: "src/login.ts:42" }));
    const block = commentBlock(q, 1);
    expect(block.split("\n")[0]).toBe("Comment 1");
    expect(block).toContain("> @reviewer: Check for an empty string here.");
    expect(parseCommentBlock(block)).toEqual({ text: q.text, comment: { body: q.comment!.body } });
  });

  it("says when the code under a thread changed since", () => {
    const q = threadQuote(pr(), { path: "a.ts", outdated: true, comments: [{ author: "r", body: "x", at: "" }] });
    expect(q.comment?.body).toBe(t("session-gitpr.thread_note_outdated", { number: 12, where: "a.ts" }));
  });
});
