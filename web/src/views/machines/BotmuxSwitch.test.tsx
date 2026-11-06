// @vitest-environment jsdom
// "Also show botmux sessions" in a machine page's header (#2575, #2842): shows the hub's value, a flip PUTs it, a failed save flips back, and an older hub
// without the route shows no switch.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
// api() reads the token from localStorage, which this jsdom has none of
vi.hoisted(() => {
  const kept = new Map<string, string>();
  const g = globalThis as unknown as { localStorage: Pick<Storage, "getItem" | "setItem" | "removeItem"> };
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});
import { BotmuxSwitch } from "./BotmuxSwitch";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let on = true;
let failPut = false;
let missing = false;
let puts: unknown[] = [];

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
function hub(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const path = String(input).split("?")[0]!;
  if (path !== "/api/v1/machines/mac/botmux" || missing) return Promise.resolve(json({ error: "not found" }, 404));
  if (init?.method === "PUT") {
    const body = JSON.parse(String(init.body)) as { on: boolean };
    puts.push(body);
    if (failPut) return Promise.resolve(json({ error: "boom" }, 500));
    on = body.on;
  }
  return Promise.resolve(json({ on }));
}

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  // Response.text() resolves after a macrotask
  for (let i = 0; i < 5; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
};
const sw = () => host.querySelector<HTMLButtonElement>('button[data-testid="botmux-switch"][role="switch"]');

beforeEach(() => {
  on = true;
  failPut = false;
  missing = false;
  puts = [];
  vi.stubGlobal("fetch", vi.fn(hub));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("BotmuxSwitch", () => {
  it("shows the hub's value and saves a flip", async () => {
    act(() => root.render(<BotmuxSwitch machineID="mac" />));
    await flush();
    expect(sw()?.getAttribute("aria-checked")).toBe("true");
    expect(sw()?.textContent).toContain("botmux");
    expect(sw()?.textContent).toContain("开");
    // what it does, for a screen reader too (the hover title is not read out)
    expect(document.getElementById(sw()!.getAttribute("aria-describedby")!)?.textContent).toContain("botmux");
    act(() => sw()!.click());
    await flush();
    expect(puts).toEqual([{ on: false }]);
    expect(sw()?.getAttribute("aria-checked")).toBe("false");
    expect(sw()?.textContent).toContain("关");
  });

  it("flips back when the save fails", async () => {
    failPut = true;
    act(() => root.render(<BotmuxSwitch machineID="mac" />));
    await flush();
    act(() => sw()!.click());
    await flush();
    expect(sw()?.getAttribute("aria-checked")).toBe("true");
  });

  it("is not shown by an older hub without the route", async () => {
    missing = true;
    act(() => root.render(<BotmuxSwitch machineID="mac" />));
    await flush();
    expect(sw()).toBeNull();
  });
});
