// Phones: the soft keyboard and touch input. iOS Safari does not shrink the page for the keyboard (the visual viewport
// does, the layout one stays), so the shell sizes itself to what is visible: the composer rides on the keyboard and
// the chat above it is not hidden under it. Chrome on Android resizes the page itself (`interactive-widget` in
// index.html), and 100dvh is enough there.
import { useEffect, type RefObject } from "react";

const KEYBOARD_MIN = 120;

export interface ViewportFrame {
  top: number;
  height: number;
}

/** Where the visible part of the page is while something (the keyboard) covers its bottom; undefined when nothing
 *  does, or the page is pinch-zoomed (the visual viewport is small then too, and following it would jump around). */
export function keyboardFrame(innerHeight: number, vv: { height: number; offsetTop: number; scale: number } | null | undefined): ViewportFrame | undefined {
  if (!vv || Math.abs(vv.scale - 1) > 0.01) return undefined;
  // a scrollbar or a toolbar sliding away is not a keyboard
  if (innerHeight - vv.height < KEYBOARD_MIN) return undefined;
  return { top: Math.max(0, vv.offsetTop), height: Math.round(vv.height) };
}

/** Keeps `ref`'s element over the visible part while the keyboard is up. It sets the element's style itself: the
 *  keyboard sends a stream of frames as it slides, and a React state per frame would re-render the whole page. */
export function useKeyboardFrame(ref: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const vv = typeof window === "undefined" ? undefined : window.visualViewport;
    if (!vv) return;
    const update = () => {
      const el = ref.current;
      if (!el) return;
      const f = keyboardFrame(window.innerHeight, vv);
      el.style.position = f ? "fixed" : "";
      el.style.insetInline = f ? "0" : "";
      el.style.top = f ? `${f.top}px` : "";
      el.style.height = f ? `${f.height}px` : "";
    };
    update();
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, [ref]);
}

/** A coarse pointer (a finger): the composer's Enter makes a new line there and only its button sends. */
export function isCoarsePointer(): boolean {
  return typeof matchMedia === "function" && matchMedia("(pointer: coarse)").matches;
}

/** Whether Enter (without Shift) sends: on a keyboard with a pointer, yes; on a touch screen it is a new line. */
export function enterSends(e: { key: string; shiftKey: boolean }, coarse: boolean): boolean {
  return e.key === "Enter" && !e.shiftKey && !coarse;
}
