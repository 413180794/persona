// What a click on a notification the service worker showed does (sw.ts notificationclick; lib/notify.ts
// postNotification has the worker show one where the page can't make its own). Kept apart from sw.ts so it is tested.

/** The few parts of the worker's Clients and WindowClient this uses. */
export interface ClickWindow {
  focused?: boolean;
  visibilityState?: string;
  focus(): Promise<unknown>;
  navigate(url: string): Promise<unknown | null>;
}
export interface ClickClients {
  matchAll(o: { type: "window"; includeUncontrolled: boolean }): Promise<ClickWindow[]>;
  openWindow(url: string): Promise<unknown | null>;
}

/**
 * Brings a window of the app forward on `url` (the agent's conversation). Only a window this worker controls can be
 * navigated, so those are the ones looked at, a focused or visible one first. When there is none, or it can't be
 * navigated, a new window opens on `url`. Without a url, an open window comes forward.
 */
export async function openFromNotification(clients: ClickClients, url: string | undefined): Promise<void> {
  const wins = await clients.matchAll({ type: "window", includeUncontrolled: false });
  const rank = (w: ClickWindow) => (w.focused ? 2 : w.visibilityState === "visible" ? 1 : 0);
  const win = [...wins].sort((a, b) => rank(b) - rank(a))[0];
  if (!url) {
    if (win) await win.focus().catch(() => null);
    else await clients.openWindow("/").catch(() => null);
    return;
  }
  if (win) {
    await win.focus().catch(() => null);
    const went = await win.navigate(url).catch(() => null);
    if (went) return;
  }
  await clients.openWindow(url).catch(() => null);
}
