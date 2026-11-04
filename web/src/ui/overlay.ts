// The pure parts of the overlays in atoms.tsx (#1996): where Tab goes in a dialog, where a menu sits on screen.
import { NARROW, PHONE, useMedia } from "../lib/viewport";

/** A phone-width screen: Dialog is a bottom sheet, 设置 a list of categories (Tailwind's max-sm). */
export const SHEET_QUERY = PHONE;
/** Narrower than the sidebar's breakpoint (lg): a menu keeps to the visible viewport above the keyboard. */
export const NARROW_QUERY = NARROW;
export { useMedia };

/** Tab inside a modal: the index to move focus to so it stays in, or null where the browser's own order already does
 *  (between two of its controls). `current` is -1 when the focus is not on one of the `count` controls. */
export function trapTab(count: number, current: number, back: boolean): number | null {
  if (count === 0) return -1; // nothing to land on: the dialog itself keeps it
  if (current < 0) return back ? count - 1 : 0;
  if (back && current === 0) return count - 1;
  if (!back && current === count - 1) return 0;
  return null;
}

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable="true"]';

/** The controls Tab can reach in `root`, in document order (shown and not inert). */
export function focusables(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => el.getClientRects().length > 0 && !el.closest("[inert]"));
}

export interface MenuPlace {
  left: number;
  top: number;
  width: number;
  /** Set only when the visible viewport is shorter than the menu's own cap (a phone with the keyboard up). */
  maxHeight?: number;
}

/** Where a menu of height `h` opens next to `anchor`, kept 8px inside the area it may use: the window, or on a phone
 *  the visible viewport (`view`: its top and height, from --kb-top / --kb-h) the soft keyboard leaves. */
export function placeMenu(
  anchor: { left: number; right: number; top: number; bottom: number },
  h: number,
  o: { width: number; align: "left" | "right"; side: "top" | "bottom"; screenW: number; screenH: number; view?: { top: number; height: number } },
): MenuPlace {
  const width = Math.min(o.width, o.screenW - 16);
  const left = Math.max(8, Math.min(o.align === "left" ? anchor.left : anchor.right - width, o.screenW - width - 8));
  const lo = (o.view?.top ?? 0) + 8;
  const hi = o.view ? o.view.top + o.view.height - 8 : o.screenH - 8;
  const maxHeight = o.view ? Math.max(0, hi - lo) : undefined;
  const fit = Math.min(h, maxHeight ?? h);
  let top = o.side === "bottom" ? anchor.bottom + 6 : anchor.top - fit - 6;
  if (o.side === "bottom" && top + fit > hi) top = anchor.top - fit - 6;
  if (top < lo) top = lo;
  if (o.view && top + fit > hi) top = Math.max(lo, hi - fit);
  return maxHeight === undefined ? { left, top, width } : { left, top, width, maxHeight };
}
