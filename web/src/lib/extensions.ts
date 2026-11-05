// 设置 › 扩展 (docs/extensions.md「hub 的接口」「App：设置 › 扩展」, iOS ExtensionsModel): the hub's extensions, each one's
// switch, its configuration as a form drawn from its schema (`form.fields`), and its secrets, write-only. Installing,
// upgrading and removing happen on the hub's machine (`personad extension`), never here.
import { api, ApiError, enc } from "./api";
import type { Machine } from "./types";
import { placeOf, type UIPlace } from "./extension-ui";
import { extRoot, listOf } from "./extension-api";
import { refreshExtensionPlaces } from "./extension-slots";

export type Json = string | number | boolean | null | Json[] | { [k: string]: Json };
export type JsonObject = { [k: string]: Json };

/** One of GET /api/v1/extensions. */
export interface HubExtension {
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
  /** An extension from a market (a hub with plugin_auto_upgrade): its own auto-upgrade switch, its newer version and what
   *  happened last with an upgrade (docs/extensions.md「自动升级」). */
  auto_upgrade?: boolean;
  upgrade?: ExtensionUpgrade;
  upgrade_note?: ExtensionUpgradeNote;
  /** An extension with a program on computers: how that program is on each machine it is switched on for. */
  nodes?: ExtensionNodeState[];
}

/** An extension's program on one machine (docs/extensions.md「在电脑上运行的程序」). */
export interface ExtensionNodeState {
  machine_id: string;
  version?: string;
  /** running | starting | stopped | error | missing | lease_expired | offline | outdated */
  state: string;
  error?: string;
}

/** A market extension's newer version and where it stands. */
export interface ExtensionUpgrade {
  id: string;
  name?: string;
  market: string;
  from: string;
  to: string;
  /** available | waiting_idle | needs_permission | off | skipped | failed | incompatible */
  state: string;
  added_permissions?: ExtensionPermission[];
  detail?: string;
}

/** What happened last with an extension's upgrade. */
export interface ExtensionUpgradeNote {
  /** upgraded | rolled_back | needs_permission | failed */
  kind: string;
  from: string;
  to: string;
  detail?: string;
  at: string;
}

export interface ExtensionChoice {
  value: Json;
  title?: string;
}

/** One field of the form: the hub's reading of the manifest's JSON Schema subset. */
export interface ExtensionField {
  name: string;
  /** string | integer | number | boolean | array | object */
  type: string;
  title?: string;
  description?: string;
  default?: Json;
  required?: boolean;
  enum?: ExtensionChoice[];
  format?: string;
  pattern?: string;
  min_length?: number;
  max_length?: number;
  minimum?: number;
  maximum?: number;
  /** A list's items; format machine: the hub's machines by id (of the systems in os), drawn as a switch each. */
  items?: { type: string; enum?: ExtensionChoice[]; format?: string; os?: string[] };
  min_items?: number;
  max_items?: number;
  unique_items?: boolean;
  /** An object's fields (a group of the form). */
  fields?: ExtensionField[];
  /** x-confirm: what to ask before a switch of it is turned on (the extension cua's 「扩展开着时不自动锁屏」). */
  confirm?: string;
}

/** A declared secret and whether it has a value (never the value). */
export interface ExtensionSecret {
  name: string;
  title: string;
  description?: string;
  required?: boolean;
  set: boolean;
}

/** A line of what an extension may do. */
export interface ExtensionPermission {
  key: string;
  title: string;
  detail?: string;
}

/** GET /api/v1/extensions/{id}/-/: the extension with its form, settings, secrets and permissions. */
export interface HubExtensionDetail extends HubExtension {
  publisher?: string;
  homepage?: string;
  origin?: string;
  previous_version?: string;
  /** hub.json's: switched and set up there only. */
  read_only?: boolean;
  form?: { fields?: ExtensionField[] };
  config?: JsonObject;
  secrets?: ExtensionSecret[];
  permissions?: ExtensionPermission[];
  /** The pages it draws in its settings (placement plugin_page, docs/extensions.md「声明式 UI」). */
  ui?: UIPlace[];
  /** The field (format machine) that picks the computers its program runs on; their rows say how it is there. */
  node_machines?: string;
}

// ── the hub ─────────────────────────────────────────────

const base = (id: string) => `${extRoot()}/${enc(id)}/-`;

type ListAnswer = { extensions?: HubExtension[]; plugins?: HubExtension[]; can_install?: boolean; markets?: boolean };

export const listExtensions = () => api<ListAnswer>(extRoot()).then((r) => listOf(r));
/** The list with what this hub allows: installing at all (a database), and markets (docs/extensions.md「扩展市场」). */
export const listExtensionsInfo = () =>
  api<ListAnswer>(extRoot()).then((r) => ({
    extensions: listOf(r),
    canInstall: !!r.can_install,
    markets: !!r.markets,
  }));
