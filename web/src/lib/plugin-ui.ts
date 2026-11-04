// A plugin's declarative UI (docs/plugins.md「声明式 UI」): the plugin sends a View (a few fixed kinds of items), the
// web draws it with its own design system, and buttons go back to the plugin. No plugin code runs here, no plugin
// colors, sizes or icons: a tone becomes one of the theme's role colors. The checks match server/internal/plugin/ui.go
// (the hub refuses a bad view before it gets here; the web still draws only what it knows).
import { api, ApiError, enc } from "./api";

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
  type: "text" | "metric" | "progress" | "row" | "button" | "link" | "field" | "divider" | string;
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
  /** field */
  name?: string;
  kind?: "text" | "secret" | "number" | "toggle" | "choice" | string;
  choices?: UIChoice[];
  placeholder?: string;
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
}

/** Where a plugin draws (GET /api/v1/plugins/-/ui, and a plugin detail's `ui`). */
export interface UIPlace {
  plugin: string;
  id: string;
  title: string;
  placement: "plugin_page" | "launcher_section" | string;
}

/** What a button's press answered: a new view, a word, a link; nothing at all: draw again. */
export interface UIActionResult {
  view?: UIView;
  toast?: string;
  open_url?: string;
}

export interface UIContext {
  launcher?: string;
  machine?: string;
}

// ── the hub ─────────────────────────────────────────────

export function uiPlaces(placement: string, launcher?: string, machine?: string): Promise<UIPlace[]> {
  const q = new URLSearchParams({ placement });
  if (launcher) q.set("launcher", launcher);
  if (machine) q.set("machine", machine);
  return api<{ ui?: UIPlace[] }>(`/plugins/-/ui?${q}`).then((r) => r.ui ?? []);
}

const uiPath = (plugin: string, ui: string) => `/plugins/${enc(plugin)}/-/ui/${enc(ui)}`;

const withContext = (context?: UIContext) => (context && (context.launcher || context.machine) ? { context } : {});

/** Draws the view (no action). */
export const renderUI = (plugin: string, ui: string, context?: UIContext, state?: string) =>
  api<{ view: UIView }>(uiPath(plugin, ui), { method: "POST", json: { ...withContext(context), ...(state ? { state } : {}) } }).then((r) => r.view);

/** A press of a button (or a row): the action's id, the view's state, and the fields' values when it submits. */
export const actUI = (plugin: string, ui: string, action: string, opts: { context?: UIContext; state?: string; values?: Record<string, string> } = {}) =>
  api<UIActionResult>(uiPath(plugin, ui), {
    method: "POST",
    json: { ...withContext(opts.context), ...(opts.state ? { state: opts.state } : {}), action, ...(opts.values ? { values: opts.values } : {}) },
  });

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

/** What to do after a press: the view to show (new, or "reload" to draw again), a word to say, a link to open. */
export function outcome(r: UIActionResult): { view?: UIView; reload: boolean; toast?: string; url?: string } {
  const url = r.open_url && isHttps(r.open_url) ? r.open_url : undefined;
  return { view: r.view, reload: !r.view && !r.toast && !r.open_url, toast: r.toast || undefined, url };
}
