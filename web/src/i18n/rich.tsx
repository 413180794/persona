import { Fragment, type ReactNode } from "react";
import { t, type Vars } from "./index";

/**
 * t() for text with elements inside: `{name}` placeholders whose value in `parts` is a node (a link, <b>, a <Kbd>)
 * render as that node; string and number values fill in as with t(). The sentence stays one entry, so each language
 * orders it its own way.
 */
export function tRich(key: string, parts: Record<string, ReactNode>): ReactNode[] {
  const plain: Vars = {};
  for (const [k, v] of Object.entries(parts)) if (typeof v === "string" || typeof v === "number") plain[k] = v;
  const text = t(key, plain);
  return text.split(/(\{\w+\})/g).map((piece, i) => {
    const m = /^\{(\w+)\}$/.exec(piece);
    return <Fragment key={i}>{m && m[1] in parts ? parts[m[1]] : piece}</Fragment>;
  });
}
