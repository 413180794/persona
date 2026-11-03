import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getState, refresh, refreshTodos, reset, setSpacesSupported } from "./store";

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

  it("refresh() 页面加载/重连时若旧 hub 返回无 spaces 的待办列表，spacesSupported 判为 false (#881 review)", async () => {
    const legacyTodos = [
      { id: "todo-legacy", title: "旧版待办", created_at: "", updated_at: "" },
    ];
    const fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/todos")) {
        return Promise.resolve(new Response(JSON.stringify({ todos: legacyTodos }), { status: 200 }));
      }
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetch);

    await refresh();
    expect(getState().todosSupported).toBe(true);
    expect(getState().todosLoaded).toBe(true);
    expect(getState().spacesSupported).toBe(false);
  });

  it("当 GET /api/v1/todos 成功返回待办列表时，todosSupported=true 且存入 todos", async () => {
    const mockTodos = [
      { id: "todo-1", title: "测试待办", created_at: "2026-11-02T10:00:00Z", updated_at: "", spaces: [] },
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
    expect(st.spacesSupported).toBe(true);
    expect(st.todos).toHaveLength(1);
    expect(st.todos[0].id).toBe("todo-1");
  });

  it("refreshTodos 独立重拉待办，若 404 设 todosSupported=false 与 spacesSupported=false", async () => {
    const fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/todos")) {
        return Promise.resolve(new Response(JSON.stringify({ error: "not found" }), { status: 404 }));
      }
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetch);

    await refreshTodos();
    expect(getState().todosSupported).toBe(false);
    expect(getState().spacesSupported).toBe(false);
  });

  it("支持主动标记 spacesSupported=false（例如收到 /spaces 404 时）(#874 item 1)", () => {
    expect(getState().spacesSupported).toBe(true);
    setSpacesSupported(false);
    expect(getState().spacesSupported).toBe(false);
  });

  it("spacesSupported 被置成 false 后，通过 refreshTodos 重新计算恢复为 true (#881 review)", async () => {
    setSpacesSupported(false);
    expect(getState().spacesSupported).toBe(false);

    const fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/todos")) {
        return Promise.resolve(new Response(JSON.stringify({ todos: [] }), { status: 200 }));
      }
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetch);

    await refreshTodos();
    expect(getState().spacesSupported).toBe(true);
  });

  it("当现代 hub 返回空待办列表时，spacesSupported 保持原样为 true，且 todosLoaded 为 true (#881 review)", async () => {
    const fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/todos")) {
        return Promise.resolve(new Response(JSON.stringify({ todos: [] }), { status: 200 }));
      }
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetch);

    await refreshTodos();
    expect(getState().todosSupported).toBe(true);
    expect(getState().todosLoaded).toBe(true);
    expect(getState().spacesSupported).toBe(true);
  });

  it("当旧 hub 返回没有 spaces 属性的待办列表时，spacesSupported 判定为 false (#874 item 1)", async () => {
    const legacyTodos = [
      { id: "todo-legacy", title: "旧版待办", created_at: "", updated_at: "" },
    ];
    const fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/todos")) {
        return Promise.resolve(new Response(JSON.stringify({ todos: legacyTodos }), { status: 200 }));
      }
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetch);

    await refreshTodos();
    expect(getState().todosSupported).toBe(true);
    expect(getState().spacesSupported).toBe(false);
  });
});
