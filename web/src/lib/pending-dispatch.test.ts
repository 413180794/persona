import { describe, expect, it } from "vitest";
import { pendingDispatch, type FeedItem, type TaskPlan } from "./feed";
import type { Todo } from "./todos";

const at = "2026-11-10T08:00:00Z";
const todo: Todo = { id: "example-todo", title: "Choose an example date", created_at: at, updated_at: at };
const task = { title: "Example task", prompt: "Check the example", machine: "fixture-device", cwd: "/demo", todo_id: todo.id };
const plan: TaskPlan = { id: "example-plan", conversation: "example-chat", state: "started", tasks: [task] };
const planned = { ...todo, plan_id: plan.id };
const request: FeedItem = { id: "example-request", conversation: "example-chat", at, kind: "task", ref: todo.id };

describe("pending dispatch evidence", () => {
  it("does not infer a start from origin or supervision", () => {
    expect(pendingDispatch(todo, {})).toBe(false);
    expect(pendingDispatch({ ...todo, origin: "example-chat", supervision: { supervisor: "example-chat" } }, {})).toBe(false);
    expect(pendingDispatch(todo, {}, [{ ...request, kind: "review" }])).toBe(false);
  });

  it("keeps open and requested plans visible, including a plan that has not loaded yet", () => {
    expect(pendingDispatch(planned, {})).toBe(true);
    for (const state of ["open", "started"] as const) {
      expect(pendingDispatch(planned, { [plan.id]: { ...plan, state } })).toBe(true);
    }
    expect(pendingDispatch(todo, {}, [request])).toBe(true);
    expect(pendingDispatch(todo, {}, [{ ...request, ref: "another-todo" }])).toBe(false);
  });

  it("does not restart dismissed, failed, completed or previously attached work", () => {
    expect(pendingDispatch(planned, { [plan.id]: { ...plan, state: "dismissed" } })).toBe(false);
    expect(pendingDispatch(planned, { [plan.id]: { ...plan, tasks: [{ ...task, error: "Example failure" }] } })).toBe(false);
    expect(pendingDispatch({ ...planned, start_error: "Example failure" }, {})).toBe(false);
    expect(pendingDispatch({ ...planned, done_at: at }, {})).toBe(false);
    for (const released_at of [undefined, at]) {
      expect(pendingDispatch({ ...planned, agents: [{ machine_id: "fixture-device", session_id: "worker", linked_at: at, released_at }] }, {})).toBe(false);
    }
  });
});
