import { useEffect, useState } from "react";
import { api, ApiError } from "./api";
import { extRoot } from "./extension-api";
import { EXTENSIONS_CHANGED, type QuickPrompt } from "./extension-prompts";
import { onHubEvent, useStore } from "./store";

const EMPTY: never[] = [];

// Declarative lists refresh on reconnect, visibility and local extension changes, never while the page is hidden.
export function useDeclaredResult<T>(path: string, key: string): { items: T[]; loaded: boolean } {
  const enabled = useStore((s) => s.features.includes("extensions") || s.features.includes("plugins"));
  const known = useStore((s) => s.conn === "online");
  const [items, setItems] = useState<T[]>([]);
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    if (!enabled) { setItems([]); setLoaded(false); return; }
    let live = true, generation = 0;
    const load = () => {
      if (document.visibilityState === "hidden") return;
      const n = ++generation;
      void api<Record<string, T[]>>(`${extRoot()}/-/${path}`).then(
        (r) => { if (live && n === generation) { setItems(Array.isArray(r[key]) ? r[key] : []); setLoaded(true); } },
        (e) => { if (live && n === generation && e instanceof ApiError && e.status === 404) { setItems([]); setLoaded(true); } },
      );
    };
    load();
    const off = onHubEvent("hello", load);
    document.addEventListener("visibilitychange", load);
    window.addEventListener(EXTENSIONS_CHANGED, load);
    return () => { live = false; off(); document.removeEventListener("visibilitychange", load); window.removeEventListener(EXTENSIONS_CHANGED, load); };
  }, [enabled, path, key]);
  return { items: enabled ? items : EMPTY, loaded: enabled ? loaded : known };
}

export function useDeclaredList<T>(path: string, key: string): T[] { return useDeclaredResult<T>(path, key).items; }
export function useQuickPrompts(): QuickPrompt[] { return useDeclaredList<QuickPrompt>("prompts", "prompts"); }
