// Imported by main.tsx before App: App's module takes a `#token=` out of the address bar as soon as it loads (a sign-in
// link), so the live screen's has to be read first (screen.ts).
import { screenTarget, type ScreenTarget } from "./screen";

function take(): ScreenTarget | null {
  if (typeof window === "undefined") return null;
  const { target, rest } = screenTarget(window.location.href);
  if (target) window.history.replaceState(window.history.state, "", rest); // the app's token leaves the address bar
  return target;
}

export const screen = take();
