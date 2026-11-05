// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Machine } from "../lib/types";

vi.mock("../lib/store", () => {
  const state = { machines: [{ id: "first", name: "First computer", online: true }, { id: "second", name: "Second computer", online: true }] };
  return { useStore: (selector: (value: { machines: Machine[] }) => unknown) => selector(state) };
});
vi.mock("react-dom", async (original) => ({ ...(await original<typeof import("react-dom")>()), createPortal: (children: React.ReactNode) => children }));

import { NewAgent } from "./NewAgent";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type Pending = { path: string; signal?: AbortSignal | null; answer: (value: unknown) => void; reject: (error: unknown) => void };
let pending: Pending[];
let host: HTMLDivElement;
let root: Root;
const flush = () => act(async () => { await Promise.resolve(); });
const dirs = (name: string) => ({ path: `/projects/${name}`, entries: [{ name, path: `/projects/${name}/child` }] });
const clickMachine = (name: string) => act(() => [...host.querySelectorAll("button")].find((button) => button.textContent?.trim() === name)!.click());

beforeEach(() => {
  localStorage.clear();
  pending = [];
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (!String(input).includes("/dirs?")) return Promise.resolve(new Response("{}"));
    return new Promise<Response>((resolve, reject) => pending.push({ path: String(input), signal: init?.signal, answer: (value) => resolve(new Response(JSON.stringify(value))), reject }));
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<NewAgent open onClose={() => {}} />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("NewAgent directory requests", () => {
  it("keeps the current machine's directories when an old answer arrives last", async () => {
    await flush();
    const first = pending[0]!;
    clickMachine("Second computer");
    await flush();
    const second = pending.at(-1)!;
    expect(second.path).toContain("/machines/second/dirs?");
    await act(async () => second.answer(dirs("current-folder")));
    expect(host.textContent).toContain("current-folder");
    await act(async () => first.answer(dirs("stale-folder")));
    expect(host.textContent).not.toContain("stale-folder");
    expect(host.textContent).toContain("current-folder");
    expect(first.signal?.aborted).toBe(true);
  });

  it("keeps the current directories when a cancelled request rejects", async () => {
    await flush();
    const first = pending[0]!;
    clickMachine("Second computer");
    await flush();
    await act(async () => pending.at(-1)!.answer(dirs("current-folder")));
    await act(async () => first.reject(new DOMException("Aborted", "AbortError")));
    expect(host.textContent).toContain("current-folder");
  });

  it("keeps the directory spinner until the current request finishes", async () => {
    await flush();
    const first = pending[0]!;
    clickMachine("Second computer");
    await flush();
    const spinner = () => host.querySelector('span[style*="width: 10px"]');
    expect(spinner()).not.toBeNull();
    await act(async () => first.reject(new DOMException("Aborted", "AbortError")));
    expect(spinner()).not.toBeNull();
    await act(async () => pending.at(-1)!.answer(dirs("current-folder")));
    expect(spinner()).toBeNull();
  });
});
