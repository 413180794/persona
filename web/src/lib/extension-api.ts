// The hub's extension routes across the plugin → extension rename (docs/extensions/extensions.md "Naming and compatibility", #1844, #1845):
// a hub that has them (hello feature `extensions`) answers under /api/v1/extensions/… with the keys extensions,
// extension and extension_name; an older one only under /api/v1/plugins/… with plugins, plugin and plugin_name. The web
// asks the new routes when the hub says so and reads either set of keys, so it works against both.
import { getState, hubHas } from "./store";

export const EXTENSIONS_FEATURE = "extensions";

/** Whether the hub has the new routes. Unknown (before hello) is no: the old routes answer on every hub. */
export function newRoutes(features: string[] = getState().features): boolean {
  return hubHas(features, EXTENSIONS_FEATURE, false);
}

/** The routes' root under /api/v1: "/extensions", or "/plugins" on an older hub. */
export function extRoot(features?: string[]): "/extensions" | "/plugins" {
  return newRoutes(features) ? "/extensions" : "/plugins";
}

/** Whether the hub has a capability under either name: extension_<name> (#1844), or plugin_<name> before it. */
export function hubHasExt(features: string[], name: string): boolean {
  return features.includes(`extension_${name}`) || features.includes(`plugin_${name}`);
}

/** A list the hub sent under `extensions` (new) or `plugins` (old). */
export function listOf<T>(r: { extensions?: T[]; plugins?: T[] } | null | undefined): T[] {
  return r?.extensions ?? r?.plugins ?? [];
}

/** The extension's id an event or a place names: `extension` (new) or `plugin` (old). */
export function extIdOf(r: { extension?: unknown; plugin?: unknown }): string {
  return typeof r.extension === "string" ? r.extension : typeof r.plugin === "string" ? r.plugin : "";
}
