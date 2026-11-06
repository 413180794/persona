// @vitest-environment jsdom
// "Computer control" on a Mac's page (#5455): a read sent before the switch, an action or a newer read never puts an older
// state back on screen.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
// api() reads the token from localStorage, which this jsdom has none of
vi.hoisted(() => {
  const kept = new Map<string, string>();
  const g = globalThis as unknown as { localStorage: Pick<Storage, "getItem" | "setItem" | "removeItem"> };
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});
import type { MachineComputer } from "../../lib/computer";
import { ComputerSection } from "./ComputerSection";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Every request waits until the test answers it, so the order of the answers is the test's.
type Pending = { method: string; path: string; answer: (v: MachineComputer) => void };
let pending: Pending[] = [];
const json = (v: unknown) => new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
function hub(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return new Promise((resolve) => {
    pending.push({ method: init?.method ?? "GET", path: String(input).split("?")[0]!, answer: (v) => resolve(json(v)) });
  });
}

const installing = (step: string): MachineComputer => ({ enabled: true, installed: false, job: { kind: "install", state: "running", step } }) as MachineComputer;
const off: MachineComputer = { enabled: false };

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); await Promise.resolve(); });
};
const toggle = () => host.querySelector<HTMLButtonElement>('[role="switch"]')!;
const take = (method: string) => {
  const i = pending.findIndex((p) => p.method === method);
  expect(i).toBeGreaterThanOrEqual(0);
  return pending.splice(i, 1)[0]!;
};

beforeEach(() => {
  pending = [];
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

describe("Computer control", () => {
  it("keeps the switch off when a read sent before turning it off answers last", async () => {
    act(() => root.render(<ComputerSection mid="mac" computer={installing("download")} online />));
    const read = take("GET"); // asked when the page opened
    act(() => toggle().click());
    await take("PUT").answer(off);
    await flush();
    expect(toggle().getAttribute("aria-checked")).toBe("false");
    read.answer(installing("download"));
    await flush();
    expect(toggle().getAttribute("aria-checked")).toBe("false");
  });

  it("shows the newest read when an older one answers after it", async () => {
    act(() => root.render(<ComputerSection mid="mac" computer={installing("download")} online />));
    const first = take("GET");
    act(() => void vi.advanceTimersByTime(2000)); // the 2 s watch asks again before the first answer
    const second = take("GET");
    second.answer(installing("verify"));
    await flush();
    expect(host.textContent).toContain("verify");
    first.answer(installing("download"));
    await flush();
    expect(host.textContent).toContain("verify");
    expect(host.textContent).not.toContain("download");
  });

  // The 2 s watch while a job runs stops in a hidden tab and asks once when the tab is shown again.
  it("does not ask while the tab is hidden, and asks once when it is shown again", async () => {
    let visibility: DocumentVisibilityState = "visible";
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
    const show = (v: DocumentVisibilityState) => act(() => { visibility = v; document.dispatchEvent(new Event("visibilitychange")); });
    try {
      act(() => root.render(<ComputerSection mid="mac" computer={installing("download")} online />));
      take("GET").answer(installing("download")); // asked when the page opened, and only once
      await flush();
      expect(pending).toHaveLength(0);
      act(() => void vi.advanceTimersByTime(2000));
      take("GET").answer(installing("download"));
      await flush();
      show("hidden");
      act(() => void vi.advanceTimersByTime(60_000));
      expect(pending).toHaveLength(0);
      show("visible");
      take("GET").answer(installing("verify"));
      await flush();
      expect(pending).toHaveLength(0);
      expect(host.textContent).toContain("verify");
    } finally {
      delete (document as { visibilityState?: unknown }).visibilityState;
    }
  });
});
