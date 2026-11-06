// `import tokens from "./styles.css?page-tokens"` gives the stylesheet's page color tokens without shipping the
// stylesheet: { light, dark }, the `--name: #hex` pairs of its first `:root {}` and first `.dark {}` block. The look
// code (src/ui/extension-theme.ts pageToken) runs before the first paint, so its chunk is preloaded; inlining the
// whole stylesheet as a string there cost about 7 KiB gzipped, and the same CSS already ships as a .css file.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import type { Plugin } from "vite";

export interface PageTokens {
  light: Record<string, string>;
  dark: Record<string, string>;
}

// Only hex values count: a token set from another one (var(--x)) or a non-color is left out, and the first
// declaration of a name in the block wins.
function hexTokens(block: string): Record<string, string> {
  const tokens: Record<string, string> = {};
  for (const [, name, value] of block.matchAll(/--([\w-]+):\s*(#[0-9a-f]+)/gi)) tokens[name] ??= value;
  return tokens;
}

export function pageTokens(css: string): PageTokens {
  return {
    light: hexTokens(/:root\s*\{([^}]+)\}/.exec(css)?.[1] ?? ""),
    dark: hexTokens(/\.dark\s*\{([^}]+)\}/.exec(css)?.[1] ?? ""),
  };
}

const PREFIX = "\0page-tokens:";

export const pageTokensPlugin: Plugin = {
  name: "page-tokens",
  enforce: "pre",
  resolveId(source, importer) {
    const [path, query] = source.split("?");
    if (query !== "page-tokens" || !path.endsWith(".css")) return;
    const absolute = path.startsWith(".") && importer ? join(dirname(importer.split("?")[0]), path) : path;
    return `${PREFIX}${absolute}.js`;
  },
  load(id) {
    if (!id.startsWith(PREFIX)) return;
    const path = id.slice(PREFIX.length, -".js".length);
    this.addWatchFile(path);
    return `export default ${JSON.stringify(pageTokens(readFileSync(path, "utf8")))};`;
  },
};
