// Settings › Appearance › Show terminals in sidebar: under each machine of the sidebar's "By machine" grouping, the plain shells of its terminal
// host (the agents are listed there already). Off unless turned on; kept by this browser.
import { useSyncExternalStore } from "react";

const KEY = "sidebar.terminals";
const listeners = new Set<() => void>();

export function getSidebarTerminals(): boolean {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

export function setSidebarTerminals(on: boolean): void {
  try {
    if (on) localStorage.setItem(KEY, "1");
    else localStorage.removeItem(KEY);
  } catch {
    // storage unavailable: nothing is kept, the default (off) stays
  }
  listeners.forEach((l) => l());
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  if (typeof window !== "undefined") window.addEventListener("storage", l);
  return () => {
    listeners.delete(l);
    if (typeof window !== "undefined") window.removeEventListener("storage", l);
  };
};

export function useSidebarTerminals(): boolean {
  return useSyncExternalStore(subscribe, getSidebarTerminals, () => false);
}
