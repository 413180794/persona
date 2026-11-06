// @vitest-environment jsdom
// A knowledge search shown from the cache paints at once and is still asked again, so entries the hub learned since appear.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { KBEntry, KBResult } from "../../lib/kb";
import { lists, searches } from "./cache";
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
const oldEntry: KBEntry = { kind: "docs", id: "old", name: "Older guide", relevance: 0 };
const newEntry: KBEntry = { kind: "docs", id: "new", name: "Newer guide", relevance: 0 };
const hit = (entry: KBEntry): KBResult => ({ entry, count: 1 });
const input = () => host.querySelector<HTMLInputElement>('input[aria-label="搜索知识库"]')!;
const render = () => act(() => root.render(
  <ListPane kind="docs" detailOpen={false} reloadKey={0} onToggleSidebar={() => {}} onOpen={() => {}} />,
));
const search = (value: string) => act(() => {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input(), value);
  input().dispatchEvent(new Event("input", { bubbles: true }));
});
const searchRequests = () => requests.filter((r) => r.path.startsWith("/kb-search"));
const typed = async () => act(async () => vi.advanceTimersByTime(300));

beforeEach(() => {
  vi.useFakeTimers();
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

describe("a cached knowledge search", () => {
  it("shows the cached results at once, asks the hub again and shows what it answers", async () => {
    searches.set("ab", [hit(oldEntry)]);
    render();
    search("ab");
    expect(host.textContent).toContain(oldEntry.name);
    expect(host.textContent).not.toContain("搜索中");

    await typed();
    expect(searchRequests().map((r) => r.path)).toEqual(["/kb-search?q=ab&results=1"]);
    await act(async () => searchRequests()[0].resolve({ results: [hit(oldEntry), hit(newEntry)] }));
    expect(host.textContent).toContain(oldEntry.name);
    expect(host.textContent).toContain(newEntry.name);
    expect(searches.get("ab")?.map((r) => r.entry.id)).toEqual(["old", "new"]);
  });

  it("keeps the cached results without an error when the refresh fails", async () => {
    searches.set("ab", [hit(oldEntry)]);
    render();
    search("ab");
    await typed();
    await act(async () => searchRequests()[0].reject(new TypeError("Failed to fetch")));
    expect(host.textContent).toContain(oldEntry.name);
    expect(host.textContent).not.toContain("连不上 hub");
    expect(searches.get("ab")?.map((r) => r.entry.id)).toEqual(["old"]);
  });

  it("paints a cached search without the loading label after a remount", async () => {
    render();
    search("ab");
    await typed();
    await act(async () => searchRequests()[0].resolve({ results: [hit(oldEntry)] }));
    await act(async () => root.unmount());
    root = createRoot(host);

    render();
    search("ab");
    expect(host.textContent).toContain(oldEntry.name);
    expect(host.textContent).not.toContain("搜索中");
    expect(host.textContent).not.toContain("加载中");
    await typed();
    expect(searchRequests()).toHaveLength(2);
  });
});
