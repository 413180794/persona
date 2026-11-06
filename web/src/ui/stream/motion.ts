// "Reply animation" (settings, on by default, kept in the browser) and the system's reduced motion: replies fade in word by
// word only when both allow it.
import { useSyncExternalStore } from "react";

const KEY = "persona.replyMotion";
const listeners = new Set<() => void>();
const REDUCE = "(prefers-reduced-motion: reduce)";

/** Whether "Reply animation" is on (the setting alone; see useReplyMotion for what applies). */
export function replyMotionEnabled(): boolean {
  try {
    return localStorage.getItem(KEY) !== "0";
  } catch {
    return true;
  }
}

export function setReplyMotionEnabled(on: boolean): void {
  try {
    if (on) localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, "0");
  } catch {
    // storage unavailable: nothing is kept, the default (on) stays
  }
  listeners.forEach((l) => l());
}

export const prefersReducedMotion = () => typeof matchMedia === "function" && matchMedia(REDUCE).matches;

/** How a scripted scroll moves: animated, or straight there under reduced motion (the CSS rule cannot reach scrollTo). */
export const scrollBehavior = (): ScrollBehavior => (prefersReducedMotion() ? "auto" : "smooth");

function subscribe(l: () => void) {
  listeners.add(l);
  const q = typeof matchMedia === "function" ? matchMedia(REDUCE) : null;
  q?.addEventListener("change", l);
  window.addEventListener("storage", l);
  return () => {
    listeners.delete(l);
    q?.removeEventListener("change", l);
    window.removeEventListener("storage", l);
  };
}

/** Whether replies fade in: "Reply animation" is on and the system doesn't ask for reduced motion. */
export function replyMotion(): boolean {
  return replyMotionEnabled() && !prefersReducedMotion();
}

/** The "Reply animation" setting for the settings page's switch. */
export function useReplyMotionSetting(): boolean {
  return useSyncExternalStore(subscribe, replyMotionEnabled, () => true);
}

/** Whether replies fade in now (the setting and reduced motion). */
export function useReplyMotion(): boolean {
  return useSyncExternalStore(subscribe, replyMotion, () => false);
}
