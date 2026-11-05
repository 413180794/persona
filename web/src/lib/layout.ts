// Which columns of the shell are shown on a wide screen (the shortcuts ⌘⌥B and ⌘⇧↵, docs/web.md「快捷键」). A narrow
// screen has a drawer and a full-screen panel instead; these do nothing there.
//
// The sidebar folds to a rail of icons (#2524), not away: the rail keeps the pages one click off and its own button to
// open it again, so the choice is kept in this browser (a reload keeps it) without leaving anyone stuck without a
// sidebar. Maximizing (⌘⇧↵) still hides it outright, for this page only.
import { useSyncExternalStore } from "react";

export interface Layout {
  /** The sidebar column is folded to a rail of icons (⌘⌥B, the button at its top), kept in this browser. */
  sidebarCollapsed: boolean;
  /** Only the middle is shown: the sidebar column and the right panel are hidden (⌘⇧↵). */
  maximized: boolean;
}

export const COLLAPSED_KEY = "persona.sidebarCollapsed";

function stored(): boolean {
  try {
    return typeof localStorage !== "undefined" && localStorage.getItem(COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

let layout: Layout = { sidebarCollapsed: stored(), maximized: false };
const subscribers = new Set<() => void>();

function set(next: Layout) {
  layout = next;
  subscribers.forEach((f) => f());
}

export const getLayout = () => layout;

export function setSidebarCollapsed(collapsed: boolean) {
  try {
    if (collapsed) localStorage.setItem(COLLAPSED_KEY, "1");
    else localStorage.removeItem(COLLAPSED_KEY);
  } catch {
    // storage refused (private mode): the choice lasts this page only
  }
  set({ ...layout, sidebarCollapsed: collapsed });
}

export const toggleSidebarColumn = () => setSidebarCollapsed(!layout.sidebarCollapsed);
export const toggleMaximized = () => set({ ...layout, maximized: !layout.maximized });
export const resetLayout = () => set({ sidebarCollapsed: stored(), maximized: false });

// another tab folding or opening it
if (typeof addEventListener !== "undefined") {
  addEventListener("storage", (e) => {
    if ((e.key === COLLAPSED_KEY || e.key === null) && stored() !== layout.sidebarCollapsed) set({ ...layout, sidebarCollapsed: stored() });
  });
}

export function useLayout(): Layout {
  return useSyncExternalStore(
    (f) => {
      subscribers.add(f);
      return () => void subscribers.delete(f);
    },
    getLayout,
    getLayout,
  );
}

/** Whether the sidebar column is drawn on a wide screen (full or as the rail). */
export const sidebarShown = (l: Layout) => !l.maximized;

/** Whether a wide screen draws the rail of icons instead of the full sidebar. */
export const sidebarRail = (l: Layout) => l.sidebarCollapsed && !l.maximized;
