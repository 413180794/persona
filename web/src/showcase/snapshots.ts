// The component showcase extension's pages (schema/extension/examples/showcase, copied from the extension's snapshots/, #1852):
// the views every app draws for its screenshots, so the four apps can be compared page by page.
import type { UIView } from "../lib/extension-ui";
import { readView } from "../lib/extension-ui";

const files = import.meta.glob("../../../schema/extension/examples/showcase/*.json", { eager: true, import: "default" });

/** Page name (the file's name) → its view, in the files' order. */
export const SHOWCASE: Record<string, UIView> = Object.fromEntries(
  Object.entries(files)
    .map(([path, raw]) => [path.split("/").pop()!.replace(/\.json$/, ""), readView(raw)] as const)
    .sort(([a], [b]) => a.localeCompare(b)),
);

/** How the page is drawn: a card on "Overview", a widget (small as a card), or a full page. */
export function showcaseKind(name: string): "card" | "widget" | "page" {
  if (name === "card") return "card";
  if (name.startsWith("widget-") || name.startsWith("shortcut-")) return "widget";
  return "page";
}
