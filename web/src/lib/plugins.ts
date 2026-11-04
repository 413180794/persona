// 设置 › 插件 (docs/plugins.md「hub 的接口」「App：设置 › 插件」, iOS PluginsModel): the hub's plugins, each one's
// switch, its configuration as a form drawn from its schema (`form.fields`), and its secrets, write-only. Installing,
// upgrading and removing happen on the hub's machine (`personad plugin`), never here.
import { api, ApiError, enc } from "./api";
import type { Machine } from "./types";
import type { UIPlace } from "./plugin-ui";
import { refreshPluginPlaces } from "./plugin-slots";

export type Json = string | number | boolean | null | Json[] | { [k: string]: Json };
export type JsonObject = { [k: string]: Json };

/** One of GET /api/v1/plugins. */
export interface HubPlugin {
  id: string;
  name: string;
  kind: string;
  /** disabled | needs_setup | incompatible | stopped | starting | running | error */
  status: string;
  error?: string;
  version?: string;
  /** builtin | hub.json | installed */
  source?: string;
  enabled?: boolean;
  description?: string;
  /** The extension points it contributes to, with the ids. */
  contributes?: Record<string, string[]>;
  /** What it still needs before it runs, in words. */
  needs_setup?: string[];
  warnings?: string[];
}

export interface PluginChoice {
  value: Json;
  title?: string;
}

/** One field of the form: the hub's reading of the manifest's JSON Schema subset. */
export interface PluginField {
  name: string;
  /** string | integer | number | boolean | array | object */
  type: string;
  title?: string;
  description?: string;
  default?: Json;
  required?: boolean;
  enum?: PluginChoice[];
  format?: string;
  pattern?: string;
  min_length?: number;
  max_length?: number;
  minimum?: number;
  maximum?: number;
  /** A list's items; format machine: the hub's machines by id (of the systems in os), drawn as a switch each. */
  items?: { type: string; enum?: PluginChoice[]; format?: string; os?: string[] };
  min_items?: number;
  max_items?: number;
  unique_items?: boolean;
  /** An object's fields (a group of the form). */
  fields?: PluginField[];
}

/** A declared secret and whether it has a value (never the value). */
export interface PluginSecret {
  name: string;
  title: string;
  description?: string;
  required?: boolean;
  set: boolean;
}

/** A line of what a plugin may do. */
export interface PluginPermission {
  key: string;
  title: string;
  detail?: string;
}

/** GET /api/v1/plugins/{id}/-/: the plugin with its form, settings, secrets and permissions. */
export interface HubPluginDetail extends HubPlugin {
  publisher?: string;
  homepage?: string;
  origin?: string;
  previous_version?: string;
  /** hub.json's: switched and set up there only. */
  read_only?: boolean;
  form?: { fields?: PluginField[] };
  config?: JsonObject;
  secrets?: PluginSecret[];
  permissions?: PluginPermission[];
  /** The pages it draws in its settings (placement plugin_page, docs/plugins.md「声明式 UI」). */
  ui?: UIPlace[];
}

// ── the hub ─────────────────────────────────────────────

const base = (id: string) => `/plugins/${enc(id)}/-`;

export const listPlugins = () => api<{ plugins?: HubPlugin[]; can_install?: boolean }>("/plugins").then((r) => r.plugins ?? []);
/** The list with what this hub allows: installing at all (a database), and markets (docs/plugins.md「插件市场」). */
export const listPluginsInfo = () =>
  api<{ plugins?: HubPlugin[]; can_install?: boolean; markets?: boolean }>("/plugins").then((r) => ({
    plugins: r.plugins ?? [],
    canInstall: !!r.can_install,
    markets: !!r.markets,
  }));
export const pluginDetail = (id: string) => api<HubPluginDetail>(`${base(id)}/`);
/** After a change that can start, stop or reconfigure a plugin: its entries, cards and sections are asked for again. */
export const changed = <T>(r: T): T => (refreshPluginPlaces(), r);
export const setPluginEnabled = (id: string, enabled: boolean) => api<HubPluginDetail>(`${base(id)}/enabled`, { method: "PUT", json: { enabled } }).then(changed);
export const setPluginConfig = (id: string, config: JsonObject) => api<HubPluginDetail>(`${base(id)}/config`, { method: "PUT", json: { config } }).then(changed);
export const setPluginSecret = (id: string, name: string, value: string) =>
  api<HubPluginDetail>(`${base(id)}/secrets/${enc(name)}`, { method: "PUT", json: { value } });
