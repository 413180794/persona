import { describe, expect, it } from "vitest";
import { boardSectionOf, boardSections } from "./task-board";
import type { Session } from "./types";
import type { Todo, TodoStatus } from "./todos";

const todo = (id: string, created_at: string): Todo => ({ id, title: id, created_at, updated_at: created_at });
const session = (sid: string): Session => ({ machine_id: "m", session_id: sid, title: sid, cwd: "/w", status: "idle", state: "idle" }) as Session;

describe("the task board (iOS TaskBoardSheet)", () => {
  it("puts each status in its section, 断了 with 等你", () => {
    const want: Record<TodoStatus, string> = { needs_you: "waiting", interrupted: "waiting", working: "working", not_started: "working", unread: "unread", follow_up: "stopped", done: "done", failed: "failed" };
    for (const [st, key] of Object.entries(want)) expect(boardSectionOf(st as TodoStatus)).toBe(key);
  });

  it("orders the sections, keeps only the ones with something, newest task first, agents by their state", () => {
    const rows = [
      { todo: todo("old", "2026-11-01T00:00:00Z"), status: "working" as const },
      { todo: todo("new", "2026-11-03T00:00:00Z"), status: "not_started" as const },
      { todo: todo("bad", "2026-11-02T00:00:00Z"), status: "failed" as const },
    ];
    const sections = boardSections(rows, [{ session: session("a"), state: "needs_you" }, { session: session("b"), state: "working" }]);
    expect(sections.map((s) => s.key)).toEqual(["waiting", "working", "failed"]);
    expect(sections[0].agents.map((a) => a.session.session_id)).toEqual(["a"]);
    expect(sections[1].rows.map((r) => r.todo.id)).toEqual(["new", "old"]);
    expect(sections[1].agents.map((a) => a.session.session_id)).toEqual(["b"]);
  });

  it("has no sections when the conversation has nothing", () => {
    expect(boardSections([], [])).toEqual([]);
  });
});
