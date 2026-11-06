// @vitest-environment jsdom
// ClaudeDirs: every change is one PUT of the whole blocked list, so a second change sent while the first is in flight
// is built from the old list and, landing last, undoes the first on the hub. The buttons are disabled while busy; the
// rule input's Enter must wait the same way.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ClaudeDirsState } from "../../lib/agent-settings";
import { t } from "../../i18n";

type Call = { method: string; json?: unknown; resolve: (v: unknown) => void };
const calls: Call[] = [];
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (_path: string, init?: { method?: string; json?: unknown }) =>
    new Promise((resolve) => calls.push({ method: init?.method ?? "GET", json: init?.json, resolve })),
}));
vi.mock("../../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../../ui/atoms")>()), toast: vi.fn() }));

const { ClaudeDirs } = await import("./ClaudeDirs");

const HOME = "/home/u";
const STATE: ClaudeDirsState = {
  home: HOME,
  live: true,
  blocked: [],
  dirs: [
    { path: `${HOME}/.claude`, default: true, source: "default", running: 0 },
    { path: `${HOME}/.claude-work`, source: "found", running: 0 },
  ],
};

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  calls.length = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const puts = () => calls.filter((c) => c.method === "PUT");
const input = () => host.querySelector<HTMLInputElement>("input")!;
const type = (value: string) =>
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input(), value);
    input().dispatchEvent(new Event("input", { bubbles: true }));
  });
const enter = () => act(() => void input().dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })));
const blockButton = () => [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === t("settings-claudedirs.block"))!;

async function mount() {
  act(() => root.render(<ClaudeDirs mid="m1" name="Mac" />));
  await act(async () => calls[0].resolve(STATE));
}

describe("ClaudeDirs blocked list saves", () => {
  it("ignores Enter in the rule input while another change is in flight", async () => {
    await mount();
    act(() => blockButton().click());
    expect(puts()).toHaveLength(1);
    expect(puts()[0].json).toEqual({ blocked: ["~/.claude-work"] });

    type("~/.automation/*");
    enter();
    // A second PUT here would be built from the old list, without ~/.claude-work
    expect(puts()).toHaveLength(1);

    await act(async () => puts()[0].resolve({ ...STATE, blocked: ["~/.claude-work"], dirs: [STATE.dirs[0], { ...STATE.dirs[1], blocked: true }] }));
    enter();
    expect(puts()).toHaveLength(2);
    expect(puts()[1].json).toEqual({ blocked: ["~/.claude-work", "~/.automation/*"] });
  });

  it("sends one PUT when Enter is pressed twice quickly", async () => {
    await mount();
    type("~/.automation/*");
    enter();
    enter();
    expect(puts()).toHaveLength(1);
  });
});