export const clearPluginSecret = (id: string, name: string) => api<HubPluginDetail>(`${base(id)}/secrets/${enc(name)}`, { method: "DELETE" });

/** A 400 invalid_config: the field the hub refused (its dotted path, "" when it names none) and why. */
export function configError(e: unknown): { field: string; message: string } | null {
  if (!(e instanceof ApiError) || e.code !== "invalid_config") return null;
  const field = typeof e.body.field === "string" ? e.body.field : "";
  const message = typeof e.body.message === "string" && e.body.message ? e.body.message : e.message;
  return { field, message };
}

// ── words ───────────────────────────────────────────────

const STATES: Record<string, string> = {
  running: "运行中",
  stopped: "空闲，用到时启动",
  starting: "正在启动",
  needs_setup: "需要设置",
  disabled: "已停用",
  incompatible: "这台 hub 用不了",
  error: "出错了",
};

export const pluginState = (p: HubPlugin) => STATES[p.status] ?? "状态未知";

export const isProblem = (p: HubPlugin) => p.status === "error" || p.status === "incompatible";

/** The tone of a plugin's dot: red broken, orange waiting on the user, green running, gray otherwise. */
export function pluginTone(p?: HubPlugin | null): "red" | "orange" | "green" | "idle" {
  if (!p) return "idle";
  if (isProblem(p)) return "red";
  if (p.status === "needs_setup") return "orange";
  return p.status === "running" ? "green" : "idle";
}

export function sourceText(s?: string): string | undefined {
  return s === "builtin" ? "内置" : s === "hub.json" ? "hub.json" : s === "installed" ? "已安装" : undefined;
}

export function pointText(name: string): string {
  return name === "connectors" ? "连接器" : name === "voice" ? "语音对话" : name;
}

/** The detail's line under the name: state · version · source. */
export const stateLine = (p: HubPlugin) => [pluginState(p), p.version, sourceText(p.source)].filter(Boolean).join(" · ");

/** How many, and how many need the user. */
export function listSummary(list?: HubPlugin[] | null): string {
  if (!list) return "";
  if (!list.length) return "没有";
  const attention = list.filter((p) => p.status === "needs_setup" || isProblem(p)).length;
  return attention ? `${list.length} 个 · ${attention} 个要处理` : `${list.length} 个`;
}

export const fieldLabel = (f: PluginField) => f.title || f.name;

/** The title with 「（必填）」 when it has to be filled in and has no default to fall back to. */
export const fieldTitle = (f: PluginField) => fieldLabel(f) + (f.required && f.default === undefined ? "（必填）" : "");

export const choiceLabel = (c: PluginChoice) => c.title || (typeof c.value === "string" ? c.value : JSON.stringify(c.value));

/** What shows when a field is empty: the default it falls back to, or 未设置. */
export function placeholder(f: PluginField): string {
  if (f.default !== undefined && f.default !== null) {
    const choice = f.enum?.find((c) => same(c.value, f.default!));
    const text = choice ? choiceLabel(choice) : typeof f.default === "object" ? "" : String(f.default);
    if (text) return `默认：${text}`;
  }
  return "未设置";
}

export const secretState = (s: PluginSecret) => (s.set ? "已设置" : s.required ? "未设置（必填）" : "未设置");

// ── the draft, by path ──────────────────────────────────

export function same(a: Json | undefined, b: Json | undefined): boolean {
  if (a === b) return true;
  if (a === null || b === null || a === undefined || b === undefined || typeof a !== "object" || typeof b !== "object") return false;
  if (Array.isArray(a) || Array.isArray(b)) {
    return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((x, i) => same(x, b[i]));
  }
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  return ka.length === kb.length && ka.every((k) => k in b && same(a[k], b[k]));
}

