// A plugin's declarative UI (docs/plugins.md「声明式 UI」): the plugin sends a View (a few fixed kinds of items), the
// web draws it with its own design system, and buttons go back to the plugin. No plugin code runs here, no plugin
// colors, sizes or icons: a tone becomes one of the theme's role colors. The checks match server/internal/plugin/ui.go
// (the hub refuses a bad view before it gets here; the web still draws only what it knows). UI version 2
// (docs/plugins.md「UI 插槽」) adds the slots (nav_page, overview_card, machine_section), chart, stat_grid, segmented,
// refresh_after and the render context; an item type the web does not know is skipped, never a white page. An
// inbox_item (docs/plugins.md「收件箱条目」) draws the body of an inbox item from the plugin's own connector.
import { api, ApiError, enc } from "./api";

/** The UI version the web draws (context.ui_version). */
export const UI_VERSION = 2;

export type UITone = "neutral" | "positive" | "caution" | "danger";

export interface UIAction {
  id: string;
  /** Words to confirm with first; empty: no confirmation. */
  confirm?: string;
  /** Send every field's value along. */
  submit?: boolean;
}

export interface UIChoice {
  value: string;
  title?: string;
}

/** One item of a section; which fields it has depends on `type`. */
export interface UIItem {
  type: "text" | "metric" | "progress" | "row" | "button" | "link" | "field" | "divider" | "chart" | "stat_grid" | "segmented" | string;
  text?: string;
  /** text: body | secondary | caption | code */
  style?: string;
  label?: string;
  value?: string;
  detail?: string;
  tone?: UITone;
  ratio?: number;
  title?: string;
  subtitle?: string;
  badge?: { text: string; tone?: UITone };
  action?: UIAction;
  /** button: normal | destructive */
  role?: string;
  url?: string;
  /** field, segmented */
  name?: string;
  /** field: text | secret | number | toggle | choice; chart: bar | line */
  kind?: "text" | "secret" | "number" | "toggle" | "choice" | "bar" | "line" | string;
  choices?: UIChoice[];
  placeholder?: string;
  /** chart: its series (the same labels, in the same order), the unit of the values, bars stacked */
  series?: ChartSeries[];
  unit?: string;
  stacked?: boolean;
  /** stat_grid */
  stats?: UIStat[];
}

/** One line, or one color of bars; no tone: the web's own series colors, in order. */
export interface ChartSeries {
  name?: string;
  tone?: UITone;
  points: ChartPoint[];
}

/** A value at a label; `display` is how to write it ("¥1.20"), else the web writes the value with the chart's unit. */
export interface ChartPoint {
  label: string;
  value: number;
  display?: string;
}

/** One number of a stat_grid; the tone colors its detail. */
export interface UIStat {
  label: string;
  value: string;
  detail?: string;
  tone?: UITone;
}

export interface UISection {
  title?: string;
  footer?: string;
  items: UIItem[];
}

export interface UIView {
  title?: string;
  sections: UISection[];
  /** The plugin's own, sent back as it is. */
  state?: string;
  /** Draw again after this many seconds while on screen (30 at least, a day at most; 0 never). */
  refresh_after?: number;
}

/** Where a plugin draws (GET /api/v1/plugins/-/ui, and a plugin detail's `ui`). */
export interface UIPlace {
  plugin: string;
  /** The plugin's name (UI version 2). */
  plugin_name?: string;
  id: string;
  title: string;
  placement: "plugin_page" | "launcher_section" | "nav_page" | "overview_card" | "machine_section" | "inbox_item" | string;
  /** compact (a card) or full (a page, a section). */
  size?: "compact" | "full" | string;
  /** A nav_page's icon, from UI_ICONS. */
  icon?: string;
  /** The page (nav_page or plugin_page of the same plugin) an overview_card opens. */
  opens?: string;
  /** On a nav_page's entry; updated by the plugin_ui event. */
  badge?: UIBadge | null;
  /** The connector whose inbox items an inbox_item draws (the items' `source`). */
  connector?: string;
}

export interface UIBadge {
  text: string;
  tone?: UITone;
}

/** What a button's press answered: a new view, a word, a link; nothing at all: draw again. */
export interface UIActionResult {
  view?: UIView;
  toast?: string;
  open_url?: string;
}

/** Where a view is drawn: the launcher (launcher_section), the machine (machine_section), the inbox item (inbox_item), the size (a card). */
export interface UIContext {
  launcher?: string;
  machine?: string;
  item?: string;
  size?: "compact" | "full";
}

