import { describe, expect, it } from "vitest";
import { assistantEntries } from "./assistant-entries";
import { boardSections } from "./task-board";
import { triage, triageTodos, waitingForMerge } from "./todo-triage";
import { makeSessionLookup, todoStatus, type Todo } from "./todos";
import type { Session } from "./types";

const now = Date.parse("2026-11-16T12:00:00Z");
const at = "2026-11-16T11:00:00Z";
const task = (id: string, patch: Partial<Todo> = {}): Todo => ({
  id, title: `Example ${id}`, origin: "main", created_at: at, updated_at: at, ...patch,
});
const hold = (until = "2026-11-17T12:00:00Z") => ({
  supervisor: "main", state: "parked" as const,
  parked: { reason: "Waiting for the supplier's reply", waiting_on: "someone_else" as const, until, count: 1 },
});
const url = "https://github.com/example/demo/pull/42";
const facts = { url, state: "open" as const, draft: false, checked_at: at, review: "required" as const, checks: "success" as const, conflicts: false };
// The forge snapshot must be fresh, unlike the task's creation time.
facts.checked_at = "2026-11-16T11:59:00Z";
const worker: Session = { machine_id: "demo", session_id: "worker", title: "Example worker", cwd: "/demo", state: "working", status: "working", updated_at: at };
const lookup = makeSessionLookup([worker]);
const active = [{ machine_id: "demo", session_id: "worker", linked_at: at }];
const until = "2026-11-16T16:05:00+08:00";
const dateHold = {
  supervisor: "main", state: "parked" as const,
  parked: { waiting_on: "date" as const, until, count: 1, reason: "Check the deployment window",
    evidence: { kind: "date" as const, target: "Other sessions on the computer", wake: "The window opens" } },
};