/** A detail as either kind of hub sends it, its pages' places read with the new keys. */
export const detailOf = (d: HubExtensionDetail): HubExtensionDetail => (d.ui ? { ...d, ui: d.ui.map(placeOf) } : d);
export const extensionDetail = (id: string) => api<HubExtensionDetail>(`${base(id)}/`).then(detailOf);
/** After a change that can start, stop or reconfigure an extension: its entries, cards and sections are asked for again. */
export const changed = <T>(r: T): T => (refreshExtensionPlaces(), r);
export const setExtensionEnabled = (id: string, enabled: boolean) =>
  api<HubExtensionDetail>(`${base(id)}/enabled`, { method: "PUT", json: { enabled } }).then(detailOf).then(changed);
export const setExtensionAutoUpgrade = (id: string, auto: boolean) =>
  api<HubExtensionDetail>(`${base(id)}/auto-upgrade`, { method: "PUT", json: { auto } }).then(detailOf);
/** The auto-upgrade switch for all market extensions; a hub from before it has no route (null). */
export const extensionUpgrades = () =>
  api<{ auto: boolean; upgrades?: ExtensionUpgrade[] }>(`${extRoot()}/-/upgrades`).then((r) => r.auto, () => null);
export const setExtensionUpgrades = (auto: boolean) => api<{ auto: boolean }>(`${extRoot()}/-/upgrades`, { method: "PUT", json: { auto } }).then((r) => r.auto);
export const setExtensionConfig = (id: string, config: JsonObject) =>
  api<HubExtensionDetail>(`${base(id)}/config`, { method: "PUT", json: { config } }).then(detailOf).then(changed);
export const setExtensionSecret = (id: string, name: string, value: string) =>
  api<HubExtensionDetail>(`${base(id)}/secrets/${enc(name)}`, { method: "PUT", json: { value } }).then(detailOf);
export const clearExtensionSecret = (id: string, name: string) =>
  api<HubExtensionDetail>(`${base(id)}/secrets/${enc(name)}`, { method: "DELETE" }).then(detailOf);

/** A 400 invalid_config: the field the hub refused (its dotted path, "" when it names none) and why. */
export function configError(e: unknown): { field: string; message: string } | null {
  if (!(e instanceof ApiError) || e.code !== "invalid_config") return null;
  const field = typeof e.body.field === "string" ? e.body.field : "";
  const message = typeof e.body.message === "string" && e.body.message ? e.body.message : e.message;
  return { field, message };
}

// ── words ───────────────────────────────────────────────

// Running or idle (stopped: started again when something needs it) is all one to the user: it is on.
const STATES: Record<string, string> = {
  running: "启动",
  stopped: "启动",
  starting: "正在启动",
  needs_setup: "需要设置",
  disabled: "停用",
  incompatible: "这台 hub 用不了",
  error: "出错了",
};

export const extensionState = (p: HubExtension) => STATES[p.status] ?? "状态未知";

/** Whether an enabled extension's program runs right now (the detail page's small print); "" for any other state. */
export const runNote = (p: HubExtension) => (p.status === "running" ? "正在运行" : p.status === "stopped" ? "暂未运行，用到时自动启动" : "");

export const isProblem = (p: HubExtension) => p.status === "error" || p.status === "incompatible";

/** The tone of an extension's dot: red broken, orange waiting on the user, green on (running or idle), gray otherwise. */
export function extensionTone(p?: HubExtension | null): "red" | "orange" | "green" | "idle" {
  if (!p) return "idle";
  if (isProblem(p)) return "red";
  if (p.status === "needs_setup") return "orange";
  return p.status === "running" || p.status === "stopped" ? "green" : "idle";
}

export function sourceText(s?: string): string | undefined {
  return s === "builtin" ? "内置" : s === "hub.json" ? "hub.json" : s === "installed" ? "已安装" : undefined;
}

export function pointText(name: string): string {
  return name === "connectors" ? "连接器" : name === "voice" ? "语音对话" : name;
}

/** The detail's line under the name: state · version · source. */
/** The 升级 group's line for a newer version: what happens to it (the App's ExtensionText.upgradeDetail). */
export function upgradeDetail(u: ExtensionUpgrade): string {
  const why = u.detail ? "：" + u.detail : "";
  switch (u.state) {
    case "needs_permission": return morePermissions(u.added_permissions ?? []);
    case "waiting_idle": return "扩展空闲时自动升级";
    case "available": return "稍后自动升级";
    case "off": return "自动升级关着";
    case "skipped": return `上次升级后起不来，已回到 ${u.from}，不再自动升级到这个版本`;
    case "failed": return "没装上" + why;
    case "incompatible": return "这台 hub 装不了" + why;
    default: return "";
  }
}

/** When the line asks something of the user, what its button says (it opens the upgrade in the market). */
export function upgradeAction(u: ExtensionUpgrade): string | null {
  if (u.state === "needs_permission") return "去确认";
  return ["off", "skipped", "failed"].includes(u.state) ? "去升级" : null;
}

