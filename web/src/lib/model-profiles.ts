import { api, enc } from "./api";
import type { ModelBinding } from "./model-config";

export interface ModelProfile {
  id: string;
  name: string;
  description?: string;
  purposes?: Record<string, ModelBinding>;
  launchers?: Record<string, Record<string, string>>;
  tiers?: Record<string, { launcher: string; model?: string; effort?: string }>;
  active?: boolean;
  changes: string[];
}
export interface ModelProfiles {
  revision: number;
  active?: string;
  profiles: ModelProfile[];
  rollback?: { to?: string; changes: string[] };
}
export const modelProfiles = () => api<ModelProfiles>("/model-profiles");
export const switchModelProfile = (id: string) => api<ModelProfiles>(`/model-profiles/${enc(id)}/apply`, { method: "POST" });
export const rollbackModelProfile = () => api<ModelProfiles>("/model-profiles/rollback", { method: "POST" });
