// @vitest-environment jsdom
// "Activity" in an extension's details: the figures on screen always belong to the chosen window and extension, a failed
// refresh is shown rather than passing the old figures off as current, and one hub 404 does not hide the next extension.
// Its log: the lines on screen always answer the current search, and a failed search is shown.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

const api = vi.hoisted(() => ({ metrics: vi.fn(), logs: vi.fn(), follow: vi.fn() }));
vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  getExtensionMetrics: api.metrics,
  getExtensionLogs: api.logs,
  followExtensionLogs: api.follow,
}));

import { AdminError } from "../api";
import type { ExtensionMetrics, LogsPage, MetricsWindow } from "../types";
import { ExtensionRunning } from "./ExtensionRunning";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const text = () => host.textContent ?? "";
const render = async (id: string) => {
  await act(async () => root.render(<ExtensionRunning id={id} />));
  await flush();
};
const pick = async (label: string) => {
  const radio = [...host.querySelectorAll<HTMLElement>('[role="radio"]')].find((b) => b.textContent?.trim() === label)!;
  await act(async () => radio.click());
  await flush();
};
const metrics = (calls: number): ExtensionMetrics =>
  ({
    window: "24h",
    total: { calls, errors: 0, error_rate: 0, p50_ms: 1, p95_ms: 2, p99_ms: 3 },
    series: [],
    methods: [],
    starts: 1,
    exits: 0,
    exit_reasons: {},
    recent_exits: [],
  }) as unknown as ExtensionMetrics;
const deferred = <T,>() => {
  let resolve!: (v: T) => void, reject!: (e: unknown) => void;
  const promise = new Promise<T>((a, b) => { resolve = a; reject = b; });
  return { promise, resolve, reject };
};

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  api.metrics.mockReset();
  api.logs.mockReset().mockImplementation(() => new Promise(() => {}));
  api.follow.mockReset().mockImplementation(() => () => {});
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  vi.useRealTimers();
});

describe("ExtensionRunning", () => {
  it("drops the previous window's figures while the new window loads", async () => {
    const week = deferred<ExtensionMetrics>();
    api.metrics.mockImplementation((_id: string, win: MetricsWindow) => (win === "7d" ? week.promise : Promise.resolve(metrics(4321))));
    await render("ext.a");
    expect(text()).toContain("4,321");
    await pick("7 天");
    expect(api.metrics).toHaveBeenLastCalledWith("ext.a", "7d");
    expect(text()).not.toContain("4,321");
    expect(text()).toContain("加载中");
    await act(async () => week.resolve(metrics(98765)));
    await flush();
    expect(text()).toContain("98,765");
  });

  it("shows a failed refresh instead of passing the old figures off as current", async () => {
    api.metrics.mockResolvedValueOnce(metrics(4321)).mockRejectedValueOnce(new Error("refresh broke"));
    await render("ext.a");
    expect(text()).toContain("4,321");
    await act(async () => { vi.advanceTimersByTime(30_000); });
    await flush();
    expect(api.metrics).toHaveBeenCalledTimes(2);
    expect(text()).toContain("refresh broke");
  });

  it("does not poll while the page is hidden", async () => {
    api.metrics.mockResolvedValue(metrics(1));
    const state = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    try {
      await render("ext.a");
      await act(async () => { vi.advanceTimersByTime(90_000); });
      expect(api.metrics).not.toHaveBeenCalled();
      state.mockReturnValue("visible");
      await act(async () => { document.dispatchEvent(new Event("visibilitychange")); });
      await flush();
      expect(api.metrics).toHaveBeenCalledTimes(1);
    } finally {
      state.mockRestore();
    }
  });

  it("a hub 404 for one extension does not hide the next one's section", async () => {
    api.metrics.mockImplementation((id: string) => (id === "ext.a" ? Promise.reject(new AdminError(404, "not found", {})) : Promise.resolve(metrics(555))));
    await render("ext.a");
    expect(host.querySelector("section")).toBeNull();
    await render("ext.b");
    expect(host.querySelector("section")).not.toBeNull();
    expect(text()).toContain("555");
  });
});

describe("ExtensionLogs", () => {
  const page = (next: number, ...texts: string[]): LogsPage => ({
    lines: texts.map((text, i) => ({ seq: next - texts.length + i, at: "2026-01-01T00:00:00Z", source: "program", text })),
    next,
    kept: texts.length,
  });
  const logs = () => host.querySelectorAll("section")[1]?.textContent ?? "";
  // The search waits 0.3 s after the last key.
  const search = async (q: string) => {
    const input = host.querySelector<HTMLInputElement>("input")!;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, q);
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => { vi.advanceTimersByTime(300); });
    await flush();
  };
  const start = async (id = "ext.a") => {
    await act(async () => root.render(<ExtensionRunning id={id} />));
    await act(async () => { vi.advanceTimersByTime(300); });
    await flush();
  };

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "setTimeout", "clearTimeout"] });
    api.metrics.mockResolvedValue(metrics(1));
  });

  it("drops the last search's lines while the new search is pending", async () => {
    api.logs.mockImplementation((_id: string, q: string) => (q ? new Promise(() => {}) : Promise.resolve(page(3, "alpha old"))));
    await start();
    expect(logs()).toContain("alpha old");
    await search("beta");
    expect(api.logs).toHaveBeenLastCalledWith("ext.a", "beta");
    expect(logs()).not.toContain("alpha old");
    expect(logs()).toContain("加载中");
  });

  it("shows a failed search instead of the last search's lines", async () => {
    api.logs.mockImplementation((_id: string, q: string) => (q ? Promise.reject(new Error("search broke")) : Promise.resolve(page(3, "alpha old"))));
    await start();
    expect(logs()).toContain("alpha old");
    await search("beta");
    expect(logs()).toContain("search broke");
    expect(logs()).not.toContain("alpha old");
  });

  it("ignores a late answer for an older search", async () => {
    const older = deferred<LogsPage>();
    const newer = deferred<LogsPage>();
    api.logs.mockImplementation((_id: string, q: string) => (q === "be" ? older.promise : q === "beta" ? newer.promise : Promise.resolve(page(1, "first"))));
    await start();
    await search("be");
    await search("beta");
    await act(async () => newer.resolve(page(9, "beta new")));
    await flush();
    await act(async () => older.resolve(page(5, "be stale")));
    await flush();
    expect(logs()).toContain("beta new");
    expect(logs()).not.toContain("be stale");
  });

  it("follows from where the current search's answer ends", async () => {
    const newer = deferred<LogsPage>();
    api.logs.mockImplementation((_id: string, q: string) => (q ? newer.promise : Promise.resolve(page(10, "alpha old"))));
    await start();
    const follow = [...host.querySelectorAll("button")].find((b) => b.textContent?.includes("实时跟随"))!;
    await act(async () => follow.click());
    expect(api.follow).toHaveBeenLastCalledWith("ext.a", 9, expect.any(Function), expect.any(Function));
    api.follow.mockClear();
    await search("beta");
    // nothing follows the new search before its answer says where its lines end
    expect(api.follow).not.toHaveBeenCalled();
    await act(async () => newer.resolve(page(20, "beta new")));
    await flush();
    expect(api.follow).toHaveBeenCalledTimes(1);
    expect(api.follow).toHaveBeenLastCalledWith("ext.a", 19, expect.any(Function), expect.any(Function));
  });
});
