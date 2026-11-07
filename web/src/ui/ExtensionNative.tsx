// Fixed native recipes, isolated from legacy extension pages. No extension styles or scripts.
import { useState } from "react";
import type { Color, Spark } from "../lib/extension-wire";
import { t } from "../i18n";
import { cx } from "./atoms";
import { type IconName } from "./icons";
const NATIVE_ICON_ALIASES: Record<string, IconName> = { plugin: "extension", doc: "read", settings: "gear", pull_request: "pullRequest", refresh: "retry", edit: "pencil", robot: "agent", speaker: "volume", location: "target", trend_up: "up", trend_down: "arrowDown" };
export const nativeIcon = (name?: string): IconName => name ? NATIVE_ICON_ALIASES[name] ?? name : "extension";

export const NATIVE = Object.freeze({
  page: "flex min-w-0 flex-col gap-4 text-sm text-ink",
  row: "flex min-w-0 items-center gap-3 px-4 py-3",
  field: "w-full rounded-control bg-field px-3 py-2 text-sm text-ink shadow-inset-field outline-none disabled:opacity-60",
  card: "overflow-hidden rounded-card bg-surface shadow-card",
  label: "text-sm font-medium text-ink-2",
  detail: "text-xs text-ink-3",
  grid: { 1: "grid-cols-1", 2: "grid-cols-2", 3: "grid-cols-2 md:grid-cols-3", 4: "grid-cols-2 lg:grid-cols-4", 5: "grid-cols-2 lg:grid-cols-5", 6: "grid-cols-2 lg:grid-cols-6" } as Record<number, string>,
  tileWidth: { narrow: "w-36", regular: "w-52", wide: "w-4/5" } as Record<string, string>,
  aspect: { square: "1 / 1", landscape: "4 / 3", wide: "16 / 9", portrait: "3 / 4" } as Record<string, string>,
  tone: { neutral: "text-ink", positive: "text-green", caution: "text-orange", danger: "text-red" },
  thumbnail: { square: "size-10", wide: "h-10 w-16" } as Record<string, string>,
  searchDelay: 300, stepperDelay: 600, longPressDelay: 450, swipeDistance: 48,
});
// The existing design system has these named roles. Other native hues are negotiated gaps, not silently approximated.
export const NATIVE_COLOR: Partial<Record<Color, string>> = { red: "var(--red)", orange: "var(--orange)", green: "var(--green)", blue: "var(--accent)", purple: "var(--ai)", gray: "var(--ink-3)" };
export const nativeColor = (color?: Color, tone?: string) => color ? NATIVE_COLOR[color] : tone === "positive" ? "var(--green)" : tone === "caution" ? "var(--orange)" : tone === "danger" ? "var(--red)" : "var(--ink-3)";

/** A native active tile uses only a whitelisted design-system role, never extension CSS. */
export const nativeActiveStyle = (color?: Color) => color && NATIVE_COLOR[color] ? { backgroundColor: `color-mix(in srgb, ${NATIVE_COLOR[color]} 12%, var(--surface))` } : undefined;

/** Accessible native tabs; the fixed recipe keeps labels to one line. */
export function NativeTabs({ name, items, value, disabled, onChoose }: { name: string; items: Array<{ value: string; title?: string; badge?: string }>; value: string; disabled?: boolean; onChoose: (value: string) => void }) {
  return <div role="tablist" aria-label={name} className="flex flex-wrap gap-2">{items.map((item) => <button key={item.value} type="button" role="tab" aria-selected={value === item.value} disabled={disabled} onClick={() => onChoose(item.value)} className={cx("rounded-control px-3 py-2 text-sm whitespace-nowrap disabled:opacity-60", value === item.value ? "bg-accent-tint text-accent-ink" : "bg-field text-ink-2")}><span>{item.title ?? item.value}</span>{item.badge !== undefined && <span>{item.badge}</span>}</button>)}</div>;
}

/** A single trend, with point values available to keyboard, hover and screen-reader users. */
export function NativeSpark({ spark, label }: { spark: Spark; label: string }) {
  const [at, setAt] = useState<number | null>(null);
  const width = 100, height = 28, pad = 2;
  const low = Math.min(...spark.values), high = Math.max(...spark.values);
  const points = spark.values.map((value, i) => ({ x: pad + i / Math.max(1, spark.values.length - 1) * (width - pad * 2), y: height - pad - (high === low ? 0.5 : (value - low) / (high - low)) * (height - pad * 2) }));
  const line = points.map((p, i) => `${i ? "L" : "M"}${p.x},${p.y}`).join(" ");
  const color = nativeColor(spark.color, spark.tone);
  return <span className="relative inline-block w-24 shrink-0 text-ink-3">
    <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${label}: ${spark.values.join(", ")}`}>
      {spark.fill && <path d={`${line} L${points.at(-1)?.x ?? pad},${height} L${pad},${height} Z`} fill={color} opacity={0.12} />}
      <path d={line} fill="none" stroke={color} strokeWidth={2} vectorEffect="non-scaling-stroke" />
      {points.map((p, i) => <circle key={i} cx={p.x} cy={p.y} r={4} fill="transparent" tabIndex={0} aria-label={String(spark.values[i])} onFocus={() => setAt(i)} onBlur={() => setAt(null)} onMouseEnter={() => setAt(i)} onMouseLeave={() => setAt(null)}><title>{spark.values[i]}</title></circle>)}
    </svg>
    {at !== null && <span role="tooltip" className="absolute bottom-full rounded-control bg-tooltip-bg px-2 py-1 text-xs text-tooltip-fg">{spark.values[at]}</span>}
    <details className="text-xs"><summary className="cursor-pointer">{t("extensions-native.values")}</summary><span className={cx(NATIVE.detail, "select-text")}>{spark.values.join(", ")}</span></details>
  </span>;
}
