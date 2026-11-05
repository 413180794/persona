// @vitest-environment jsdom
// The inbox that could not load the first time (#2970, after Android's failure empty state): its own empty state with
// 重试, never 都处理完了 under a red error; 重试 loads again and the ordinary empty state comes back.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

let fail = true;
const calls: string[] = [];
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string) => {
    calls.push(path);
    return path.startsWith("/inbox") && fail ? Promise.reject(new TypeError("Failed to fetch")) : Promise.resolve({});
  },
}));

// agents waiting on you come from the store; `waiting` puts one there (a fixed array: the selector's result is stable),
// `storeLoaded` says whether the store's first snapshot is in
const fakeStore = vi.hoisted(() => ({ waiting: false, storeLoaded: true, list: [{ id: "ix1", machine_id: "m", session_id: "s1", session_title: "改登录页", kind: "question" }] }));
vi.mock("../../lib/store", async (orig) => {
  const m = await orig<typeof import("../../lib/store")>();
  return { ...m, useStore: <T,>(sel: (s: ReturnType<typeof m.getState>) => T) => m.useStore((s) => sel({ ...s, loaded: fakeStore.storeLoaded, ...(fakeStore.waiting ? { interactions: fakeStore.list as never } : {}) })) };
});

import { InboxList } from "./InboxList";
import { resetInboxForTests } from "../../lib/inbox";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};

beforeEach(() => {
  resetInboxForTests();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("收件箱：首次加载失败", () => {
  it("shows its own empty state with 重试; 重试 loads again and 都处理完了 comes back", async () => {
    fail = true;
    act(() => root.render(<InboxList />));
    await flush();
    const failed = host.querySelector("[data-inbox-failed]");
    expect(failed?.textContent).toContain("收件箱没能加载");
    expect(failed?.textContent).toContain("连不上 hub");
    expect(host.textContent).not.toContain("都处理完了");
    expect(host.querySelector("p.text-red")).toBeNull(); // the reason is in the empty state, not a red line over it

    fail = false;
    const before = calls.filter((c) => c.startsWith("/inbox")).length;
    const retry = [...failed!.querySelectorAll("button")].find((b) => b.textContent?.trim() === "重试");
    await act(async () => retry!.click());
    await flush();
    expect(calls.filter((c) => c.startsWith("/inbox")).length).toBe(before + 1);
    expect(host.querySelector("[data-inbox-failed]")).toBeNull();
    expect(host.textContent).toContain("都处理完了");
  });

  it("with agents waiting the list is not empty: the error is one red line over it, no failed empty state", async () => {
    fail = true;
    fakeStore.waiting = true;
    try {
      act(() => root.render(<InboxList />));
      await flush();
      expect(host.querySelector("p.text-red")?.textContent).toContain("连不上 hub");
      expect(host.querySelector("[data-inbox-failed]")).toBeNull();
      expect(host.textContent).toContain("改登录页");
      expect(host.textContent).not.toContain("都处理完了");
    } finally {
      fakeStore.waiting = false;
    }
  });
});

describe("收件箱：首屏一次画好", () => {
  it("waits for the store as well as the inbox, so the agents waiting do not push painted rows down", async () => {
    fail = false;
    fakeStore.waiting = true;
    fakeStore.storeLoaded = false;
    try {
      act(() => root.render(<InboxList />));
      await flush();
      // the inbox answered, the store has not: only 加载中, no rows and no empty state yet
      expect(calls.some((c) => c.startsWith("/inbox"))).toBe(true);
      expect(host.textContent).toContain("加载中");
      expect(host.textContent).not.toContain("改登录页");
      expect(host.textContent).not.toContain("都处理完了");
      expect(host.querySelector("section")).toBeNull();

      fakeStore.storeLoaded = true;
      act(() => root.render(<InboxList />));
      await flush();
      expect(host.textContent).not.toContain("加载中");
      expect(host.textContent).toContain("改登录页");
    } finally {
      fakeStore.waiting = false;
      fakeStore.storeLoaded = true;
    }
  });
});
