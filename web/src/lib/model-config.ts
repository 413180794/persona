import { api, enc } from "./api";

export interface ModelBinding {
  provider?: string;
  model?: string;
  effort?: string;
}

export interface ModelProvider {
  id: string;
  name: string;
  kind: "api" | "launcher" | "plugin";
  api?: "messages" | "responses";
  model?: string;
  machines?: string[];
  hidden?: boolean;
  has_key?: boolean;
}

export interface ModelPurpose {
  id: string;
  name: string;
  group: string;
  kind: "api" | "launcher" | "plugin";
  parent?: string;
  machine_id?: string;
  override: ModelBinding;
  effective: ModelBinding;
  revision: number;
  inherited_from?: string;
  status?: string;
  fields?: string[];
  provider_ids?: string[];
}

export interface ModelConfigurationBody {
  purposes: ModelPurpose[];
  providers: ModelProvider[];
}
export interface ModelPurposeBody {
  purpose: ModelPurpose;
  providers: ModelProvider[];
}
export interface ModelDraft {
  provider: string;
  model: string;
  effort: string;
}

export const modelConfiguration = () => api<ModelConfigurationBody>("/model-config");
export const modelPurpose = (id: string) => api<ModelPurposeBody>(`/model-config/purposes/${enc(id)}`);

export function draftFor(p: ModelPurpose): ModelDraft {
  return { provider: p.override?.provider ?? "", model: p.override?.model ?? "", effort: p.override?.effort ?? "" };
}

export function chooseProvider(draft: ModelDraft, provider: string): ModelDraft {
  return draft.provider === provider ? draft : { provider, model: "", effort: "" };
}

export function supportsField(p: ModelPurpose, field: keyof ModelDraft): boolean {
  return (p.fields ?? ["provider", "model"]).includes(field);
}

export function modelPatch(p: ModelPurpose, draft: ModelDraft): Record<string, string | number | null> {
  const patch: Record<string, string | number | null> = { revision: p.revision };
  for (const field of ["provider", "model", "effort"] as const) {
    if (supportsField(p, field)) patch[field] = draft[field].trim() || null;
  }
  return patch;
}

export function saveModelPurpose(p: ModelPurpose, draft: ModelDraft): Promise<ModelPurposeBody> {
  return api(`/model-config/purposes/${enc(p.id)}`, { method: "PATCH", json: modelPatch(p, draft) });
}

export function providersFor(p: ModelPurpose, providers: ModelProvider[]): ModelProvider[] {
  const candidates = providers.filter((provider) =>
    p.provider_ids ? p.provider_ids.includes(provider.id) : provider.kind === p.kind,
  );
  return candidates.filter((provider) => !provider.hidden || provider.id === p.override?.provider || provider.id === p.effective?.provider);
}

export function modelSummary(p: ModelPurpose, providers: ModelProvider[] = []): string {
  const name = providers.find((v) => v.id === p.effective?.provider)?.name ?? p.effective?.provider;
  return [name, p.effective?.model, p.effective?.effort && `思考 ${p.effective.effort}`].filter(Boolean).join(" · ") || "未配置";
}

export function purposeGroups(purposes: ModelPurpose[], query: string): [string, ModelPurpose[]][] {
  const q = query.trim().toLocaleLowerCase();
  const groups = new Map<string, ModelPurpose[]>();
  for (const p of purposes) {
    if (q && ![p.name, p.group, p.id, modelSummary(p)].some((s) => s.toLocaleLowerCase().includes(q))) continue;
    const group = p.group || "其他";
    groups.set(group, [...(groups.get(group) ?? []), p]);
  }
  return [...groups.entries()];
}
