// @vitest-environment jsdom
// The knowledge list's tag filter popover from the keyboard: it is portalled to the end of <body>, so opening it moves
// focus onto the current chip, the arrow keys walk the chips, and Esc or a pick hands focus back to the filter button.
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { TagFilter } from "./KBList";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

function Harness({ initial }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <button type="button">before</button>
      <TagFilter tags={["alpha", "beta", "gamma"]} value={value} onChange={setValue} />
      <button type="button">after</button>
    </>
  );
}

const trigger = () => host.querySelector<HTMLButtonElement>('button[aria-label^="按标签筛选"]')!;
const dialog = () => document.body.querySelector<HTMLElement>('[role="dialog"]');
const chips = () => [...dialog()!.querySelectorAll<HTMLButtonElement>("[data-chips] > button")];
const chip = (label: string) => chips().find((b) => b.textContent === label)!;
const key = (target: Element, k: string) => act(() => { target.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true })); });

function open() {
  trigger().focus();
  act(() => trigger().click());
  expect(dialog()).not.toBeNull();
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("the knowledge tag filter popover from the keyboard", () => {
  it("focuses 全部 when no tag is picked, and the picked tag's chip otherwise", () => {
    act(() => root.render(<Harness />));
    open();
    expect(document.activeElement).toBe(chip("全部"));
    key(document.activeElement!, "Escape");

    act(() => root.unmount());
    root = createRoot(host);
    act(() => root.render(<Harness initial="beta" />));
    open();
    expect(document.activeElement).toBe(chip("#beta"));
    expect(document.activeElement!.getAttribute("aria-pressed")).toBe("true");
  });

  it("closes on Esc and puts focus back on the filter button", () => {
    act(() => root.render(<Harness />));
    open();
    expect(dialog()!.contains(document.activeElement)).toBe(true);
    key(document.activeElement!, "Escape");
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(trigger());
  });

  it("returns focus to the filter button after picking a tag, which then shows as current", () => {
    act(() => root.render(<Harness />));
    open();
    act(() => chip("#gamma").click());
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(trigger());
    expect(trigger().getAttribute("aria-label")).toBe("按标签筛选，当前 #gamma");

    // Clearing from the popover's own link also hands focus back.
    open();
    expect(document.activeElement).toBe(chip("#gamma"));
    act(() => [...dialog()!.querySelectorAll("button")].find((b) => b.textContent === "清除筛选")!.click());
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(trigger());
  });

  it("moves between the chips with the arrow keys, Home and End, wrapping at the ends", () => {
    act(() => root.render(<Harness />));
    open();
    const labels = () => (document.activeElement as HTMLElement).textContent;
    key(document.activeElement!, "ArrowRight");
    expect(labels()).toBe("#alpha");
    key(document.activeElement!, "ArrowDown");
    expect(labels()).toBe("#beta");
    key(document.activeElement!, "End");
    expect(labels()).toBe("#gamma");
    key(document.activeElement!, "ArrowRight");
    expect(labels()).toBe("全部");
    key(document.activeElement!, "ArrowLeft");
    expect(labels()).toBe("#gamma");
    key(document.activeElement!, "Home");
    expect(labels()).toBe("全部");
    key(document.activeElement!, "ArrowUp");
    expect(labels()).toBe("#gamma");
  });

  it("leaves focus where a click outside put it", () => {
    act(() => root.render(<Harness />));
    open();
    const after = [...host.querySelectorAll("button")].find((b) => b.textContent === "after")!;
    after.focus();
    act(() => { after.dispatchEvent(new MouseEvent("mousedown", { bubbles: true })); });
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(after);
  });
});