/** What a newer version asks for more, as the hub says it in its notice. */
export function morePermissions(added: ExtensionPermission[]): string {
  if (!added.length) return "要的权限有变化";
  const count = ["", "一", "两", "三", "四", "五"];
  const lines = added.map((p) => p.title + (p.detail ? `（${p.detail}）` : ""));
  return (added.length < count.length ? `要多${count[added.length]}项权限：` : `要多 ${added.length} 项权限：`) + lines.join("、");
}

/** The last upgrade, when it is news of its own (asking and failing are the newer version's line). */
export function upgradeNote(n: ExtensionUpgradeNote): string | null {
  if (n.kind === "upgraded") return `已自动升级到 ${n.to}（原来是 ${n.from}）`;
  if (n.kind === "rolled_back") return `${n.to} 起不来，已回到 ${n.from}`;
  return null;
}

export const stateLine = (p: HubExtension) => [extensionState(p), p.version, sourceText(p.source)].filter(Boolean).join(" · ");

/** How many, and how many need the user. */
export function listSummary(list?: HubExtension[] | null): string {
  if (!list) return "";
  if (!list.length) return "没有";
  const attention = list.filter((p) => p.status === "needs_setup" || isProblem(p)).length;
  return attention ? `${list.length} 个 · ${attention} 个要处理` : `${list.length} 个`;
}

export const fieldLabel = (f: ExtensionField) => f.title || f.name;

/** The title with 「（必填）」 when it has to be filled in and has no default to fall back to. */
export const fieldTitle = (f: ExtensionField) => fieldLabel(f) + (f.required && f.default === undefined ? "（必填）" : "");

export const choiceLabel = (c: ExtensionChoice) => c.title || (typeof c.value === "string" ? c.value : JSON.stringify(c.value));

/** What shows when a field is empty: the default it falls back to, or 未设置. */
export function placeholder(f: ExtensionField): string {
  if (f.default !== undefined && f.default !== null) {
    const choice = f.enum?.find((c) => same(c.value, f.default!));
    const text = choice ? choiceLabel(choice) : typeof f.default === "object" ? "" : String(f.default);
    if (text) return `默认：${text}`;
  }
  return "未设置";
}

export const secretState = (s: ExtensionSecret) => (s.set ? "已设置" : s.required ? "未设置（必填）" : "未设置");

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
export function listItems(draft: JsonObject, path: string[], f: ExtensionField): string[] {
  const v = valueAt(draft, path) ?? f.default;
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

/** A machine on an extension's machine list (format machine): online or not, and for an extension that operates computers how
 *  Cua Driver is there (known once it is turned on). */
export function machineStatus(m: Machine, computers: boolean): string {
  const parts = [m.online ? "在线" : "离线"];
  const c = m.computer;
  // Every Mac's while the extension is on, once its node has said (a never-asked one has neither a verdict nor a note).
  if (computers && m.online && c && (c.ready || c.note)) {
    const missing = c.accessibility === "missing" || c.screen_recording === "missing";
    parts.push(!c.installed ? "Cua 没装" : !c.daemon ? "Cua 没在运行" : missing ? "Cua 缺权限" : c.desktop === "locked" ? "锁屏了" : c.ready ? "Cua 可以用" : "Cua 已装");
  }
  return parts.join(" · ");
}

/** How a plugin's program is on a machine switched on for it, for the machine's row: what the user can make of it. */
export function nodeProgramStatus(s: ExtensionNodeState | undefined): string {
  if (!s) return "等 hub 把程序发过去";
  const error = s.error && exitText(s.error);
  switch (s.state) {
    case "running":
      return `程序在运行${s.version ? ` · ${s.version}` : ""}`;
    case "starting":
      return "程序正在启动";
    case "missing":
      return "正在把程序发过去";
    case "offline":
      return "离线：连上后会启动";
    case "lease_expired":
      return "和 hub 断开太久，程序已停，连上后会再启动";
    case "outdated":
      return "这台电脑上的 personad 太旧，更新后才能运行";
    case "error":
      return error ? `程序出错：${error}` : "程序出错";
    default:
      return error ? `${s.state}：${error}` : s.state;
  }
}

/** Go's words for how a program ended (exec.ExitError), in the apps' words: exit status 3 → 退出码 3. */
export function exitText(error: string): string {
  return error.replace(/\bexit status (-?\d+)/g, "退出码 $1").replace(/\bsignal: ([a-z ]+)/g, "被信号结束（$1）");
}

/** Under the form: which switches take effect at once (lists of machines) and that the rest waits for 保存. Nothing
 *  when there is only such switches: the fields say so themselves, nothing waits for 保存. */
export function saveFooter(fields: ExtensionField[]): string | undefined {
  const all = fields.flatMap((f) => (f.type === "object" ? (f.fields ?? []) : [f]));
  const now = all.filter((f) => f.items?.format === "machine").map((f) => `「${f.title || f.name}」`);
  if (!now.length) return "改完点「保存」；保存后扩展会重新启动。留空的项用扩展的默认值。";
  if (now.length === all.length) return undefined;
  return `${now.join("、")}的开关即时生效；其他项改完点「保存」，保存后扩展会重新启动。留空的项用扩展的默认值。`;
}