export function valueAt(draft: JsonObject, path: string[]): Json | undefined {
  let cur: Json | undefined = draft;
  for (const k of path) {
    if (!cur || typeof cur !== "object" || Array.isArray(cur)) return undefined;
    cur = cur[k];
  }
  return cur === null ? undefined : cur;
}

/** Sets (undefined: removes) the value at path; a group left empty goes too. */
export function setAt(draft: JsonObject, path: string[], value: Json | undefined): JsonObject {
  const [key, ...rest] = path;
  if (key === undefined) return draft;
  const out: JsonObject = { ...draft };
  let next: Json | undefined = value;
  if (rest.length) {
    const inner = out[key];
    const obj = inner && typeof inner === "object" && !Array.isArray(inner) ? inner : {};
    const set = setAt(obj, rest, value);
    next = Object.keys(set).length ? set : undefined;
  }
  if (next === undefined) delete out[key];
  else out[key] = next;
  return out;
}

/** Without empty strings and lists the user cleared: "not set" (the schema's default applies). */
export function compact(obj: JsonObject): JsonObject {
  const out: JsonObject = {};
  for (const [k, v] of Object.entries(obj)) {
    if (v === null || v === "" || (Array.isArray(v) && !v.length)) continue;
    if (typeof v === "object" && !Array.isArray(v)) {
      const inner = compact(v);
      if (Object.keys(inner).length) out[k] = inner;
      continue;
    }
    out[k] = v;
  }
  return out;
}

/** A number field as typed: the value it reads as (undefined: empty, not set), or why it doesn't read as one. */
export function readNumber(text: string, integer: boolean): { value?: number; error?: string } {
  const t = text.trim();
  if (!t) return {};
  const n = Number(t);
  if (!Number.isFinite(n)) return { error: "要是一个数" };
  if (integer && !Number.isInteger(n)) return { error: "要是一个整数" };
  return { value: n };
}

/** Number fields typed into that do not read as one, by dotted path. */
export function numberProblems(typed: Record<string, { text: string; integer: boolean }>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(typed)) {
    const e = readNumber(v.text, v.integer).error;
    if (e) out[k] = e;
  }
  return out;
}

/** Whether the draft differs from what the hub has (both read as sent: compacted). */
export const isDirty = (draft: JsonObject, config: JsonObject | undefined) => !same(compact(draft), compact(config ?? {}));

/** The items of a list field: the draft's, else the default. */
export function listItems(draft: JsonObject, path: string[], f: PluginField): string[] {
  const v = valueAt(draft, path) ?? f.default;
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

/** A machine on a plugin's machine list (format machine): online or not, and for a plugin that operates computers how
 *  Cua Driver is there (known once it is turned on). */
export function machineStatus(m: Machine, computers: boolean): string {
  const parts = [m.online ? "在线" : "离线"];
  const c = m.computer;
  // Every Mac's while the plugin is on, once its node has said (a never-asked one has neither a verdict nor a note).
  if (computers && m.online && c && (c.ready || c.note)) {
    const missing = c.accessibility === "missing" || c.screen_recording === "missing";
    parts.push(!c.installed ? "Cua 没装" : !c.daemon ? "Cua 没在运行" : missing ? "Cua 缺权限" : c.desktop === "locked" ? "锁屏了" : c.ready ? "Cua 可以用" : "Cua 已装");
  }
  return parts.join(" · ");
}

/** Under the form: which switches take effect at once (lists of machines) and that the rest waits for 保存. */
export function saveFooter(fields: PluginField[]): string {
  const all = fields.flatMap((f) => (f.type === "object" ? (f.fields ?? []) : [f]));
  const now = all.filter((f) => f.items?.format === "machine").map((f) => `「${f.title || f.name}」`);
  if (!now.length) return "改完点「保存」；保存后插件会重新启动。留空的项用插件的默认值。";
  if (now.length === all.length) return `${now.join("、")}的开关即时生效，不用保存。`;
  return `${now.join("、")}的开关即时生效；其他项改完点「保存」，保存后插件会重新启动。留空的项用插件的默认值。`;
}
