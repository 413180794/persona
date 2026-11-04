// 设置 › 模型 › 新增供应商 › 从扩展添加 (docs/extensions.md「模型供应商」, iOS LauncherPresetModel): the model providers the
// hub's extensions offer (GET /api/v1/launchers/presets). A launcher added from one sends only its name, its key, the preset
// and maybe another of the provider's models; the hub fills in the address, the kind and the rest.
import { api, ApiError } from "./api";

export interface PresetModel {
  id: string;
  name?: string;
}

export interface LauncherPreset {
  id: string; // <plugin>/<provider>
  plugin: string;
  name: string;
  description?: string;
  kind: "launcher" | "api";
  api?: "messages" | "responses";
  base_url: string;
  model?: string;
  small_model?: string;
  models?: PresetModel[];
  key_hint?: string;
  models_error?: string;
}

/** The extensions' providers; none from a hub without the extension base (404). */
export async function loadPresets(): Promise<LauncherPreset[]> {
  try {
    return (await api<{ presets?: LauncherPreset[] }>("/launchers/presets")).presets ?? [];
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return [];
    throw e;
  }
}

/** The model a launcher gets when none is picked: the declared default, else the first listed. */
export const presetDefaultModel = (p: LauncherPreset) => p.model || p.models?.[0]?.id || "";

/** What can be picked: the listed models, with the default first when it is not among them. */
export function presetChoices(p: LauncherPreset): PresetModel[] {
  const listed = p.models ?? [];
  const d = presetDefaultModel(p);
  return !d || listed.some((m) => m.id === d) ? listed : [{ id: d }, ...listed];
}

export const presetModelLabel = (m: PresetModel) => m.name || m.id;

/** "直接 API · api.example.com", "Claude Code 启动器 · gateway.example.com". */
export function presetSubtitle(p: LauncherPreset): string {
  const kind = p.kind === "api" ? "直接 API" : "Claude Code 启动器";
  try {
    const host = new URL(p.base_url).host;
    return host ? `${kind} · ${host}` : kind;
  } catch {
    return kind;
  }
}

/** What is wrong with a launcher's name, the way the hub would put it; "" while empty or fine. */
export function launcherNameProblem(name: string): string {
  const n = name.trim();
  if (!n) return "";
  if (!/^[A-Za-z_][A-Za-z0-9_-]*$/.test(n)) return "名字只能用字母、数字、_ 和 -，以字母或 _ 开头";
  if (n === "claude" || n === "presets") return `不能叫 ${n}`;
  return "";
}

/** POST /api/v1/launchers' body: the model only when another one than the preset's default was picked. */
export function presetBody(p: LauncherPreset, name: string, key: string, model: string) {
  const m = model.trim();
  return { name: name.trim(), api_key: key.trim(), preset: p.id, ...(m && m !== presetDefaultModel(p) ? { model: m } : {}) };
}
