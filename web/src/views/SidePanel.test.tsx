// @vitest-environment jsdom
// The side panel's Files / Git / file viewer / link panels load when first opened (#2673): the panel shows the loading
// look while the chunk comes, then the panel; a chunk that will not load shows a way to retry, never a blank panel.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const gate = vi.hoisted(() => {
  let open!: () => void;
  const ready = new Promise<void>((r) => (open = r));
  return { ready, open: () => open() };
});
vi.mock("./session/FilesPanel", async () => {
  await gate.ready;
  return { FilesPanel: ({ cwd }: { cwd: string }) => <p data-testid="files">文件 {cwd}</p> };
});
vi.mock("./session/GitPanel", () => {
  throw new TypeError("Failed to fetch dynamically imported module: /assets/GitPanel-abc.js");
});
import { closePanel, openPanel } from "../lib/panel";
import { SidePanel } from "./SidePanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
// a lazy chunk resolves after an unknown number of ticks (the first test also pays for the module's transform)
const until = async (ok: () => boolean) => {
  for (let i = 0; i < 200 && !ok(); i++) await act(async () => { await new Promise((r) => setTimeout(r, 10)); });
};

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => closePanel());
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
});

describe("侧边面板按需加载", () => {
  it("shows the loading look until the panel's chunk is there, then the panel", async () => {
    act(() => openPanel({ kind: "files", mid: "m1", sid: "s1", cwd: "/work/demo" }));
    act(() => root.render(<SidePanel />));
    await act(async () => { await Promise.resolve(); });
    expect(host.querySelector('[data-testid="files"]')).toBeNull();
    expect(host.textContent).toContain("加载");
    act(() => gate.open());
    await until(() => Boolean(host.querySelector('[data-testid="files"]')));
    expect(host.querySelector('[data-testid="files"]')?.textContent).toBe("文件 /work/demo");
  });

  // vitest wraps the factory's error, so this is the boundary's general message; which errors reload is PageBoundary.test
  it("shows a way to retry, inside the panel, when a panel's chunk does not load", async () => {
    sessionStorage.setItem("persona.chunkReload", String(Date.now()));
    vi.spyOn(console, "error").mockImplementation(() => {});
    act(() => openPanel({ kind: "git", mid: "m1", sid: "s1", cwd: "/work/demo" }));
    act(() => root.render(<SidePanel />));
    await until(() => Boolean(host.querySelector('[role="alert"]')));
    const alert = host.querySelector('[role="alert"]');
    expect(alert?.textContent).toContain("重新加载");
    // the panel's frame (its resize handle) is still there: the error stays inside the panel
    expect(host.querySelector('aside [role="separator"]')).not.toBeNull();
  });
});
