// @vitest-environment jsdom
// "Browser (ego lite)" reads on a Mac's page: an answer for another machine, or an older answer, never lands on the page that
// is shown now, and the 2 s watch pauses while the tab is hidden and asks right away once it is shown again.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
// api() reads the token from localStorage, which this jsdom has none of
vi.hoisted(() => {
  const kept = new Map<string, string>();
  const g = globalThis as unknown as { localStorage: Pick<Storage, "getItem" | "setItem" | "removeItem"> };
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});
vi.mock("./BrowserLogins", () => ({ BrowserLogins: () => <div data-testid="browser-logins" /> }));

import type { MachineBrowser } from "../../lib/browser";
import { BrowserSection } from "./BrowserSection";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Every request waits until the test answers it, so the order of the answers is the test's.
type Pending = { method: string; path: string; answer: (v: MachineBrowser) => void };
let pending: Pending[] = [];
const json = (v: unknown) => new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
function hub(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return new Promise((resolve) => {
    pending.push({ method: init?.method ?? "GET", path: String(input).split("?")[0]!, answer: (v) => resolve(json(v)) });
  });
}

const notInstalled: MachineBrowser = { enabled: true, installed: false };
const ready: MachineBrowser = { enabled: true, installed: true, set_up: true, ready: true };
// the first-run setup was opened on the Mac: the page asks again every 2 s
const settingUp: MachineBrowser = { enabled: true, installed: true, set_up: false, job: { kind: "open", state: "running" } };

let host: HTMLDivElement;
let root: Root;
let visibility: DocumentVisibilityState = "visible";
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); await Promise.resolve(); });
};
const installButton = () => [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "安装 ego lite");
const logins = () => host.querySelector('[data-testid="browser-logins"]');
const render = (mid: string, b: MachineBrowser) => act(() => root.render(<BrowserSection mid={mid} browser={b} online />));
const answer = async (p: Pending, v: MachineBrowser) => { p.answer(v); await flush(); };

beforeEach(() => {
  pending = [];
  visibility = "visible";
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
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

describe("BrowserSection reads", () => {
  it("drops an answer for the machine that was shown before", async () => {
    render("a", ready);
    await flush();
    const a = pending.find((p) => p.path === "/api/v1/machines/a/browser")!;
    expect(a).toBeDefined();

    render("b", ready);
    await flush();
    // a's answer arrives after b's page opened: b keeps its own state
    await answer(a, notInstalled);
    expect(installButton()).toBeUndefined();
    expect(logins()).not.toBeNull();

    const b = pending.find((p) => p.path === "/api/v1/machines/b/browser")!;
    await answer(b, ready);
    expect(installButton()).toBeUndefined();
  });

  it("drops a switch answer for the machine that was shown before", async () => {
    render("a", { enabled: false });
    await flush();
    act(() => host.querySelector<HTMLButtonElement>('button[role="switch"]')!.click());
    const put = pending.find((p) => p.method === "PUT")!;
    expect(put.path).toBe("/api/v1/machines/a/browser");

    render("b", ready);
    await flush();
    await answer(put, notInstalled);
    expect(installButton()).toBeUndefined();
    expect(logins()).not.toBeNull();
  });

  it("keeps the newer of two overlapping answers", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    render("mac", settingUp);
    await flush();
    await act(async () => { vi.advanceTimersByTime(2000); });
    await flush();
    const gets = pending.filter((p) => p.method === "GET");
    expect(gets).toHaveLength(2);

    // the newer answer comes first, the older one after it
    await answer(gets[1]!, ready);
    await answer(gets[0]!, notInstalled);
    expect(installButton()).toBeUndefined();
    expect(logins()).not.toBeNull();
  });

  it("does not ask while the tab is hidden, and asks once it is shown again", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    visibility = "hidden";
    render("mac", settingUp);
    await flush();
    const opened = pending.length;

    await act(async () => { vi.advanceTimersByTime(2000); });
    await flush();
    expect(pending).toHaveLength(opened);

    visibility = "visible";
    await act(async () => { document.dispatchEvent(new Event("visibilitychange")); });
    await flush();
    expect(pending).toHaveLength(opened + 1);
    expect(pending.at(-1)!.path).toBe("/api/v1/machines/mac/browser");
  });
});
