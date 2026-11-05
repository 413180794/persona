// Which columns of the shell are shown on a wide screen (the shortcuts ⌘⌥B and ⌘⇧↵, docs/web.md「快捷键」). Kept for the
// session only: a reload brings the sidebar back, so nobody is left without it by a key they forgot. A narrow screen has
// a drawer and a full-screen panel instead; these do nothing there.
import { useSyncExternalStore } from "react";

export interface Layout {
  /** The sidebar column is hidden (⌘⌥B). */
  sidebarHidden: boolean;
  /** Only the middle is shown: the sidebar column and the right panel are hidden (⌘⇧↵). */
  maximized: boolean;
}

let layout: Layout = { sidebarHidden: false, maximized: false };
const subscribers = new Set<() => void>();

function set(next: Layout) {
  layout = next;
  subscribers.forEach((f) => f());
}

export const getLayout = () => layout;
export const toggleSidebarColumn = () => set({ ...layout, sidebarHidden: !layout.sidebarHidden });
export const toggleMaximized = () => set({ ...layout, maximized: !layout.maximized });
export const resetLayout = () => set({ sidebarHidden: false, maximized: false });

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

/** Whether the sidebar column is drawn on a wide screen. */
export const sidebarShown = (l: Layout) => !l.sidebarHidden && !l.maximized;
