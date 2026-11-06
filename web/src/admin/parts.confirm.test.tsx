// @vitest-environment jsdom
// ConfirmButton keeps keyboard focus: the trash icon hands focus to "Cancel" when it asks, and the trigger gets it back
// when the pair goes away (Cancel, Escape, the 6 s timeout, after confirming), unless the user has moved on.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../i18n";
import { ConfirmButton } from "./parts";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let onConfirm: ReturnType<typeof vi.fn<() => void>>;

const trigger = () => host.querySelector<HTMLButtonElement>('button[aria-label="Remove source"]');
const byText = (label: string) => [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label);
const cancel = () => byText(t("admin-parts.cancel"));
const click = async (el: HTMLElement | null | undefined) => {
  expect(el).toBeTruthy();
  await act(async () => { el!.click(); });
};
const ask = async () => {
  trigger()!.focus();
  await click(trigger());
};

beforeEach(async () => {
  host = document.createElement("div");
  document.body.appendChild(host);
  onConfirm = vi.fn<() => void>();
  const other = document.createElement("input");
  other.id = "elsewhere";
  document.body.appendChild(other);
  root = createRoot(host);
  await act(async () => { root.render(<ConfirmButton label="Remove source" confirm="Remove" onConfirm={onConfirm} />); });
});

afterEach(() => {
  act(() => root.unmount());
  document.body.innerHTML = "";
  vi.useRealTimers();
});

describe("ConfirmButton focus", () => {
  it("moves focus to Cancel when it asks, and back to the trigger on Cancel", async () => {
    await ask();
    expect(document.activeElement).toBe(cancel());
    await click(cancel());
    expect(document.activeElement).toBe(trigger());
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("backs out on Escape and returns focus to the trigger", async () => {
    await ask();
    await act(async () => { cancel()!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })); });
    expect(cancel()).toBeUndefined();
    expect(document.activeElement).toBe(trigger());
  });

  it("returns focus to the trigger when the 6 s timeout reverts it", async () => {
    vi.useFakeTimers();
    await ask();
    expect(document.activeElement).toBe(cancel());
    await act(async () => { vi.advanceTimersByTime(6000); });
    expect(cancel()).toBeUndefined();
    expect(document.activeElement).toBe(trigger());
  });

  it("does not pull focus back if the user moved elsewhere before the timeout", async () => {
    vi.useFakeTimers();
    await ask();
    const other = document.getElementById("elsewhere") as HTMLInputElement;
    other.focus();
    await act(async () => { vi.advanceTimersByTime(6000); });
    expect(document.activeElement).toBe(other);
  });

  it("confirms once and returns focus to the trigger", async () => {
    await ask();
    await click(byText("Remove"));
    await act(async () => { await Promise.resolve(); });
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(cancel()).toBeUndefined();
    expect(document.activeElement).toBe(trigger());
  });
});
