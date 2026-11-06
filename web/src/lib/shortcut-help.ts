// Whether the shortcut help overlay (⌘/, views/ShortcutHelp.tsx) is open.
import { useSyncExternalStore } from "react";

let open = false;
const subscribers = new Set<() => void>();

function set(next: boolean) {
  if (next === open) return;
  open = next;
  subscribers.forEach((f) => f());
}

export const closeHelp = () => set(false);
export const toggleHelp = () => set(!open);

export function useHelpOpen(): boolean {
  return useSyncExternalStore(
    (f) => {
      subscribers.add(f);
      return () => void subscribers.delete(f);
    },
    () => open,
    () => open,
  );
}

/** Settings › Keyboard shortcuts (also when Settings is the page already: its category follows the address). */
export function openShortcutSettings(go: (to: string) => void) {
  go("/settings?tab=shortcuts");
  dispatchEvent(new PopStateEvent("popstate"));
}
