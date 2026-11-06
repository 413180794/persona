// @vitest-environment jsdom
// Settings > Claude Code: two rows saved at once. Each row stays busy until its own save ends, and the shown computer's
// answer to one save does not put another row back to the value it had before its own save.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentSetting } from "../../lib/agent-settings";

type Call = { path: string; method: string; json?: { key: string; value: unknown }; resolve: (v: unknown) => void; reject: (e: unknown) => void };
const calls: Call[] = [];
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string, init?: { method?: string; json?: { key: string; value: unknown } }) =>
    new Promise((resolve, reject) => calls.push({ path, method: init?.method ?? "GET", json: init?.json, resolve, reject })),
}));
const MACHINES = [{ id: "m-1", name: "工作", online: true }];
vi.mock("../../lib/store", async (orig) => ({
  ...(await orig<typeof import("../../lib/store")>()),
  useStore: (select: (s: { machines: typeof MACHINES }) => unknown) => select({ machines: MACHINES }),
}));
vi.mock("./ClaudeDirs", () => ({ ClaudeDirs: () => null })); // its own reads are not this test's
vi.mock("../../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../../ui/atoms")>()), toast: () => {} }));

const { AgentSettings } = await import("./AgentSettings");

// Two bool options the catalog has no words for, so each row is named by the title the node sends.
const row = (key: string, value: boolean | null): AgentSetting => ({ key, title: key, detail: "", type: "bool", default: false, value });
const list = (a: boolean | null, b: boolean | null) => [row("optA", a), row("optB", b)];

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  calls.length = 0;
  localStorage.clear();
  localStorage.setItem("jarvis.agentSettings.applyAll", "0"); // only the shown computer, one PUT per change
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const sw = (label: string) => host.querySelector<HTMLButtonElement>(`[role="switch"][aria-label="${label}"]`)!;
const click = (label: string) => act(async () => sw(label).click());
const put = (key: string) => calls.find((c) => c.method === "PUT" && c.json?.key === key)!;
const answer = (c: Call, v: unknown) => act(async () => c.resolve(v));

async function loaded() {
  await act(async () => root.render(<AgentSettings agent="claude" />));
  await answer(calls.find((c) => c.method === "GET" && c.path.endsWith("/claude-settings"))!, { settings: list(false, false) });
}

describe("Settings > Claude Code, two saves at once", () => {
  it("keeps the other row busy and at its new value when the first save answers", async () => {
    await loaded();
    await click("optA");
    await click("optB");
    expect(sw("optA").disabled).toBe(true);
    expect(sw("optB").disabled).toBe(true);

    // A's answer was read before B's write landed on the node: B is still off in it.
    await answer(put("optA"), { settings: list(true, false) });
    expect(sw("optA").disabled).toBe(false);
    expect(sw("optA").getAttribute("aria-checked")).toBe("true");
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
    expect(sw("optB").disabled).toBe(true);

    await answer(put("optB"), { settings: list(true, true) });
    expect(sw("optA").disabled).toBe(false);
    expect(sw("optB").disabled).toBe(false);
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
  });

  it("does not let a late answer to the first save undo the second", async () => {
    await loaded();
    await click("optA");
    await click("optB");
    await answer(put("optB"), { settings: list(true, true) });
    expect(sw("optB").disabled).toBe(false);
    expect(sw("optA").disabled).toBe(true);

    await answer(put("optA"), { settings: list(true, false) });
    expect(sw("optA").getAttribute("aria-checked")).toBe("true");
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
  });

  it("still takes the whole list when one save is alone", async () => {
    await loaded();
    await click("optA");
    // The node's answer also carries a change made elsewhere (B set on another device).
    await answer(put("optA"), { settings: list(true, true) });
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
  });

  it("rolls back only the refused row", async () => {
    await loaded();
    await click("optA");
    await click("optB");
    await act(async () => put("optA").reject(new Error("refused")));
    expect(sw("optA").getAttribute("aria-checked")).toBe("false");
    expect(sw("optA").disabled).toBe(false);
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
    expect(sw("optB").disabled).toBe(true);
  });
});
