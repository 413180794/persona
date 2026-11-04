import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { dropAnswered, endInteraction, getState, hubHas, larkOn, refresh, refreshTodos, reset, setSpacesSupported } from "./store";

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

describe("hub features", () => {
  it("answers `unknown` until the hub names any", () => {
    expect(hubHas([], "lark", true)).toBe(true);
    expect(hubHas([], "connectors")).toBe(false);
    expect(hubHas(["terminal_stream"], "lark", true)).toBe(false);
    expect(hubHas(["terminal_stream", "lark"], "lark")).toBe(true);
  });

  it("has Feishu through lark-connector only with lark, as before while the hub has not said", () => {
    expect(larkOn({ features: [] })).toBe(true);
    expect(larkOn({ features: ["plugins", "lark_attachments", "lark_native_encryption"] })).toBe(false);
    expect(larkOn({ features: ["plugins", "lark", "lark_moments"] })).toBe(true);
  });
});

describe("待回答的卡片：刷新不复活已结束的卡片 (#1673)", () => {
  const ix = { id: "ix_q", machine_id: "m", session_id: "s", kind: "question" as const, questions: [{ question: "发吗？", options: [{ label: "发" }] }] };

  beforeEach(() => {
    reset();
    vi.stubGlobal("localStorage", { getItem: () => "mock-token", setItem: () => {}, removeItem: () => {} });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
    reset();
  });

  /** /interactions answers at once with `pending`; /sessions waits for release() (a busy hub takes seconds). */
  function slowSessions(pending: unknown[]) {
    let release = () => {};
    const held = new Promise<void>((r) => (release = r));
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (url: string) => {
      if (url.includes("/interactions")) return new Response(JSON.stringify({ interactions: pending }), { status: 200 });
      if (url.includes("/sessions")) await held;
      return new Response(JSON.stringify({}), { status: 200 });
    }));
    return () => release();
  }

  it("刷新途中回答的卡片，刷新回来后不再出现", async () => {
    let release = slowSessions([ix]);
    await (async () => {
      const p = refresh();
      release();
      await p;
    })();
    expect(getState().interactions.map((i) => i.id)).toEqual(["ix_q"]);

    release = slowSessions([ix]); // the snapshot still has it: asked for before the answer
    const p = refresh();
    await new Promise((r) => setTimeout(r, 0));
    endInteraction("ix_q", { ix, resolution: { answers: { "发吗？": "发" } } });
    expect(getState().interactions).toEqual([]);
    release();
    await p;
    expect(getState().interactions).toEqual([]);
    expect(getState().answered.ix_q?.resolution).toEqual({ answers: { "发吗？": "发" } });

    // a snapshot asked for after the answer is taken as it is
    release = slowSessions([]);
    const q = refresh();
    release();
    await q;
    expect(getState().interactions).toEqual([]);
  });

  it("已回答的卡片过一会儿收起，也可以手动收起", () => {
    vi.useFakeTimers();
    endInteraction("ix_q", { ix, resolution: { answers: { "发吗？": "发" } } });
    expect(Object.keys(getState().answered)).toEqual(["ix_q"]);
    vi.advanceTimersByTime(25_000);
    expect(getState().answered).toEqual({});

    endInteraction("ix_q", { ix, resolution: {} });
    dropAnswered("ix_q");
    expect(getState().answered).toEqual({});
  });

  it("没有答案（交回电脑、已过期）只收起卡片", () => {
    endInteraction("ix_q");
    expect(getState().answered).toEqual({});
  });
});
