// @vitest-environment jsdom
// Closing the side panel with its × gives the keyboard focus back to the button that opened it, or to the message box
// when that button is gone, never to the page (#5155).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./session/FilesPanel", () => ({
  FilesPanel: ({ onClose }: { onClose: () => void }) => (
    <button type="button" aria-label="关闭面板" onClick={onClose}>
      ×
    </button>
  ),
}));
import { registerComposer } from "../lib/links";
import { closePanel, togglePanel } from "../lib/panel";
import { SidePanel } from "./SidePanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let trigger: HTMLButtonElement;
const until = async (ok: () => boolean) => {
  for (let i = 0; i < 200 && !ok(); i++) await act(async () => { await new Promise((r) => setTimeout(r, 10)); });
};
const close = () => host.querySelector<HTMLButtonElement>('aside [aria-label="关闭面板"]');

// the header's Files button, pressed from the keyboard (focused, then clicked, as Enter does)
async function openFromButton() {
  trigger.focus();
  act(() => trigger.click());
  await until(() => Boolean(close()));
  expect(close()).not.toBeNull();
}

// Tab to × and press it
function pressClose() {
  const x = close()!;
  x.focus();
  expect(document.activeElement).toBe(x);
  act(() => x.click());
  expect(host.querySelector("aside")).toBeNull();
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  trigger = document.createElement("button");
  trigger.textContent = "文件";
  trigger.addEventListener("click", () => togglePanel({ kind: "files", mid: "m1", sid: "s1", cwd: "/work/demo" }));
  document.body.append(trigger);
  root = createRoot(host);
  act(() => root.render(<SidePanel />));
});

afterEach(() => {
  act(() => closePanel());
  act(() => root.unmount());
  host.remove();
  trigger.remove();
});

describe("side panel focus on close", () => {
  it("gives the focus back to the button that opened the panel", async () => {
    await openFromButton();
    pressClose();
    expect(document.activeElement).toBe(trigger);
  });

  it("gives it to the message box when that button is gone", async () => {
    const box = document.createElement("textarea");
    document.body.append(box);
    const unregister = registerComposer({ key: "m1/s1", send: async () => true, focus: () => box.focus() });
    try {
      await openFromButton();
      trigger.remove();
      pressClose();
      expect(document.activeElement).toBe(box);
    } finally {
      unregister();
      box.remove();
    }
  });

  it("leaves the focus where the user put it when the panel closes from outside", async () => {
    const other = document.createElement("input");
    document.body.append(other);
    try {
      await openFromButton();
      other.focus();
      act(() => closePanel());
      expect(document.activeElement).toBe(other);
    } finally {
      other.remove();
    }
  });
});
