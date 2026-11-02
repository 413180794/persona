import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getState, refresh, refreshTodos, reset } from "./store";

describe("待办 store 与 404 兼容 (C2)", () => {
  beforeEach(() => {
    reset();
    vi.stubGlobal("localStorage", { getItem: () => "mock-token", setItem: () => {}, removeItem: () => {} });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    reset();
  });

  it("当 GET /api/v1/todos 返回 404 时，设 todosSupported=false，不报错", async () => {
    const fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/todos")) {
        return Promise.resolve(new Response(JSON.stringify({ error: "not found" }), { status: 404 }));
      }
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetch);

    await expect(refresh()).resolves.not.toThrow();
    const st = getState();
    expect(st.todosSupported).toBe(false);
    expect(st.todosLoaded).toBe(true);
    expect(st.todos).toEqual([]);
  });

  it("当 GET /api/v1/todos 成功返回待办列表时，todosSupported=true 且存入 todos", async () => {
    const mockTodos = [
      { id: "todo-1", title: "测试待办", created_at: "2026-11-02T10:00:00Z", updated_at: "" },
    ];
    const fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/todos")) {
        return Promise.resolve(new Response(JSON.stringify({ todos: mockTodos }), { status: 200 }));
      }
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetch);

    await refresh();
    const st = getState();
    expect(st.todosSupported).toBe(true);
    expect(st.todosLoaded).toBe(true);
    expect(st.todos).toHaveLength(1);
    expect(st.todos[0].id).toBe("todo-1");
  });

  it("refreshTodos 独立重拉待办，若 404 设 todosSupported=false", async () => {
    const fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/todos")) {
        return Promise.resolve(new Response(JSON.stringify({ error: "not found" }), { status: 404 }));
      }
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetch);

    await refreshTodos();
    expect(getState().todosSupported).toBe(false);
  });
});
