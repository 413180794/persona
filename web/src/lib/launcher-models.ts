// 新建 Agent's model picker (#794): the models a launcher's agent lists in its own /model menu
// (GET /machines/{mid}/launchers/{name}/models, node/launcher_models.go), which launcher the picker asks, searching the
// list, the thinking efforts a pick allows, and what a start sends. A pick is for one start only: nothing is saved.
import { api, ApiError, enc } from "./api";
import type { ModelBinding } from "./model-config";
import { EFFORTS } from "./session-actions";
import type { Launcher } from "./types";

export interface AgentModel {
  /** What a start sends as `model` (Claude Code's aliases too: opus, sonnet…). */
  id: string;
  name: string;
  description?: string;
  /** The concrete model an alias stands for with this launcher's settings. */
  resolved?: string;
  /** What the agent runs when no model is given: picking it is the same as picking none. */
  default?: boolean;
  /** Thinking efforts it takes, weakest first; none: no effort setting. */
  efforts?: string[];
  default_effort?: string;
}

export interface LauncherModels {
  /** What the launcher runs: claude, codex, traex, a runtime's agent. */
  agent: string;
  /** Empty: this agent has no list (no picker). */
  models: AgentModel[];
  /** Why a pick would not reach the agent (the launcher fixes its own --model); the start sends no model then. */
  locked?: string;
}

/** The list for `launcher` on `mid` (seconds when the node has to start the CLI); null from an older hub (no picker). */
export async function launcherModels(mid: string, launcher: string, opts: { refresh?: boolean; signal?: AbortSignal } = {}): Promise<LauncherModels | null> {
  try {
    const r = await api<LauncherModels>(`/machines/${enc(mid)}/launchers/${enc(launcher)}/models${opts.refresh ? "?refresh=1" : ""}`, { signal: opts.signal });
    return { ...r, models: r.models ?? [] };
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

/** 模型配置's list for a launcher provider (`launcher.<name>`, GET /model-config/providers/{id}/models, #943): asked on a
 *  machine that has the launcher, which `machine` names. null: an older hub or a provider without a list (no picker). */
export async function providerModels(provider: string, opts: { refresh?: boolean; machine?: string; signal?: AbortSignal } = {}): Promise<(LauncherModels & { machine?: string }) | null> {
  // machine: the purpose's own (a task, 飞书 /agent run there), so the list is that machine's launcher.
  const query = new URLSearchParams();
  if (opts.refresh) query.set("refresh", "1");
  if (opts.machine) query.set("machine", opts.machine);
  try {
    const r = await api<LauncherModels & { machine?: string }>(`/model-config/providers/${enc(provider)}/models${query.size ? `?${query}` : ""}`, { signal: opts.signal });
    return { ...r, models: r.models ?? [] };
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

/** What is typed in the search, as an id to save when no model has exactly that id (模型配置 takes ids the list lacks). */
export function customModel(models: AgentModel[], query: string): string {
  const id = query.trim();
  return id && !/\s/.test(id) && !models.some((m) => m.id === id) ? id : "";
}

/** The 模型配置 binding of 新建 Agent that still applies with the chosen launcher: the hub keeps its model and effort
 *  unless another launcher is picked (hub applyModel). Not following 模型配置: nothing. */
export function followedBinding(chosen: string, effective: ModelBinding | null | undefined): ModelBinding {
  if (!effective?.provider || (chosen && `launcher.${chosen}` !== effective.provider)) return {};
  return effective;
}

/** The launcher whose list the picker shows: the chosen one, else the one 模型配置 runs (`launcher.<name>`); "": none. */
export function pickerLauncher(chosen: string, followed: ModelBinding): string {
  if (chosen) return chosen;
  const p = followed.provider ?? "";
  return p.startsWith("launcher.") ? p.slice("launcher.".length) : "";
}

/** What the launcher runs: the list's answer, else what the launcher list says (Claude Code when it says nothing). */
export function launcherAgent(name: string, launchers: Launcher[], list?: LauncherModels | null): string {
  if (list?.agent) return list.agent;
  const l = launchers.find((x) => x.name === name);
  return l?.agent_type || l?.agent || "claude";
}

/** Only Claude Code starts with a thinking effort; Codex and the rest keep their own. */
export const takesEffort = (agent: string) => agent === "claude";

export function defaultModel(models: AgentModel[]): AgentModel | undefined {
  return models.find((m) => m.default);
}

export const modelName = (m: AgentModel) => m.name || m.id;

/** 「默认」's second line: what 模型配置 sets, else the concrete model the default entry stands for. */
export function defaultDetail(models: AgentModel[], followed: ModelBinding): string {
  if (followed.model) return followed.model;
  const d = defaultModel(models);
  if (!d) return "";
  return d.resolved || (d.id === "default" ? "" : d.id);
}

/** A row's second line: the concrete model, else the id when it says more than the name. */
export function modelDetail(m: AgentModel): string {
  if (m.resolved && m.resolved !== m.name) return m.resolved;
  return m.id.toLowerCase() !== modelName(m).toLowerCase() ? m.id : "";
}

function matches(fields: (string | undefined)[], query: string): boolean {
  const text = fields.filter(Boolean).join("\n").toLocaleLowerCase();
  return query.toLocaleLowerCase().split(/\s+/).filter(Boolean).every((word) => text.includes(word));
}

/** What the open list shows for `query` (name, id, resolved model, description; every word): 「默认」 first when it
 *  matches, then the rest in the agent's order. */
export function modelRows(models: AgentModel[], query: string, followed: ModelBinding = {}): { showDefault: boolean; rows: AgentModel[] } {
  const d = defaultModel(models);
  const showDefault = !query.trim() || matches(["默认", "default", defaultDetail(models, followed), d?.name, d?.id, d?.resolved, d?.description], query);
  const rows = models.filter((m) => m !== d && matches([m.name, m.id, m.resolved, m.description], query));
  return { showDefault, rows };
}

const ALL_EFFORTS = EFFORTS.map((e) => e.id);

/** The efforts the control offers: the picked model's, with none picked the one that runs (模型配置's, else the
 *  default entry's). No list to go by (loading, failed, older hub), or 模型配置's model not in it under that id: all
 *  of them. */
export function effortChoices(list: LauncherModels | null | undefined, model: string, followed: ModelBinding = {}): string[] {
  if (!list?.models.length) return ALL_EFFORTS;
  const id = model || followed.model;
  const m = list.models.find((x) => x.id === id) ?? (id ? undefined : defaultModel(list.models));
  return m ? (m.efforts ?? []) : ALL_EFFORTS;
}

/** The effort kept when the choices change: one they no longer offer goes back to 「跟随启动器」. */
export function fitEffort(effort: string, choices: string[]): string {
  return choices.includes(effort) ? effort : "";
}

/** What a start sends: the model only when it is not the default and the launcher lets it through, the effort only to
 *  Claude Code and only one the model takes. */
export function startPicks(o: { agent: string; list: LauncherModels | null | undefined; model: string; effort: string; followed?: ModelBinding }): { model?: string; effort?: string } {
  const out: { model?: string; effort?: string } = {};
  const m = o.list?.models.find((x) => x.id === o.model);
  if (m && !m.default && !o.list?.locked) out.model = m.id;
  if (o.effort && takesEffort(o.agent) && effortChoices(o.list, out.model ?? "", o.followed).includes(o.effort)) out.effort = o.effort;
  return out;
}
