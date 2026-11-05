// @vitest-environment jsdom
// 机器: a refresh that fails after a good one keeps the last cards up and says so in red; a hub with no
// machines shows the empty state, and one that cannot be reached at all says it cannot see them.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Machine } from "../lib/types";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

const fake = vi.hoisted(() => ({ machines: [] as Machine[] }));
vi.mock("../lib/store", async (importOriginal) => {
  const real = await importOriginal<typeof import("../lib/store")>();
  return { ...real, useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T) => select({ ...real.getState(), loaded: true, machines: fake.machines }) };
});
vi.mock("../lib/sessions", async (importOriginal) => ({ ...(await importOriginal<typeof import("../lib/sessions")>()), useAgentSessions: () => [] }));

const { Machines } = await import("./Machines");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let replies: number[] = [];
const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
function hub(input: RequestInfo | URL): Promise<Response> {
  const path = String(input).split("?")[0]!;
  if (path !== "/api/v1/metrics") return Promise.resolve(json({ error: "not found" }, 404));
  const status = replies.length > 1 ? replies.shift()! : replies[0] ?? 200;
  return Promise.resolve(status === 200 ? json({ machines: [] }) : json({ error: "unavailable" }, status));
}

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
};
const alert = () => host.querySelector('[role="alert"]');

beforeEach(() => {
  fake.machines = [];
  replies = [];
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  vi.stubGlobal("fetch", vi.fn(hub));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("Machines", () => {
  it("keeps the last cards and says so in red when a refresh fails", async () => {
    fake.machines = [{ id: "mac", name: "电脑 A", online: true }];
    replies = [200, 503];
    act(() => root.render(<Machines onToggleSidebar={() => {}} />));
    await flush();
    expect(alert()).toBeNull();
    expect(host.textContent).toContain("电脑 A");
    await act(async () => { vi.advanceTimersByTime(10_000); });
    await flush();
    expect(alert()?.textContent).toContain("显示的是上次的数据");
    expect(host.textContent).toContain("电脑 A");
    expect(host.textContent).not.toContain("还没有机器");
  });

  it("shows the empty state when the hub has no machines", async () => {
    act(() => root.render(<Machines onToggleSidebar={() => {}} />));
    await flush();
    expect(host.textContent).toContain("还没有机器");
    expect(alert()).toBeNull();
  });

  it("says it cannot see the machines when the first load fails", async () => {
    replies = [503];
    act(() => root.render(<Machines onToggleSidebar={() => {}} />));
    await flush();
    expect(host.textContent).toContain("现在看不到机器");
    expect(alert()).not.toBeNull();
    expect(alert()?.textContent).not.toContain("显示的是上次的数据");
  });
});
