// @vitest-environment jsdom
// Machine detail, offline: when the error text says why, the plain reason and next step lead and the raw error goes
// last; when it does not, the raw text stays first (#3633).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Machine } from "../../lib/types";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

const fake = vi.hoisted(() => ({ machines: [] as Machine[] }));
vi.mock("../../lib/store", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../lib/store")>();
  return { ...real, useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T) => select({ ...real.getState(), loaded: true, machines: fake.machines }) };
});

const { MachineDetail } = await import("./MachineDetail");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
};
const box = () => host.querySelector("[data-offline-box]");

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify({ error: "not found" }), { status: 404 }))));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("MachineDetail offline box", () => {
  it("leads with the plain reason and puts the raw error last", async () => {
    const raw = "ssh: connect to host box port 22: Operation timed out";
    fake.machines = [{ id: "box", name: "盒子", online: false, error: raw } as Machine];
    act(() => root.render(<MachineDetail mid="box" onToggleSidebar={() => {}} />));
    await flush();
    const lines = [...box()!.querySelectorAll("p")];
    expect(lines[0]!.getAttribute("data-failure")).toBe("timeout");
    expect(lines[0]!.textContent).not.toContain("ssh:");
    expect(lines.at(-1)!.hasAttribute("data-raw-error")).toBe(true);
    expect(lines.at(-1)!.textContent).toBe(raw);
  });

  it("keeps the raw text first when it does not say why", async () => {
    fake.machines = [{ id: "box", name: "盒子", online: false, error: "something odd" } as Machine];
    act(() => root.render(<MachineDetail mid="box" onToggleSidebar={() => {}} />));
    await flush();
    const lines = [...box()!.querySelectorAll("p")];
    expect(lines[0]!.textContent).toBe("something odd");
    expect(box()!.querySelector("[data-raw-error]")).toBeNull();
  });
});