/** What the web says with every render and action (docs/plugins.md「上下文」): time zone, language, platform, UI version. */
export interface UIContextSent extends UIContext {
  tz?: string;
  locale?: string;
  platform: "web";
  ui_version: number;
}

/** The context sent: the place's, with the browser's time zone and language. */
export function uiContext(context?: UIContext): UIContextSent {
  let tz: string | undefined;
  try {
    tz = Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    tz = undefined;
  }
  const locale = typeof navigator !== "undefined" ? navigator.language || undefined : undefined;
  const out: UIContextSent = { platform: "web", ui_version: UI_VERSION };
  if (tz) out.tz = tz;
  if (locale) out.locale = locale;
  if (context?.launcher) out.launcher = context.launcher;
  if (context?.machine) out.machine = context.machine;
  if (context?.item) out.item = context.item;
  if (context?.size) out.size = context.size;
  return out;
}

// ── the hub ─────────────────────────────────────────────

/** The plugins' places of a placement; no placement: every slot's but launcher sections' (nav, overview, machines). */
export function uiPlaces(placement: string, launcher?: string, machine?: string): Promise<UIPlace[]> {
  const q = new URLSearchParams(placement ? { placement } : {});
  if (launcher) q.set("launcher", launcher);
  if (machine) q.set("machine", machine);
  return api<{ ui?: UIPlace[] }>(`/plugins/-/ui${q.size ? `?${q}` : ""}`).then((r) => (Array.isArray(r.ui) ? r.ui : []));
}

const uiPath = (plugin: string, ui: string) => `/plugins/${enc(plugin)}/-/ui/${enc(ui)}`;

/** Draws the view (no action). A view that does not read at all is an error; unknown items are left out. */
export const renderUI = (plugin: string, ui: string, context?: UIContext, state?: string) =>
  api<{ view: unknown }>(uiPath(plugin, ui), { method: "POST", json: { context: uiContext(context), ...(state ? { state } : {}) } }).then((r) => readView(r.view));

/** A press of a button (or a row): the action's id, the view's state, and the fields' values when it submits. */
export const actUI = (plugin: string, ui: string, action: string, opts: { context?: UIContext; state?: string; values?: Record<string, string> } = {}) =>
  api<UIActionResult>(uiPath(plugin, ui), {
    method: "POST",
    json: { context: uiContext(opts.context), ...(opts.state ? { state: opts.state } : {}), action, ...(opts.values ? { values: opts.values } : {}) },
  }).then((r) => ({ ...r, view: r.view ? readView(r.view) : undefined }));

/**
 * The plugin said this is not its gateway (a launcher_section without `hosts` is listed on every launcher; docs/plugins.md
 * 「声明式 UI」): 404 {code: "unsupported"}. The section is hidden, not an error.
 */
export const isUnsupported = (e: unknown) => e instanceof ApiError && e.status === 404 && e.code === "unsupported";

// ── reading a view ──────────────────────────────────────

const TONES: readonly string[] = ["neutral", "positive", "caution", "danger"];

/** A tone the web knows, else neutral. */
export const toneOf = (t?: string): UITone => (t && TONES.includes(t) ? (t as UITone) : "neutral");

/** A ratio as a percentage the bar can draw: 0…100, whatever the plugin sent. */
export function percent(ratio?: number): number {
  if (typeof ratio !== "number" || !Number.isFinite(ratio)) return 0;
  return Math.round(Math.min(1, Math.max(0, ratio)) * 1000) / 10;
}

/** Only https links open (the hub checks too). */
export function isHttps(url?: string): boolean {
  if (!url) return false;
  try {
    return new URL(url).protocol === "https:";
  } catch {
    return false;
  }
}

/** The fields of a view, by name, as they start: what the plugin filled in (never a secret's), toggles "true" / "false". */
export function fieldValues(view: UIView | null | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const s of view?.sections ?? []) {
    for (const it of s.items ?? []) {
      if (it.type !== "field" || !it.name) continue;
      if (it.kind === "secret") out[it.name] = "";
      else if (it.kind === "toggle") out[it.name] = it.value === "true" ? "true" : "false";
      else if (it.kind === "choice") out[it.name] = it.value ?? it.choices?.[0]?.value ?? "";
      else out[it.name] = it.value ?? "";
    }
    // a segmented is a field chosen at once: a submitting button sends its choice too
    for (const it of s.items ?? []) {
      if (it.type === "segmented" && it.name) out[it.name] = it.value ?? it.choices?.[0]?.value ?? "";
    }
  }
  return out;
}

