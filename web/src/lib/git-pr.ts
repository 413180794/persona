// The current branch's pull request in the Git panel (#6121, docs/protocol/protocol.md "git/pr"): what it is, its checks, its
// open review threads, opening one and merging it, all through gh on that computer as it is signed in. A review thread
// goes to the agent as a comment in the composer's quote list (lib/comments.ts), the same block a comment on picked
// words makes, so it is sent with the next message and nothing new is invented for it.
import { api, enc } from "./api";
import { clampComment } from "./comments";
import type { DraftQuote } from "./drafts";
import { t } from "../i18n";

export interface GitCheck {
  name: string;
  state: "pass" | "fail" | "pending" | "skipped";
  url?: string;
}

export interface GitPRComment {
  author: string;
  body: string;
  url?: string;
  at: string;
}

export interface GitPRThread {
  path: string;
  line?: number;
  outdated?: boolean;
  comments: GitPRComment[];
}

export interface GitPR {
  number: number;
  url: string;
  title: string;
  state: "OPEN" | "CLOSED" | "MERGED";
  draft?: boolean;
  base: string;
  head: string;
  mergeable?: string;
  merge_state?: string;
  review?: string;
  auto_merge?: boolean;
  checks: GitCheck[];
  checks_state?: "passing" | "failing" | "pending";
  threads: GitPRThread[];
}

/** What every PR call answers: the branch and its pull request (null: it has none). */
export interface GitPRAnswer {
  branch: string;
  pr: GitPR | null;
}

export type MergeMethod = "squash" | "merge" | "rebase";

export function gitPR(base: string, cwd: string, signal?: AbortSignal): Promise<GitPRAnswer> {
  return api<GitPRAnswer>(`${base}/pr?cwd=${enc(cwd)}`, { signal });
}

export function gitPRCreate(base: string, body: { cwd: string; title: string; body: string; base?: string; draft?: boolean }): Promise<GitPRAnswer> {
  return api<GitPRAnswer>(`${base}/pr`, { method: "POST", json: body });
}

export function gitPRMerge(base: string, body: { cwd: string; number: number; method?: MergeMethod; auto?: boolean; auto_off?: boolean }): Promise<GitPRAnswer> {
  return api<GitPRAnswer>(`${base}/pr/merge`, { method: "POST", json: body });
}

/** How many checks are in each state. */
export function checkCounts(checks: readonly GitCheck[]): Record<GitCheck["state"], number> {
  const n = { pass: 0, fail: 0, pending: 0, skipped: 0 };
  for (const c of checks) n[c.state] = (n[c.state] ?? 0) + 1;
  return n;
}

/** Whether it can be merged now (as gh says), only once its checks and reviews allow, or not at all. */
export function mergeReady(pr: GitPR): "now" | "later" | "conflict" | "no" {
  if (pr.state !== "OPEN" || pr.draft) return "no";
  if (pr.mergeable === "CONFLICTING" || pr.merge_state === "DIRTY") return "conflict";
  if (pr.merge_state === "CLEAN" || pr.merge_state === "UNSTABLE" || pr.merge_state === "HAS_HOOKS") return "now";
  return "later";
}

/** `src/a.ts:42`, or the path alone for a thread on the whole file. */
export function threadWhere(th: GitPRThread): string {
  return th.line ? `${th.path}:${th.line}` : th.path;
}

/**
 * A review thread as a comment for the agent: the reviewers' words quoted (each comment `@author: …`), and a note in
 * the user's words saying which pull request and where, which they can edit before sending.
 */
export function threadQuote(pr: GitPR, th: GitPRThread): DraftQuote {
  const words = th.comments.map((c) => `@${c.author}: ${c.body.trim()}`).join("\n\n");
  const note = t(th.outdated ? "session-gitpr.thread_note_outdated" : "session-gitpr.thread_note", { number: pr.number, where: threadWhere(th) });
  return { text: clampComment(words), comment: { body: note } };
}
