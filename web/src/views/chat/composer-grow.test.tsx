// @vitest-environment jsdom
// The phone composer grows to a line limit, then offers Expand (#5688): the pure limit, and the box's four states
// (growing, the button, the full editor, collapsed with the same text and caret), with desktop left as it was.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Composer } from "./Composer";
import { growLines, lineCount, offersExpand } from "./composer-grow";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe("growLines", () => {
  it("takes 22% of the visible height, between 3 and 8 lines", () => {
    expect(growLines(360, 24)).toBe(3); // a small phone with its keyboard up
    expect(growLines(760, 24)).toBe(6); // a tall phone without it
    expect(growLines(3000, 24)).toBe(8);
    expect(growLines(0, 24)).toBe(3);
    expect(growLines(800, 0)).toBe(3);
  });
  it("counts wrapped lines and offers Expand only past the limit", () => {
    expect(lineCount(72, 24)).toBe(3);
    expect(lineCount(0, 24)).toBe(1);
    expect(offersExpand(3, 3)).toBe(false);
    expect(offersExpand(4, 3)).toBe(true);
  });
});

// jsdom lays nothing out: a line per "\n", 24px each (the computed line height falls back to 24 there too)
const scroll = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollHeight");
let phone = true;
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  phone = true;
  localStorage.clear();
  Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", {
    configurable: true,
    get(this: HTMLTextAreaElement) { return this.value.split("\n").length * 24; },
  });
  vi.stubGlobal("matchMedia", (q: string) => ({
    media: q,
    get matches() { return q.includes("max-width: 639px") ? phone : false; },
    addEventListener() {},
    removeEventListener() {},
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  if (scroll) Object.defineProperty(HTMLTextAreaElement.prototype, "scrollHeight", scroll);
  else delete (HTMLTextAreaElement.prototype as { scrollHeight?: number }).scrollHeight;
  vi.unstubAllGlobals();
});

const lines = (n: number) => Array.from({ length: n }, (_, i) => `step ${i + 1}`).join("\n");
const box = () => host.querySelector<HTMLTextAreaElement>("textarea")!;
const type = (value: string) =>
  act(() => {
    const el = box();
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
const expandButton = () => host.querySelector<HTMLButtonElement>('[data-testid="composer-expand"]');
const editor = () => document.querySelector<HTMLElement>('[data-testid="composer-expanded"]');

describe("the phone composer", () => {
  it("grows without a button, offers Expand past the limit, and keeps the draft and caret across the full editor", () => {
    act(() => root.render(<Composer draftKey="k1" placeholder="" onSend={async () => true} />));
    // jsdom's window is 768px tall: a limit of 7 lines
    type(lines(4));
    expect(expandButton()).toBeNull();
    expect(box().className).not.toContain("pr-8");
    expect(box().style.maxHeight).toBe(`${7 * 24}px`);

    type(lines(9));
    expect(expandButton()).not.toBeNull();
    expect(box().className).toContain("pr-8");

    box().setSelectionRange(5, 9);
    act(() => expandButton()!.click());
    const big = editor()!.querySelector("textarea")!;
    expect(big.value).toBe(lines(9));
    expect([big.selectionStart, big.selectionEnd]).toEqual([5, 9]);
    expect(document.activeElement).toBe(big);

    big.setSelectionRange(12, 12);
    act(() => document.querySelector<HTMLButtonElement>('[data-testid="composer-collapse"]')!.click());
    expect(editor()).toBeNull();
    expect(box().value).toBe(lines(9));
    expect([box().selectionStart, box().selectionEnd]).toEqual([12, 12]);
    expect(document.activeElement).toBe(box());
    expect(expandButton()).not.toBeNull();

    type(lines(2));
    expect(expandButton()).toBeNull();
  });

  it("sends from the full editor and closes it", async () => {
    const sent: string[] = [];
    act(() => root.render(<Composer draftKey="k2" placeholder="" onSend={async (m) => (sent.push(m.text), true)} />));
    type(lines(9));
    act(() => expandButton()!.click());
    await act(async () => editor()!.querySelector<HTMLButtonElement>('[data-testid="composer-expanded-send"]')!.click());
    expect(sent).toEqual([lines(9)]);
    expect(editor()).toBeNull();
  });

  it("leaves the desktop box as it was: no button, its own height cap", () => {
    phone = false;
    act(() => root.render(<Composer draftKey="k3" placeholder="" onSend={async () => true} />));
    type(lines(12));
    expect(expandButton()).toBeNull();
    expect(box().style.maxHeight).toBe("min(240px, calc(var(--kb-h) * 0.35))");
  });
});
