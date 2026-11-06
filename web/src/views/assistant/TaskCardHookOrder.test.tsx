// @vitest-environment jsdom
// A task card drawn before its todo is in the store (the todo list not read back yet, a task just sent, a reconnect's
// re-read) draws nothing, then draws the task once it arrives: both renders call the same hooks, so React does not
// throw "Rendered more hooks than during the previous render" (#310) and blank the page (#5422).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { refresh, refreshTodos, reset } from "../../lib/store";
import { TaskCard } from "./Tasks";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const now = new Date().toISOString();
const TODO = { id: "t1", title: "My task", origin: "c1", created_at: now, updated_at: now, agents: [{ machine_id: "m1", session_id: "s1", cwd: "/code/app" }] };
const SESSIONS = [{ machine_id: "m1", session_id: "s1", state: "working", status: "working", title: "a", last_text: "running the tests", updated_at: now }];
const MACHINES = [{ id: "m1", name: "MacBook", online: true }];

let todos: (typeof TODO)[] = [];
let host: HTMLDivElement;
let root: Root;
let errors: unknown[];

beforeEach(async () => {
  todos = [];
  errors = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      const u = String(url);
      const body = u.includes("/todos") ? { todos } : u.includes("/sessions") ? { sessions: SESSIONS } : u.includes("/machines") ? { machines: MACHINES } : {};
      return Promise.resolve(new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } }));
    }),
  );
  await refresh();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host, { onUncaughtError: (e) => errors.push(e), onCaughtError: (e) => errors.push(e) });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  vi.unstubAllGlobals();
});

describe("a task card drawn before its todo arrives", () => {
  it("draws the task once the todo list comes back", async () => {
    act(() => root.render(<TaskCard todoId="t1" conversation="c1" />));
    expect(host.textContent).toBe("");
    todos = [TODO];
    await act(async () => {
      await refreshTodos();
    });
    expect(errors).toEqual([]);
    expect(host.textContent).toContain("My task");
  });

  it("draws nothing again, without throwing, when its todo goes away", async () => {
    todos = [TODO];
    await act(async () => {
      await refreshTodos();
    });
    act(() => root.render(<TaskCard todoId="t1" conversation="c1" />));
    expect(host.textContent).toContain("My task");
    todos = [];
    await act(async () => {
      await refreshTodos();
    });
    expect(errors).toEqual([]);
    expect(host.textContent).toBe("");
  });
});
