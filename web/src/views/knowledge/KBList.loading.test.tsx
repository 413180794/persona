// @vitest-environment jsdom
// A cold knowledge list paints its kinds, count and rows together; cached lists and error recovery stay usable.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { KBEntry, KBKind, KBList } from "../../lib/kb";
import { lists, listKey, searches } from "./cache";
import { ListPane } from "./KBList";

type Request = { path: string; resolve: (value: unknown) => void; reject: (error: Error) => void };
const requests: Request[] = [];
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string, init: RequestInit = {}) => new Promise((resolve, reject) => {
    requests.push({ path, resolve, reject });
    init.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
  }),
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const entry: KBEntry = { kind: "docs", id: "guide", name: "Example guide", relevance: 0 };
const tablist = () => host.querySelector('[role="tablist"]');
const selectedTab = () => host.querySelector('[role="tab"][aria-selected="true"]');
const filter = () => host.querySelector('button[aria-label="按标签筛选"]');
const input = () => host.querySelector<HTMLInputElement>('input[aria-label="搜索知识库"]')!;
const render = (kind: KBKind = "docs", reloadKey = 0) => act(() => root.render(
  <ListPane kind={kind} detailOpen={false} reloadKey={reloadKey} onToggleSidebar={() => {}} onOpen={() => {}} />,
));
const answer = async (request: Request, value: unknown) => act(async () => request.resolve(value));
const fail = async (request: Request) => act(async () => request.reject(new TypeError("Failed to fetch")));
const search = (value: string) => act(() => {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input(), value);
  input().dispatchEvent(new Event("input", { bubbles: true }));
});

beforeEach(() => {
  lists.clear();
  searches.clear();
  requests.length = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  vi.useRealTimers();
  lists.clear();
  searches.clear();
});

describe("the knowledge list's first paint", () => {
  it("waits for the list before showing the kind chips, count and rows together", async () => {
    render();
    expect(requests[0].path).toBe("/kb/docs");
    expect(input()).not.toBeNull();
    expect(host.textContent).toContain("加载中");
    expect(tablist()).toBeNull();
    expect(filter()).toBeNull();
    expect(host.textContent).not.toContain("还没有文档");

    await answer(requests[0], { items: [entry], tags: ["example"] });
    expect(selectedTab()?.textContent).toBe("文档1");
    expect(filter()).not.toBeNull();
    expect(host.textContent).toContain(entry.name);
    expect(host.textContent).not.toContain("加载中");
  });

  it("finishes loading an empty response and shows its zero count", async () => {
    render();
    await answer(requests[0], { items: null, tags: null });
    expect(selectedTab()?.textContent).toBe("文档0");
    expect(filter()).not.toBeNull();
    expect(host.textContent).toContain("还没有文档");
    expect(host.textContent).not.toContain("加载中");
  });

  it.each([{ name: "empty", items: [] }, { name: "populated", items: [entry] }])("shows a $name cached list immediately and keeps it during refresh", async ({ items }) => {
    const cached: KBList = { items, tags: [] };
    lists.set(listKey("docs"), cached);
    render();
    expect(selectedTab()?.textContent).toBe(`文档${items.length}`);
    expect(host.textContent).toContain(items.length ? entry.name : "还没有文档");
    expect(host.textContent).not.toContain("加载中");
    const ready = host.innerHTML;

    render("docs", 1);
    expect(requests).toHaveLength(2);
    expect(host.innerHTML).toBe(ready);
    await fail(requests[1]);
    expect(host.innerHTML).toBe(ready);
  });

  it("keeps category links and retry available on failure, then paints the recovered list", async () => {
    render();
    await fail(requests[0]);
    expect(tablist()).not.toBeNull();
    expect(host.querySelector('[role="tab"][href="/kb/people"]')).not.toBeNull();
    expect(filter()).not.toBeNull();
    expect(host.textContent).toContain("连不上 hub");
    expect(host.textContent).not.toContain("还没有文档");

    const retry = [...host.querySelectorAll("button")].find((b) => b.textContent === "重试")!;
    act(() => retry.click());
    expect(requests).toHaveLength(2);
    expect(tablist()).toBeNull();
    expect(host.textContent).toContain("加载中");
    await answer(requests[1], { items: [entry], tags: [] });
    expect(selectedTab()?.textContent).toBe("文档1");
    expect(host.textContent).toContain(entry.name);
    expect(host.textContent).not.toContain("连不上 hub");
  });

  it("allows search before the list arrives and resumes loading when search is cleared", async () => {
    vi.useFakeTimers();
    render();
    search("guide");
    await act(async () => vi.advanceTimersByTime(300));
    expect(requests[1].path).toBe("/kb-search?q=guide&results=1");
    expect(host.textContent).toContain("搜索中");
    await answer(requests[1], { results: [{ entry, count: 1 }] });
    expect(host.textContent).toContain(entry.name);
    expect(tablist()).toBeNull();

    search("");
    expect(host.textContent).toContain("加载中");
    expect(host.textContent).not.toContain(entry.name);
    expect(tablist()).toBeNull();
    await answer(requests[0], { items: [entry], tags: [] });
    expect(selectedTab()?.textContent).toBe("文档1");
    expect(host.textContent).toContain(entry.name);
  });
});
