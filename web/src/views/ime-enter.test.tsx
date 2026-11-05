// @vitest-environment jsdom
// The Enter that picks an input-method candidate belongs to the input method, not the box (#2733): creating a todo
// in 关联待办 and adding a blocked Claude config directory wait for the composition to end.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Session } from "../lib/types";
import { LinkTodoDialog } from "./LinkTodoDialog";
import { ClaudeDirs } from "./settings/ClaudeDirs";

const createTodo = vi.fn(async (title: string) => ({ id: "t1", title }));
vi.mock("../lib/todos", async (orig) => ({
  ...(await orig<typeof import("../lib/todos")>()),
  createTodo: (title: string) => createTodo(title),
  linkAgentToTodo: vi.fn(async () => {}),
}));

const api = vi.fn(async (path: string, _init?: unknown) =>
  path.endsWith("/blocked") ? { dirs: [], blocked: ["~/.work"], home: "/home/me" } : { dirs: [], blocked: [], home: "/home/me" },
);
vi.mock("../lib/api", async (orig) => ({
  ...(await orig<typeof import("../lib/api")>()),
  api: (path: string, init?: unknown) => api(path, init),
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  createTodo.mockClear();
  api.mockClear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const type = (input: HTMLInputElement, value: string) =>
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });

const press = (input: HTMLInputElement, key: string, isComposing = false) =>
  act(async () => {
    input.dispatchEvent(new KeyboardEvent("keydown", { key, isComposing, bubbles: true, cancelable: true }));
  });

const session = { machine_id: "m1", session_id: "s1" } as Session;

describe("LinkTodoDialog's new todo", () => {
  const openCreate = async () => {
    await act(async () => root.render(<LinkTodoDialog open session={session} onClose={() => {}} />));
    const button = [...document.querySelectorAll("button")].find((b) => b.textContent?.includes("新建待办"))!;
    act(() => button.click());
    return document.querySelector<HTMLInputElement>('input[type="text"]')!;
  };

  it("is not created by the Enter that confirms a composition", async () => {
    const input = await openCreate();
    type(input, "会議");
    await press(input, "Enter", true);
    expect(createTodo).not.toHaveBeenCalled();
    await press(input, "Escape", true);
    expect(document.querySelector('input[type="text"]')).toBe(input); // the row stays open
  });

  it("is created by a plain Enter", async () => {
    const input = await openCreate();
    type(input, " 会議の準備 ");
    await press(input, "Enter");
    expect(createTodo).toHaveBeenCalledExactlyOnceWith("会議の準備");
  });
});

describe("ClaudeDirs' typed rule", () => {
  const blockedPuts = () => api.mock.calls.filter(([path]) => path.endsWith("/blocked"));

  const renderInput = async () => {
    await act(async () => root.render(<ClaudeDirs mid="m1" name="mac" />));
    return host.querySelector<HTMLInputElement>("input")!;
  };

  it("is not added by the Enter that confirms a composition", async () => {
    const input = await renderInput();
    type(input, "~/.work");
    await press(input, "Enter", true);
    expect(blockedPuts()).toHaveLength(0);
  });

  it("is added by a plain Enter", async () => {
    const input = await renderInput();
    type(input, "~/.work");
    await press(input, "Enter");
    expect(blockedPuts()).toHaveLength(1);
  });
});
