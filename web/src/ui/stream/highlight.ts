// Code highlighting (rehype-highlight, highlight.js 11's common languages), loaded the first time a reply has a fenced
// block with a language: the main bundle doesn't carry the grammars. Until it is there blocks show plain.
import { useEffect, useSyncExternalStore } from "react";
import type { Root } from "hast";

type Plugin = () => (tree: Root) => void;

let plugin: Plugin | null = null;
let loading: Promise<Plugin> | null = null;
const listeners = new Set<() => void>();

/** Loads the highlighter (once); resolves with the rehype plugin. */
export function loadHighlight(): Promise<Plugin> {
  loading ??= import("rehype-highlight").then((m) => {
    plugin = m.default as unknown as Plugin;
    listeners.forEach((l) => l());
    return plugin;
  });
  return loading;
}

/** A fenced block naming a language (```ts): only those are highlighted (no guessing). */
export const hasLanguageFence = (md: string) => /^[ \t>]*(?:`{3,}|~{3,})[ \t]*[\w+#.-]/m.test(md);

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

/** The highlight plugin once loaded; asks for it when `wanted`. */
export function useHighlight(wanted: boolean): Plugin | null {
  const p = useSyncExternalStore(subscribe, () => plugin, () => plugin);
  useEffect(() => {
    if (wanted && !p) void loadHighlight().catch(() => {});
  }, [wanted, p]);
  return p;
}
