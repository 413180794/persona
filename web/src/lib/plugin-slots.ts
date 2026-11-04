// The plugins' places outside their settings (docs/plugins.md「UI 插槽」): entries on the sidebar (nav_page), cards on
// 总览 (overview_card), sections on a machine's page (machine_section), inbox items' pages (inbox_item: the list is
// cached here, so opening an item asks nothing). One list for the whole web, asked for once
// (GET /api/v1/plugins/-/ui without a placement) and again on reconnect, when a plugin changes here, and when the tab
// comes back after a while; a nav_page's badge changes in place on the plugin_ui event. Only on a hub with
// plugin_ui_slots: an older one is never asked (it would answer an empty list anyway).
import { useMemo, useSyncExternalStore } from "react";
import { applyBadge, inboxItemPlace, placesOf, uiPlaces, type BadgeEvent, type SlotName, type UIPlace } from "./plugin-ui";
import { isResumed } from "./reconnect";
import { getState, onHubEvent, useStore } from "./store";

export const SLOTS_FEATURE = "plugin_ui_slots";

let places: UIPlace[] = [];
let asked = 0; // when the list was last asked for
let generation = 0;
let started = false;
let helloVersion = ""; // the hub version the last hello named
const listeners = new Set<() => void>();

function set(next: UIPlace[]) {
  if (next === places) return;
  places = next;
  listeners.forEach((l) => l());
}

/** Asks the hub again (a plugin turned on or off, installed, upgraded…). */
export function refreshPluginPlaces() {
  const g = ++generation;
  if (!getState().features.includes(SLOTS_FEATURE)) {
    set(places.length ? [] : places);
    return;
  }
  asked = Date.now();
  uiPlaces("").then(
    (list) => g === generation && set(list),
    () => {}, // keep what is shown; the next reconnect asks again
  );
}

function start() {
  if (started || typeof window === "undefined") return;
  started = true;
  onHubEvent("hello", (msg) => {
    // resumed: missed plugin_ui pushes are replayed; but a hub that came back as another version may run other plugins
    const version = String(msg.server_version ?? "");
    const same = version === helloVersion;
    helloVersion = version;
    if (!isResumed(msg) || !same) refreshPluginPlaces();
  });
  onHubEvent("plugin_ui", (msg) => set(applyBadge(places, msg as unknown as BadgeEvent)));
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && Date.now() - asked > 60_000) refreshPluginPlaces();
  });
  helloVersion = getState().version;
  if (getState().conn === "online") refreshPluginPlaces();
}

const subscribe = (l: () => void) => {
  start();
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
};

/** The places of one slot, in the hub's order; none on a hub without plugin_ui_slots. */
export function usePluginSlot(placement: SlotName): UIPlace[] {
  const on = useStore((s) => s.features.includes(SLOTS_FEATURE));
  const all = useSyncExternalStore(subscribe, () => places);
  return useMemo(() => (on ? placesOf(all, placement) : []), [on, all, placement]);
}

/** One place by plugin and id (a page opened from the sidebar or a card), if the hub listed it. */
export function usePluginPlace(plugin: string, id: string): UIPlace | undefined {
  const all = useSyncExternalStore(subscribe, () => places);
  return useMemo(() => all.find((u) => u.plugin === plugin && u.id === id), [all, plugin, id]);
}

/**
 * The plugin page that draws the inbox items of `source` (an inbox_item of the plugin whose connector it is), if the hub
 * lists one; none on a hub without the slot (it lists none), and the item keeps its own detail.
 */
export function useInboxItemPlace(source?: string): UIPlace | undefined {
  const items = usePluginSlot("inbox_item");
  return useMemo(() => inboxItemPlace(items, source), [items, source]);
}