/** The values a submitting button sends: every field of the view, in the order drawn. */
export function submitValues(view: UIView | null | undefined, values: Record<string, string>): Record<string, string> {
  const start = fieldValues(view);
  const out: Record<string, string> = {};
  for (const k of Object.keys(start)) out[k] = values[k] ?? start[k];
  return out;
}

/** A number field typed into that does not read as a number. */
export function numberProblem(text: string): string {
  const t = text.trim();
  return t && !Number.isFinite(Number(t)) ? "要是一个数" : "";
}

/** How a section lays out: metrics side by side, buttons in one row, everything else on its own line. */
export type UIBlock = { kind: "metrics"; items: UIItem[] } | { kind: "buttons"; items: UIItem[] } | { kind: "item"; item: UIItem };

export function blocks(items: UIItem[]): UIBlock[] {
  const out: UIBlock[] = [];
  for (const it of items) {
    const last = out[out.length - 1];
    if (it.type === "metric" || it.type === "button") {
      const kind = it.type === "metric" ? "metrics" : "buttons";
      if (last && last.kind === kind) last.items.push(it);
      else out.push({ kind, items: [it] });
    } else out.push({ kind: "item", item: it });
  }
  return out;
}

/**
 * What to do after a press: the view to show (new, or "reload" to draw again), a word to say, a link to open; `source`:
 * open the inbox item's own source page (an inbox_item's open_url persona://inbox/<item>/source, for the item it is
 * drawn for).
 */
export function outcome(r: UIActionResult, item?: string): { view?: UIView; reload: boolean; toast?: string; url?: string; source?: true } {
  const url = r.open_url && isHttps(r.open_url) ? r.open_url : undefined;
  const source = !!item && !!r.open_url && inboxSourceItem(r.open_url) === item;
  return { view: r.view, reload: !r.view && !r.toast && !r.open_url, toast: r.toast || undefined, url, ...(source ? { source: true as const } : {}) };
}

/**
 * The item an inbox item's source link names (persona://inbox/<item>/source, the id escaped as one path segment, as
 * docs/plugins.md「收件箱条目」 says); undefined for any other link.
 */
