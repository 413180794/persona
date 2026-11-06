// @vitest-environment jsdom
// Deleting a todo closes the dialog and drops the row once the hub answers the DELETE, without waiting for the page's
// refresh; a second click while the DELETE is out sends nothing. The search ignores spaces at the ends. The hub is
// faked; the data made up.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Todo } from "../lib/todos";

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

const api = vi.fn(async (_path: string, _init?: unknown): Promise<unknown> => ({}));
vi.mock("../lib/api", async (orig) => ({
  ...(await orig<typeof import("../lib/api")>()),
  api: (path: string, init?: unknown) => api(path, init),
}));

const { getState, refreshTodos } = await import("../lib/store");
const { TodosView } = await import("./Todos");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const button = (label: string, scope: ParentNode = document) =>
  [...scope.querySelectorAll<HTMLElement>("button")].find((b) => b.textContent?.trim() === label || b.getAttribute("aria-label") === label);
const click = async (el: Element | undefined | null) => {
  expect(el, "element").toBeTruthy();
  await act(async () => (el as HTMLElement).click());
  await flush();
};
const dialog = () => document.querySelector<HTMLElement>("[role=dialog]");
const listText = () => document.querySelector<HTMLElement>("[data-testid=todos-list-column]")?.textContent ?? "";

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

const iso = (agoMs: number) => new Date(Date.now() - agoMs).toISOString();
const todo = (id: string, title: string): Todo => ({ id, title, created_at: iso(7_200_000), updated_at: iso(3_600_000) });

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };

beforeEach(() => {
  api.mockReset();
  api.mockImplementation(async (path: string) => (path.endsWith("/launchers") ? { launchers: [] } : {}));
  seed({ todos: [todo("t1", "Release checklist"), todo("t2", "deploy web")], todosLoaded: true, machines: [], sessions: {} });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  document.body.innerHTML = "";
  seed(before);
  vi.restoreAllMocks();
});

const openDelete = async () => {
  await act(async () => root.render(<TodosView selectedId="t1" />));
  await flush();
  await click(button("更多选项"));
  await click(document.querySelector("[role=menuitem]"));
  expect(dialog()?.textContent).toContain("Release checklist");
};
const deletes = () => api.mock.calls.filter(([, init]) => (init as { method?: string } | undefined)?.method === "DELETE");

describe("deleting a todo", () => {
  it("closes the dialog and drops the row once the DELETE is answered, while the refresh is still out", async () => {
    const del = deferred<unknown>();
    const stale = deferred<unknown>(); // a /todos asked for before the delete
    const after = deferred<unknown>(); // the ones asked for after it: the hub is slow, they do not come back here
    let deleted = false;
    api.mockImplementation(async (path: string, init?: unknown) => {
      if ((init as { method?: string } | undefined)?.method === "DELETE") {
        deleted = true;
        return del.promise;
      }
      if (path === "/todos") return deleted ? after.promise : stale.promise;
      return path.endsWith("/launchers") ? { launchers: [] } : {};
    });
    await openDelete();
    void refreshTodos();
    await click(button("删除", dialog()!));
    expect(dialog()).toBeTruthy();

    await act(async () => del.resolve({}));
    await flush();
    expect(dialog()).toBeNull();
    expect(listText()).not.toContain("Release checklist");
    expect(getState().todos.map((t) => t.id)).toEqual(["t2"]);

    // the slow answer to a /todos asked for before the delete does not put the todo back
    await act(async () => stale.resolve({ todos: [todo("t1", "Release checklist"), todo("t2", "deploy web")] }));
    await flush();
    expect(getState().todos.map((t) => t.id)).toEqual(["t2"]);
  });

  it("sends one DELETE when Delete is clicked again while the first is out", async () => {
    const del = deferred<unknown>();
    api.mockImplementation(async (path: string, init?: unknown) => {
      if ((init as { method?: string } | undefined)?.method === "DELETE") return del.promise;
      return path.endsWith("/launchers") ? { launchers: [] } : {};
    });
    await openDelete();
    const confirm = button("删除", dialog()!)!;
    await click(confirm);
    expect(confirm.hasAttribute("disabled")).toBe(true);
    await act(async () => confirm.click());
    await flush();
    expect(deletes()).toHaveLength(1);

    await act(async () => del.resolve({}));
    await flush();
    expect(deletes()).toHaveLength(1);
  });

  it("keeps the dialog open and the row when the DELETE fails", async () => {
    api.mockImplementation(async (path: string, init?: unknown) => {
      if ((init as { method?: string } | undefined)?.method === "DELETE") throw new Error("hub down");
      return path.endsWith("/launchers") ? { launchers: [] } : {};
    });
    await openDelete();
    const confirm = button("删除", dialog()!)!;
    await click(confirm);
    expect(dialog()).toBeTruthy();
    expect(confirm.hasAttribute("disabled")).toBe(false);
    expect(getState().todos.map((t) => t.id)).toEqual(["t1", "t2"]);
  });
});

describe("todo search", () => {
  it("ignores spaces at the ends of the query", async () => {
    await act(async () => root.render(<TodosView />));
    await flush();
    const input = document.querySelector<HTMLInputElement>("[data-testid=todos-list-column] input")!;
    const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
    await act(async () => {
      set.call(input, "  deploy ");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await flush();
    expect(listText()).toContain("deploy web");
    expect(listText()).not.toContain("Release checklist");
  });
});
