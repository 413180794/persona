// A rehype plugin kept out of the main bundle (highlight.ts, math.ts): loaded the first time a reply needs it, then
// shared by every reply. A failed chunk (the hub was redeployed under the page) is asked for again by the next one.
import { useEffect, useSyncExternalStore } from "react";

export interface LazyPlugin<P> {
  /** Loads it (once); resolves with the plugin. */
  load: () => Promise<P>;
  /** The plugin once loaded, when `wanted` (asking for it then); null otherwise, so a reply that has no use for it is not
   *  drawn again when it arrives. */
  use: (wanted: boolean) => P | null;
}

export function lazyPlugin<P>(importer: () => Promise<P>): LazyPlugin<P> {
  let plugin: P | null = null;
  let loading: Promise<P> | null = null;
  const listeners = new Set<() => void>();
  const subscribe = (l: () => void) => {
    listeners.add(l);
    return () => listeners.delete(l);
  };
  const load = () =>
    (loading ??= importer().then(
      (p) => {
        plugin = p;
        listeners.forEach((l) => l());
        return p;
      },
      (e) => {
        loading = null;
        throw e;
      },
    ));
  function use(wanted: boolean): P | null {
    const p = useSyncExternalStore(subscribe, () => (wanted ? plugin : null), () => (wanted ? plugin : null));
    useEffect(() => {
      if (wanted && !p) void load().catch(() => {});
    }, [wanted, p]);
    return p;
  }
  return { load, use };
}