export function inboxSourceItem(url: string): string | undefined {
  const m = /^persona:\/\/inbox\/([^/?#]+)\/source$/.exec(url);
  if (!m) return undefined;
  try {
    return decodeURIComponent(m[1]);
  } catch {
    return undefined;
  }
}

// ── reading what the hub sent (UI version 2) ─────────────

/** The item types the web draws; any other is skipped (a plugin newer than the web). */
export const KNOWN_ITEMS: readonly string[] = ["text", "metric", "progress", "row", "button", "link", "field", "divider", "chart", "stat_grid", "segmented"];

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/** An item the web can draw: a known type with what it needs to be drawn at all (a chart with points, a grid with stats). */
function drawable(it: unknown): it is UIItem {
  if (!isObject(it) || typeof it.type !== "string" || !KNOWN_ITEMS.includes(it.type)) return false;
  switch (it.type) {
    case "chart": {
      const series = it.series;
      return Array.isArray(series) && series.length > 0 && series.every((s) => isObject(s) && Array.isArray(s.points) && s.points.length > 0 && s.points.every((p) => isObject(p) && typeof p.value === "number" && Number.isFinite(p.value)));
    }
    case "stat_grid":
      return Array.isArray(it.stats) && it.stats.length > 0 && it.stats.every(isObject);
    case "segmented":
      return typeof it.name === "string" && !!it.name && isObject(it.action) && Array.isArray(it.choices) && it.choices.length > 0;
    default:
      return true;
  }
}

/**
 * The view as the web draws it: items it does not know left out, a section they emptied gone (one with a footer
 * stays), like the hub does for an older app. Only a view that does not read at all is an error.
 */
export function readView(raw: unknown): UIView {
  if (!isObject(raw) || !Array.isArray(raw.sections)) throw new Error("插件画的界面读不出来");
  const sections: UISection[] = [];
  for (const s of raw.sections) {
    if (!isObject(s)) continue;
    const items = Array.isArray(s.items) ? s.items : [];
    const kept = items.filter(drawable);
    const footer = typeof s.footer === "string" ? s.footer : undefined;
    if (items.length > 0 && kept.length === 0 && !footer) continue;
    sections.push({ ...(typeof s.title === "string" ? { title: s.title } : {}), ...(footer ? { footer } : {}), items: kept });
  }
  const view: UIView = { sections };
  if (typeof raw.title === "string") view.title = raw.title;
  if (typeof raw.state === "string") view.state = raw.state;
  if (typeof raw.refresh_after === "number") view.refresh_after = raw.refresh_after;
  return view;
}

/** How long until a view is drawn again, in ms: refresh_after held to 30 s … a day; 0 never. */
export function refreshDelay(view: UIView | null | undefined): number {
  const s = view?.refresh_after;
  if (typeof s !== "number" || !Number.isFinite(s) || s <= 0) return 0;
  return Math.min(86400, Math.max(30, s)) * 1000;
}

// ── slots ───────────────────────────────────────────────

/** A nav_page's icons (docs/plugins.md「UI 插槽」); each app draws its own. */
export const UI_ICONS = ["plugin", "chart", "gauge", "list", "bell", "doc", "calendar", "money"] as const;
export type UIIcon = (typeof UI_ICONS)[number];

/** An icon the web knows, else plugin. */
export const uiIcon = (name?: string): UIIcon => ((UI_ICONS as readonly string[]).includes(name ?? "") ? (name as UIIcon) : "plugin");

/** The slots the web draws, besides a plugin's own pages in its settings. */
export const SLOTS = ["nav_page", "overview_card", "machine_section", "inbox_item"] as const;
export type SlotName = (typeof SLOTS)[number];

/** The places of one placement, in the hub's order; a placement the web does not know is never asked for. */
export const placesOf = (list: readonly UIPlace[], placement: SlotName) => list.filter((u) => u.placement === placement);

/**
 * The plugin page that draws an inbox item's body (inbox_item): the one whose connector brought the item in (its
 * `source`). None: the item's own detail.
 */
export const inboxItemPlace = (list: readonly UIPlace[], source?: string): UIPlace | undefined =>
  source ? list.find((u) => u.placement === "inbox_item" && !!u.connector && u.connector === source) : undefined;

/** The plugin_ui event: a nav_page's badge changed (null clears it). */
export interface BadgeEvent {
  plugin: string;
  id: string;
  badge?: UIBadge | null;
}

/** The list with one badge changed; the same list when nothing did. */
export function applyBadge(list: UIPlace[], ev: BadgeEvent): UIPlace[] {
  const badge = ev.badge && typeof ev.badge.text === "string" && ev.badge.text ? { text: ev.badge.text, ...(ev.badge.tone ? { tone: ev.badge.tone } : {}) } : null;
  let changed = false;
  const out = list.map((u) => {
    if (u.plugin !== ev.plugin || u.id !== ev.id) return u;
    if ((u.badge?.text ?? "") === (badge?.text ?? "") && (u.badge?.tone ?? "") === (badge?.tone ?? "")) return u;
    changed = true;
    return { ...u, badge };
  });
  return changed ? out : list;
}

// ── charts ──────────────────────────────────────────────

/** A number written short: 950, 1.2K, 3.4M, 2B; small ones with at most two decimals. */
export function compactNumber(v: number): string {
  if (!Number.isFinite(v)) return "—";
  const a = Math.abs(v);
  const sign = v < 0 ? "-" : "";
  const short = (x: number) => {
    const d = x >= 100 ? 0 : x >= 10 ? 1 : 2;
    return String(Number(x.toFixed(d)));
  };
  // a value that rounds up into the next unit is written in it (999,950 → 1M, not 1000K)
  for (const [n, u] of [[1e12, "T"], [1e9, "B"], [1e6, "M"], [1e3, "K"]] as const) {
    if (a >= n * 0.9995) return sign + short(a / n) + u;
  }
  return sign + short(a);
}

/** A point's value as the chart writes it: the plugin's display, else a short number with the unit. */
export const pointText = (p: ChartPoint, unit?: string) => p.display || `${compactNumber(p.value)}${unit ? ` ${unit}` : ""}`;

/** A linear scale from a domain to a range. */
export const linear = (d0: number, d1: number, r0: number, r1: number) => (v: number) => (d1 === d0 ? (r0 + r1) / 2 : r0 + ((v - d0) / (d1 - d0)) * (r1 - r0));

/** Round ticks around the values, zero always in (bars grow from it): the axis's bottom, top and its ticks. */
export function niceTicks(min: number, max: number, count = 4): { lo: number; hi: number; ticks: number[] } {
  let lo = Math.min(0, min);
  let hi = Math.max(0, max);
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return { lo: 0, hi: 1, ticks: [0, 1] };
  if (hi === lo) hi = lo + 1;
  const raw = (hi - lo) / Math.max(1, count);
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = ([1, 2, 2.5, 5, 10].find((m) => m * mag >= raw - 1e-12) ?? 10) * mag;
  lo = Math.floor(lo / step + 1e-9) * step;
  hi = Math.ceil(hi / step - 1e-9) * step;
  const ticks: number[] = [];
  for (let v = lo; v <= hi + step / 2; v += step) ticks.push(Number(v.toPrecision(12)));
  return { lo, hi, ticks };
}

/** Each label's span of the bar (or point) at a label; stacked: positives on positives, negatives under negatives. */
export interface BarSpan {
  series: number;
  from: number;
  to: number;
  /** The outermost of its stack (or any grouped bar): its free end is rounded. */
  end: boolean;
}

export function barSpans(series: ChartSeries[], stacked: boolean): BarSpan[][] {
  const n = series[0]?.points.length ?? 0;
  const out: BarSpan[][] = [];
  for (let i = 0; i < n; i++) {
    const col: BarSpan[] = [];
    let up = 0;
    let down = 0;
    series.forEach((s, k) => {
      const v = s.points[i]?.value ?? 0;
      if (!stacked) col.push({ series: k, from: 0, to: v, end: true });
      else if (v >= 0) col.push({ series: k, from: up, to: (up += v), end: false });
      else col.push({ series: k, from: down, to: (down += v), end: false });
    });
    if (stacked) {
      // the last non-empty span on each side carries the rounded end
      const top = col.filter((b) => b.to > b.from).pop();
      const bottom = col.filter((b) => b.to < b.from).pop();
      if (top) top.end = true;
      if (bottom) bottom.end = true;
    }
    out.push(col);
  }
  return out;
}

/** The lowest and highest a chart reaches (stacked: the sums). */
export function chartExtent(series: ChartSeries[], kind: string, stacked: boolean): [number, number] {
  const vals = kind === "bar" && stacked ? barSpans(series, true).flatMap((c) => c.map((b) => b.to)) : series.flatMap((s) => s.points.map((p) => p.value));
  return vals.length ? [Math.min(...vals), Math.max(...vals)] : [0, 1];
}

/** About how wide a label is drawn, in px: CJK one em, the rest about 0.6 em. */
export function labelWidth(text: string, px: number): number {
  let w = 0;
  for (const ch of text) w += /[\u2e80-\u9fff\uac00-\ud7af\uff00-\uffef]/.test(ch) ? px : px * 0.6;
  return w;
}

/**
 * The labels to write under a chart so none overlap: every k-th (the smallest k that fits), the last one too when it
 * fits (else it takes the place of the one before). `xs` are their centers; the first and last are drawn flush with the
 * edges of [0, width].
 */
export function thinLabels(labels: string[], xs: number[], width: number, px = 10.5, gap = 8): number[] {
  const n = labels.length;
  if (!n) return [];
  const span = (i: number): [number, number] => {
    const w = labelWidth(labels[i], px);
    const a = Math.min(Math.max(0, xs[i] - w / 2), Math.max(0, width - w));
    return [a, a + w];
  };
  const fits = (list: number[]) => list.every((i, j) => j === 0 || span(list[j - 1])[1] + gap <= span(i)[0]);
  for (let k = 1; k <= n; k++) {
    const list: number[] = [];
    for (let i = 0; i < n; i += k) list.push(i);
    if (!fits(list)) continue;
    if (list[list.length - 1] !== n - 1) {
      const withLast = [...list, n - 1];
      if (fits(withLast)) return withLast;
      const swapped = [...list.slice(0, -1), n - 1];
      if (fits(swapped)) return swapped;
    }
    return list;
  }
  return [n - 1];
}

/** A compact chart writes only the first and the last label, when both fit; else the last. */
export function edgeLabels(labels: string[], width: number, px = 10.5, gap = 8): number[] {
  const n = labels.length;
  if (n <= 1) return n ? [0] : [];
  return labelWidth(labels[0], px) + labelWidth(labels[n - 1], px) + gap <= width ? [0, n - 1] : [n - 1];
}

const TONE_COLOR: Record<UITone, string> = { neutral: "var(--ink-3)", positive: "var(--green)", caution: "var(--orange)", danger: "var(--red)" };

/**
 * The colors of a chart's series: a tone is its role color (an unknown one neutral); the rest take the web's series
 * colors in order (the first the main one), counted among the series without a tone.
 */
export function seriesColors(series: ChartSeries[]): string[] {
  let slot = 0;
  return series.map((s) => (s.tone ? TONE_COLOR[toneOf(s.tone)] : `var(--series-${(slot++ % 4) + 1})`));
}
