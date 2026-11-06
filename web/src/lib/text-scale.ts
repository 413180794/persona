// Text size (ui/look.ts): every font size, and every line height given in pixels (leading-[Npx], and Tailwind's leading-4 / leading-5 as calc(var(--spacing) * N)), in
// --text-scale, set only for "Small" and "Large" (styles.css). Unset, calc(12.5px * 1) is 12.5px: the page at "Standard" is what it was.
// Sizes in em / % and keywords follow their parent already and stay as they are. The page's sizes are pixels written
// into Tailwind classes (text-[12.5px]) all over, so the build rewrites the CSS (vite.config.ts) rather than each view.
// Scaling the whole page with CSS zoom was the other way: it moves every popover placed from getBoundingClientRect.

const SCALABLE = /^(?:-?\d*\.?\d+(?:px|rem)|var\(--[\w-]+\))$/;
const PX = /^-?\d*\.?\d+px$/;
/** Tailwind v4's leading-4 / leading-5: a fixed length too, written as a multiple of the spacing unit. */
const SPACING = /^calc\(var\(--spacing\)\s*\*\s*[\d.]+\)$/;

/** The declaration's new value, or null to leave it. */
export function scaledValue(prop: string, value: string): string | null {
  const v = value.trim();
  if (v.includes("--text-scale")) return null;
  if (prop === "font-size" && SCALABLE.test(v)) return `calc(${v} * var(--text-scale, 1))`;
  if (prop === "line-height" && (PX.test(v) || SPACING.test(v))) return `calc(${v} * var(--text-scale, 1))`;
  return null;
}

interface Decl { prop: string; value: string }

/** The PostCSS plugin (vite.config.ts `css.postcss`); runs after Tailwind has written its utilities. */
export function textScale() {
  return {
    postcssPlugin: "persona-text-scale",
    Declaration(decl: Decl) {
      const next = scaledValue(decl.prop, decl.value);
      if (next) decl.value = next;
    },
  };
}
textScale.postcss = true;
