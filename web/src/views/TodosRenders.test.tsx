// @vitest-environment jsdom
// A sessions push re-renders only the todo rows whose agents it changed: the rows are memoized on a view of plain
// values, and the lists, groups and callbacks they get stay the same objects while nothing they show moved.
// ReviewBadge renders once per row render, so counting it counts the rows. The data is made up.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Todo } from "../lib/todos";
import type { Session } from "../lib/types";

vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  })) as typeof window.matchMedia;
});

vi.mock("../lib/api", async (orig) => ({
  ...(await orig<typeof import("../lib/api")>()),
  api: async () => ({}),
}));

const renders = vi.hoisted(() => new Map<string, number>());
vi.mock("./assistant/Supervision", async (orig) => {
  const real = await orig<typeof import("./assistant/Supervision")>();
  return {
    ...real,
    ReviewBadge: (props: Parameters<typeof real.ReviewBadge>[0]) => {
      renders.set(props.todo.id, (renders.get(props.todo.id) ?? 0) + 1);
      return real.ReviewBadge(props);
    },
  };
});

const { getState, upsertSession } = await import("../lib/store");
const { TodosView } = await import("./Todos");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const now = new Date().toISOString();
const todo = (id: string, title: string, sid: string): Todo =>
  ({ id, title, created_at: now, updated_at: now, agents: [{ machine_id: "m1", session_id: sid }] }) as Todo;
const session = (sid: string, o: Partial<Session> = {}) =>
  ({ machine_id: "m1", session_id: sid, title: sid, cwd: "/work/app", status: "running", state: "working", updated_at: now, ...o }) as Session;

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };

let host: HTMLDivElement;
let root: Root;

beforeEach(async () => {
  seed({
    todos: [todo("ta", "整理发布清单", "s1"), todo("tb", "修登录页", "s2")],
    todosLoaded: true,
    sessions: { m1: [session("s1", { last_text: "在读代码" }), session("s2", { last_text: "在跑测试" })] },
    interactions: [],
    answered: {},
  });
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  // ta stays selected: without a selection the first row is, and a reorder would move it (a real change)
  await act(async () => root.render(<TodosView selectedId="ta" />));
  renders.clear();
});

afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  seed(before);
});

describe("todo rows on a sessions push", () => {
  it("re-render only the row whose agent changed", async () => {
    await act(async () => upsertSession(session("s1", { last_text: "改完了，在提交" })));
    expect(renders.get("ta")).toBe(1);
    expect(renders.get("tb")).toBeUndefined();
    expect(host.textContent).toContain("改完了，在提交");
  });

  it("re-render none when the push leaves what they show alone", async () => {
    await act(async () => upsertSession(session("s2", { last_text: "在跑测试", updated_at: new Date(Date.now() + 1000).toISOString() })));
    expect(renders.size).toBe(0);
  });

  it("re-render the row whose agent starts waiting on you, and it moves first", async () => {
    await act(async () => upsertSession(session("s2", { state: "needs_you", status: "blocked" })));
    expect(renders.get("tb")).toBe(1);
    expect(renders.get("ta")).toBeUndefined();
    const titles = [...host.querySelectorAll("[role=button] .truncate.font-semibold")].map((el) => el.textContent);
    expect(titles).toEqual(["修登录页", "整理发布清单"]);
  });
});
