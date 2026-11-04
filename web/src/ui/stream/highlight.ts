// Code highlighting (rehype-highlight, highlight.js 11's common languages), loaded the first time a reply has a fenced
// block with a language: the main bundle doesn't carry the grammars. Until it is there blocks show plain.
import type { Root } from "hast";
import { lazyPlugin } from "./lazy-plugin";

type Plugin = () => (tree: Root) => void;

const highlight = lazyPlugin<Plugin>(() => import("rehype-highlight").then((m) => m.default as unknown as Plugin));

/** Loads the highlighter (once); resolves with the rehype plugin. */
export const loadHighlight = highlight.load;

/** A fenced block naming a language (```ts): only those are highlighted (no guessing). */
export const hasLanguageFence = (md: string) => /^[ \t>]*(?:`{3,}|~{3,})[ \t]*[\w+#.-]/m.test(md);

/** The highlight plugin once loaded; asks for it when `wanted`. */
export const useHighlight = highlight.use;
