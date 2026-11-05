// @vitest-environment jsdom
// useModalFocus (overlay.ts): a modal takes the focus when it opens, keeps Tab inside, and gives the focus back on close.
import { act, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useModalFocus } from "./overlay";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let opener: HTMLButtonElement;

beforeEach(() => {
  // focusables() keeps only shown controls; jsdom lays nothing out, so every element would count as hidden
  vi.spyOn(Element.prototype, "getClientRects").mockReturnValue([{}] as unknown as DOMRectList);
  opener = document.createElement("button");
  opener.textContent = "open";
  host = document.createElement("div");
  document.body.append(opener, host);
  root = createRoot(host);
  opener.focus();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  opener.remove();
  vi.restoreAllMocks();
});

function Modal({ open = true, controls = 2 }: { open?: boolean; controls?: number }) {
  const ref = useRef<HTMLDivElement>(null);
  useModalFocus(ref, open);
  return (
    <div ref={ref} role="dialog" aria-modal="true" tabIndex={-1} data-testid="modal">
      {Array.from({ length: controls }, (_, i) => (
        <button key={i} type="button">
          b{i}
        </button>
      ))}
    </div>
  );
}

const modal = () => host.querySelector<HTMLElement>("[data-testid=modal]")!;
const buttons = () => [...modal().querySelectorAll("button")];
/** Presses Tab on the focused element; returns whether the hook took it (the browser would move on otherwise). */
function tab(shift = false): boolean {
  const e = new KeyboardEvent("keydown", { key: "Tab", shiftKey: shift, bubbles: true, cancelable: true });
  (document.activeElement ?? document.body).dispatchEvent(e);
  return e.defaultPrevented;
}

describe("useModalFocus", () => {
  it("moves the focus into the modal when it opens", () => {
    act(() => root.render(<Modal />));
    expect(document.activeElement).toBe(modal());
  });

  it("leaves the focus alone while closed", () => {
    act(() => root.render(<Modal open={false} />));
    expect(document.activeElement).toBe(opener);
  });

  it("wraps Tab and Shift+Tab among the modal's controls", () => {
    act(() => root.render(<Modal />));
    const [first, last] = buttons();
    expect(tab()).toBe(true); // from the modal itself to its first control
    expect(document.activeElement).toBe(first);
    expect(tab(true)).toBe(true); // Shift+Tab on the first wraps to the last
    expect(document.activeElement).toBe(last);
    expect(tab()).toBe(true); // Tab on the last wraps to the first
    expect(document.activeElement).toBe(first);
    expect(tab()).toBe(false); // between two controls the browser's own order is already right
  });

  it("keeps the focus on the modal when it has no controls", () => {
    act(() => root.render(<Modal controls={0} />));
    expect(tab()).toBe(true);
    expect(document.activeElement).toBe(modal());
    expect(tab(true)).toBe(true);
    expect(document.activeElement).toBe(modal());
  });

  it("gives the focus back to what had it when the modal unmounts", () => {
    act(() => root.render(<Modal />));
    expect(document.activeElement).not.toBe(opener);
    act(() => root.render(<></>));
    expect(document.activeElement).toBe(opener);
  });

  it("leaves the focus where the close put it when that is outside the modal", () => {
    // e.g. the image viewer's "add to composer": attaching focuses the composer's input, then the viewer unmounts
    const input = document.createElement("input");
    document.body.append(input);
    act(() => root.render(<Modal />));
    act(() => {
      input.focus();
      root.render(<></>);
    });
    expect(document.activeElement).toBe(input);
    input.remove();
  });

  it("gives the focus back when open turns false", () => {
    act(() => root.render(<Modal />));
    act(() => root.render(<Modal open={false} />));
    expect(document.activeElement).toBe(opener);
  });
});
