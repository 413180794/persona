import { useEffect, useState } from "react";
import { api } from "./api";
import { extRoot } from "./extension-api";
import { EXTENSIONS_CHANGED, type QuickPrompt } from "./extension-prompts";
import { onHubEvent, useStore } from "./store";

// Declarative lists refresh on reconnect, visibility and local extension changes, never while the page is hidden.
export function useDeclaredList<T>(path: string, key: string): T[] {
  const enabled = useStore((s) => s.features.includes("extensions") || s.features.includes("plugins"));
  const [items, setItems] = useState<T[]>([]);
  useEffect(() => {
    if (!enabled) { setItems([]); return; }
    let live = true, generation = 0;
    const load = () => {
      if (document.visibilityState === "hidden") return;
      const n = ++generation;
      void api<Record<string, T[]>>(`${extRoot()}/-/${path}`).then(
        (r) => live && n === generation && setItems(Array.isArray(r[key]) ? r[key] : []),
        () => {}, // Older hubs have no list; temporary errors do not clear existing drafts or a selected theme.
      );
    };
    load();
    const off = onHubEvent("hello", load);
    document.addEventListener("visibilitychange", load);
    window.addEventListener(EXTENSIONS_CHANGED, load);
    return () => { live = false; off(); document.removeEventListener("visibilitychange", load); window.removeEventListener(EXTENSIONS_CHANGED, load); };
  }, [enabled, path, key]);
  return enabled ? items : [];
}

export function useQuickPrompts(): QuickPrompt[] {
  return useDeclaredList<QuickPrompt>("prompts", "prompts");
}
