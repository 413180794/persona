import { useEffect, useState } from "react";
import { api, ApiError } from "./api";
import { extRoot } from "./extension-api";
import { EXTENSIONS_CHANGED, type QuickPrompt } from "./extension-prompts";
import { latestRequest, visiblePoll } from "./visible-poll";
import { onHubEvent, useStore } from "./store";

const EMPTY: never[] = [];

// Declarative lists refresh on reconnect, visibility and local extension changes, never while the page is hidden.
export function useDeclaredResult<T>(path: string, key: string, refreshMs = 0): { items: T[]; loaded: boolean } {
  const enabled = useStore((s) => s.features.includes("extensions") || s.features.includes("plugins"));
  const known = useStore((s) => s.conn === "online");
  const [items, setItems] = useState<T[]>([]);
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    if (!enabled) { setItems([]); setLoaded(false); return; }
    const request = latestRequest<Record<string, T[]>>(
      (r) => { setItems(Array.isArray(r[key]) ? r[key] : []); setLoaded(true); },
      (e) => { if (e instanceof ApiError && e.status === 404) { setItems([]); setLoaded(true); } },
    );
    const load = () => { if (document.visibilityState !== "hidden") void request.run(() => api<Record<string, T[]>>(`${extRoot()}/-/${path}`)); };
    const stopPoll = visiblePoll(load, refreshMs, {
      visible: () => document.visibilityState !== "hidden",
      subscribe: (fn) => { document.addEventListener("visibilitychange", fn); return () => document.removeEventListener("visibilitychange", fn); },
    });
    const off = onHubEvent("hello", load);
    window.addEventListener(EXTENSIONS_CHANGED, load);
    return () => { request.dispose(); off(); stopPoll(); window.removeEventListener(EXTENSIONS_CHANGED, load); };
  }, [enabled, path, key, refreshMs]);
  return { items: enabled ? items : EMPTY, loaded: enabled ? loaded : known };
}

export function useDeclaredList<T>(path: string, key: string): T[] { return useDeclaredResult<T>(path, key).items; }
export function useQuickPrompts(): QuickPrompt[] { return useDeclaredList<QuickPrompt>("prompts", "prompts"); }