describe("Parked grouping", () => {
  it("keeps a timed recheck working across unread turns, independent jobs and a task's own running review", () => {
    const unread = { ...worker, session_id: "unread", state: "unread" as const, status: "idle" };
    const shell = { ...unread, session_id: "shell", state: "idle" as const, waiting: true, background_tasks_count: 1 };
    const sessions = [unread, worker, shell];
    const bound = (s: Session) => ({ machine_id: s.machine_id, session_id: s.session_id, linked_at: at });
    const list = [
      task("mixed", { agents: [bound(unread), bound(worker)], supervision: dateHold, pr_url: url, pr_status: facts }),
      task("job", { agents: [bound(shell)], supervision: dateHold }),
      task("reviewer", { supervision: { ...dateHold, auto_review: { id: "run-1", state: "running", session_id: "review-session" } } }),
      task("gate", { agents: [bound(unread)], supervision: dateHold }),
    ];
    const lookup = makeSessionLookup(sessions);
    expect(todoStatus(list[0], lookup)).toBe("unread");
    expect(waitingForMerge(list[0], lookup, now)).toBe(false);
    for (const td of list.slice(0, 3)) expect(triage(td, lookup, now)).toMatchObject({ section: "working", nextCheck: until });
    expect(triageTodos(list, lookup, now)).toMatchObject({ open: 4, todoCounts: { working: 3, parked: 1 } });
    const entries = assistantEntries({ conversation: "main", main: "main", todos: list, sessions, interactions: [], plans: {}, feed: [], now });
    expect(entries.running.map((row) => row.key).sort()).toEqual(["task:job", "task:mixed", "task:reviewer"]);
    expect(entries.parked.map((row) => row.key)).toEqual(["task:gate"]);
    expect(triage(list[1], makeSessionLookup([{ ...shell, background_tasks_count: 0 }]), now).section).toBe("parked");
    for (const review of [
      { id: "run-1", state: "running" as const },
      { id: "run-1", state: "clean" as const, session_id: "review-session", ended_at: at },
    ]) expect(triage(task("gate", { supervision: { ...dateHold, auto_review: review } }), lookup, now).section).toBe("parked");
  });

  it("keeps external gates parked when the only work is unrelated, released, offline, or just waiting", () => {
    for (const [agents, session] of [
      [[], worker],
      [[{ ...active[0], released_at: at }], worker],
      [[{ ...active[0], start_error: "Could not start" }], worker],
      [active, { ...worker, todo: { id: "elsewhere", title: "Another task" } }],
      [active, { ...worker, state: "offline", background_tasks_count: 2 }],
      [active, { ...worker, state: "unread", status: "idle" }],
      [active, { ...worker, state: "idle", status: "idle", children: { total: 1, working: 1 } }],
      [active, { ...worker, state: "idle", status: "idle", waiting: true, background_working: true, background_tasks_count: 0, background: [{ id: "old", kind: "shell" }] }],
      [active, { ...worker, state: "idle", status: "idle", work: { phase: "blocked", on: "external" } }],
      [active, { ...worker, background_working: true, background_waiting: true, background_tasks_count: 1 }],
    ] satisfies [Todo["agents"], Session][]) {
      const td = task("gate", { agents, supervision: { ...dateHold, progress: { kind: "progress", at, text: "Background build running" } } });
      expect(triage(td, makeSessionLookup([session]), now)).toMatchObject({ section: "parked", rule: "parked" });
    }
  });

  it("keeps owner questions, failed starts and closure ahead of an active recheck", () => {
    const td = task("check", { agents: active, supervision: dateHold });
    expect(triage({ ...td, ask: { outcome: "decide", verdict: "Choose", asked_at: at } }, lookup, now).rule).toBe("owner_decides");
    expect(triage(td, makeSessionLookup([{ ...worker, state: "needs_you" }]), now).rule).toBe("agent_asks");
    expect(triage({ ...td, start_error: "Unavailable" }, lookup, now).rule).toBe("failed_start");
    expect(triage({ ...td, done_at: at }, lookup, now).rule).toBe("closed");
  });

  it("puts an elapsed fallback in Parked until the hub changes the actual state", () => {
    const overdue = task("supplier", { supervision: hold("2026-11-15T12:00:00Z"), updated_at: "2026-11-10T12:00:00Z" });
    expect(triage(overdue, lookup, now)).toMatchObject({ section: "parked", rule: "parked" });
    const page = triageTodos([overdue], lookup, now);
    expect(page.todoCounts.parked).toBe(1);
    expect([page.open, page.shelved, page.finished]).toEqual([1, 0, 0]);
  });

  it("shares one wait group and open count across the tray, To-dos and All board, even with multiple PRs", () => {
    const list = [
      task("supplier", { supervision: hold() }),
      task("review", { pr_url: url, pr_status: facts, other_prs: [{ ...facts, url: "https://github.com/example/demo/pull/41" }] }),
      task("active", { agents: active }),
      task("decision", { supervision: { supervisor: "main", state: "needs_user" } }),
    ];
    expect(triage(list[1], lookup, now)).toMatchObject({ section: "parked", rule: "pr_open" });
    const page = triageTodos(list, lookup, now);
    expect(page.todoCounts.parked).toBe(2);
    const entries = assistantEntries({ conversation: "main", main: "main", todos: list, sessions: [worker], interactions: [], plans: {}, feed: [], now });
    expect(entries).toHaveProperty("parked");
    expect(entries.parked.map((item) => item.key).sort()).toEqual(["task:review", "task:supplier"]);
    expect(entries.running.map((item) => item.key)).toEqual(["task:active"]);
    expect(entries.needsYou.map((item) => item.key)).toEqual(["task:decision"]);
    const counted = [...entries.needsYou, ...entries.running, ...entries.parked, ...entries.review].filter((item) => item.kind === "task" && item.countsOpen);
    expect(counted).toHaveLength(page.open);
    expect(new Set(counted.map((item) => item.key)).size).toBe(page.open);
    const board = boardSections(list.map((todo) => ({ todo, status: todoStatus(todo, lookup), parked: triage(todo, lookup, now).section === "parked" })), []);
    expect(board.find((section) => section.key === "parked")?.rows.map(({ todo }) => todo.id).sort()).toEqual(["review", "supplier"]);
  });

  it.each([
    ["live work", { agents: active }],
    ["owner decision", { supervision: { supervisor: "main", state: "needs_user" as const } }],
    ["closed", { done_at: at }],
    ["accepted", { supervision: { supervisor: "main", state: "accepted" as const, reviews: [{ at, verdict: "accepted" as const }] } }],
    ["deployed", { delivery: { state: "live" as const, at } }],
    ["draft", { pr_status: { ...facts, draft: true } }],
    ["stale facts", { pr_status: { ...facts, checked_at: "2026-11-16T11:00:00Z" } }],
    ["wrong PR", { pr_status: { ...facts, url: "https://github.com/example/other/pull/42" } }],
    ["supervisor review", { supervision: { supervisor: "main", state: "pending_review" as const } }],
    ["a seen supervisor review", { seen_at: "2026-11-16T12:00:00Z", supervision: { supervisor: "main", state: "pending_review" as const } }],
  ] satisfies [string, Partial<Todo>][])("does not let a ready PR hide %s", (_label, patch) => {
    const todo = task("review", { pr_url: url, pr_status: facts, ...patch });
    expect(triage(todo, lookup, now).section).not.toBe("parked");
    const entries = assistantEntries({ conversation: "main", main: "main", todos: [todo], sessions: [worker], interactions: [], plans: {}, feed: [], now });
    expect(entries).toHaveProperty("parked");
    expect(entries.parked).toEqual([]);
  });

  it("lets real work and owner decisions outrank an explicit hold", () => {
    const live = task("active", { supervision: hold(), agents: active });
    const decided = task("decision", { supervision: hold(), ask: { outcome: "decide", asked_at: at, verdict: "Choose a supplier", recommend: "Use the available supplier" } });
    expect(triage(live, lookup, now).section).toBe("working");
    expect(triage(decided, lookup, now).section).toBe("waiting");
    const entries = assistantEntries({ conversation: "main", main: "main", todos: [live, decided], sessions: [worker], interactions: [], plans: {}, feed: [], now });
    expect(entries).toHaveProperty("parked");
    expect(entries.parked).toEqual([]);
    expect(entries.running.map((item) => item.key)).toEqual(["task:active"]);
    expect(entries.needsYou.map((item) => item.key)).toEqual(["task:decision"]);
  });

  it("keeps a seen supervisor review open without calling it an external wait", () => {
    const todo = task("review", { pr_url: url, pr_status: facts, seen_at: "2026-11-16T12:00:00Z",
      supervision: { supervisor: "main", state: "pending_review" } });
    expect(triage(todo, lookup, now)).toMatchObject({ section: "other", rule: "rest" });
    expect(triageTodos([todo], lookup, now).open).toBe(1);
    const entries = assistantEntries({ conversation: "main", main: "main", todos: [todo], sessions: [worker], interactions: [], plans: {}, feed: [], now });
    expect(entries.parked).toEqual([]);
    expect(entries.review).toMatchObject([{ key: "task:review", countsOpen: true }]);
  });
});
