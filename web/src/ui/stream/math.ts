// Formulas (rehype-katex, KaTeX's CSS and fonts, mhchem's \ce / \pu, copy-tex), loaded the first time a reply has one:
// the main bundle doesn't carry KaTeX (#1996). Until it is there a formula shows as its TeX, like highlight.ts's blocks.
import type { Pluggable } from "unified";
import { lazyPlugin } from "./lazy-plugin";

const math = lazyPlugin<Pluggable>(() =>
  Promise.all([
    import("rehype-katex"),
    import("katex/dist/katex.min.css"),
    // registers its macros on the katex that rehype-katex uses
    import("katex/contrib/mhchem"),
    // copying a selection with formulas puts their LaTeX ($…$ / $$…$$) on the clipboard, not KaTeX's glyphs; it touches
    // `document` on load, so not without one (the tests)
    typeof document === "undefined" ? null : import("katex/contrib/copy-tex"),
  ]).then(([m]) => m.default as Pluggable),
);

/** Loads KaTeX (once); resolves with the rehype plugin. */
export const loadMath = math.load;
/** The KaTeX plugin once loaded; asks for it when `wanted`. */
export const useMath = math.use;
