// @vitest-environment jsdom
// #4678: the 交给 Agent dialog keeps what is typed and the agent picked while sessions are pushed; it starts over only
// when it opens again, and moves the pick only once the picked agent is gone. The hub is faked; the data made up.
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

const api = vi.fn(async (_path: string, _init?: unknown): Promise<unknown> => ({}));
vi.mock("../lib/api", async (orig) => ({
  ...(await orig<typeof import("../lib/api")>()),
  api: (path: string, init?: unknown) => api(path, init),
}));

const { getState, upsertSession, dropSession } = await import("../lib/store");
const { TodosView } = await import("./Todos");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const button = (label: string) => [...document.querySelectorAll<HTMLElement>("button")].find((b) => b.textContent?.trim() === label);
const click = async (el: Element | undefined | null) => {
  expect(el, "element").toBeTruthy();
  await act(async () => (el as HTMLElement).click());
  await flush();
};
const dialog = () => document.querySelector<HTMLElement>("[role=dialog]")!;
const box = () => dialog().querySelector<HTMLTextAreaElement>("textarea")!;
const typeInto = async (el: HTMLTextAreaElement, value: string) => {
  const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!;
  await act(async () => {
    set.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
};
const agentRow = (title: string) =>
  [...dialog().querySelectorAll<HTMLElement>("div.cursor-pointer")].find((r) => r.textContent?.includes(title));
const picked = () => [...dialog().querySelectorAll<HTMLElement>("div.cursor-pointer")].find((r) => r.className.includes("bg-accent"))?.textContent ?? "";

const iso = (agoMs: number) => new Date(Date.now() - agoMs).toISOString();
const todo: Todo = { id: "t1", title: "整理发布清单", created_at: iso(7_200_000), updated_at: iso(3_600_000) };
const session = (sid: string, title: string, o: Partial<Session> = {}) =>
  ({ machine_id: "m1", session_id: sid, title, status: "idle", ...o }) as Session;

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };

beforeEach(() => {
  api.mockReset();
  api.mockImplementation(async (path: string) => (path.endsWith("/launchers") ? { launchers: [] } : {}));
  seed({
    todos: [todo],
    todosLoaded: true,
    machines: [{ id: "m1", name: "示例电脑", online: true }],
    sessions: { m1: [session("s1", "第一个 Agent"), session("s2", "第二个 Agent")] },
  });
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

const openHandover = async () => {
  await act(async () => root.render(<TodosView selectedId="t1" />));
  await flush();
  await click(button("交给 Agent"));
  await click(button("交给已有的 Agent"));
};

describe("交给 Agent 对话框开着时", () => {
  it("会话推送不清空输入、不改选中的 Agent", async () => {
    await openHandover();
    await click(agentRow("第二个 Agent"));
    await typeInto(box(), "把网络层的测试补齐");
    expect(picked()).toContain("第二个 Agent");

    // an agent starts working: the hub pushes its session
    await act(async () => upsertSession(session("s1", "第一个 Agent", { status: "working" })));
    await flush();
    expect(box().value).toBe("把网络层的测试补齐");
    expect(picked()).toContain("第二个 Agent");
  });

  it("选中的 Agent 不在了才换成第一个；关掉再打开从头开始", async () => {
    await openHandover();
    await click(agentRow("第二个 Agent"));
    await typeInto(box(), "先看一下");
    await act(async () => dropSession("m1", "s2"));
    await flush();
    expect(picked()).toContain("第一个 Agent");
    expect(box().value).toBe("先看一下");

    await click(button("取消"));
    await click(button("交给 Agent"));
    await click(button("交给已有的 Agent"));
    expect(box().value).toBe("");
  });

  it("关掉再打开时输入清空，选中项回到第一个", async () => {
    await openHandover();
    await click(agentRow("第二个 Agent"));
    await typeInto(box(), "这句不发了");
    await click(button("取消"));
    await click(button("交给 Agent"));
    await click(button("交给已有的 Agent"));
    expect(box().value).toBe("");
    expect(picked()).toContain("第一个 Agent");
  });

  it("开着时待办换了（比如被别处删掉），写给前一个待办的话不带过去", async () => {
    seed({ todos: [todo, { ...todo, id: "t2", title: "补齐测试" }] });
    await openHandover();
    await typeInto(box(), "写给整理发布清单的");
    await act(async () => root.render(<TodosView selectedId="t2" />));
    await flush();
    expect(box()?.value ?? "").toBe("");
  });
});
