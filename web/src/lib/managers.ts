// 设置 › 管理者 (docs/managers.md, docs/protocol.md「管理者」): the hub's roster of managers (each a whole assistant),
// making one (a one-time install code, and the install commands when the hub gives them), renaming, the default,
// installing one elsewhere and removing one. The hub pushes {"type":"managers"} when the roster changes to a page
// that declared the feature `managers` (CLIENT_FEATURES).
import { t } from "../i18n";
import { api, ApiError, enc, errorText } from "./api";
import { ago } from "./format";

export interface Manager {
  id: string;
  name: string;
  online: boolean;
  default?: boolean;
  /** The assistant the hub runs itself. */
  builtin?: boolean;
  host?: string;
  os?: string;
  arch?: string;
  /** How it runs, as it reported: "child" (the hub's own), "service", "docker", "external"… */
  deploy?: string;
  version?: string;
  /** Connected since (online only). */
  since?: string;
  /** Last connected (offline: 「最后同步于…」). */
  last_seen?: string;
  /** Not installed yet: when its install code runs out. */
  installing?: string;
}

export interface ManagerList {
  managers: Manager[];
  default: string;
}

export interface InstallCode {
  code: string;
  expires_at: string;
  /** The install commands, from a hub that gives them; otherwise only the code. */
  commands?: { service?: string; docker?: string };
}

export const listManagers = () => api<ManagerList>("/managers");
export const createManager = (name: string) => api<InstallCode & { manager: Manager }>("/managers", { method: "POST", json: { name } });
export const newInstallCode = (id: string) => api<InstallCode>(`/managers/${enc(id)}/code`, { method: "POST" });
export const renameManager = (id: string, name: string) => api<{ manager: Manager }>(`/managers/${enc(id)}`, { method: "PATCH", json: { name } });
export const makeDefault = (id: string) => api<{ manager: Manager }>(`/managers/${enc(id)}`, { method: "PATCH", json: { default: true } });
export const deleteManager = (id: string) => api<{ ok: boolean }>(`/managers/${enc(id)}`, { method: "DELETE" });

/** Names are 1–40 characters (hub/managers.go managerName). */
export const NAME_MAX = 40;

export function validName(name: string): boolean {
  const n = [...name.trim()].length;
  return n > 0 && n <= NAME_MAX;
}

/** Whether a hub without the roster (from before it) answered. */
export function unsupported(e: unknown): boolean {
  return e instanceof ApiError && e.status === 404 && !e.code;
}

const OS: Record<string, string> = { darwin: "macOS", linux: "Linux", windows: "Windows" };

/** How it runs, in plain words. */
export function deployText(m: Pick<Manager, "builtin" | "deploy">): string {
  if (m.builtin || m.deploy === "child") return t("settings-managers.deploy_hub");
  switch (m.deploy) {
    case undefined:
    case "":
      return "";
    case "service":
    case "systemd":
    case "launchd":
      return t("settings-managers.deploy_service");
    case "docker":
    case "container":
      return t("settings-managers.deploy_container");
    case "external":
      return t("settings-managers.deploy_external");
    default:
      return m.deploy;
  }
}

/** Where it runs: host · OS · how. Empty for one not installed yet. */
export function whereText(m: Manager): string {
  const os = m.os ? (OS[m.os] ?? m.os) : "";
  return [m.host, os, deployText(m)].filter(Boolean).join(" · ");
}

/** Minutes the install code has left (to the nearest, at least 1 while it lasts: a fresh code says 10), 0 once it ran out. */
export function minutesLeft(until: string, now = Date.now()): number {
  const ms = Date.parse(until) - now;
  return Number.isFinite(ms) && ms > 0 ? Math.max(1, Math.round(ms / 60_000)) : 0;
}

export type Tone = "green" | "grey" | "accent";

/** The status line under the name and its dot's color. */
export function statusLine(m: Manager, now = Date.now()): { text: string; tone: Tone } {
  if (m.installing) {
    const n = minutesLeft(m.installing, now);
    return { text: n > 0 ? t("settings-managers.installing", { count: n }) : t("settings-managers.installing_expired"), tone: "accent" };
  }
  if (m.online) return { text: t("settings-managers.online"), tone: "green" };
  const when = ago(m.last_seen, now);
  return { text: when ? t("settings-managers.offline_since", { time: when }) : t("settings-managers.offline"), tone: "grey" };
}

/** Why 删除 is off for it, or "" when it can be removed. */
export function deleteBlocked(m: Manager): string {
  if (m.builtin) return t("settings-managers.delete_builtin");
  if (m.default) return t("settings-managers.delete_default");
  return "";
}

/** A failed change, in plain words. */
export function managerError(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case "default_manager":
        return t("settings-managers.err_default");
      case "builtin_manager":
        return t("settings-managers.err_builtin");
      case "no_manager":
        return t("settings-managers.err_gone");
      case "installing":
        return t("settings-managers.err_installing");
    }
    if (e.status === 400) return t("settings-managers.err_name");
    if (e.status === 403) return t("settings-managers.err_forbidden");
  }
  return errorText(e);
}
