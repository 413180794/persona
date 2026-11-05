// 设置 › hub (docs/hub-update.md「App 里」, the app's HubUpdateModel / HubUpdateStatus): the hub's version and channel,
// checking for and installing a release, what an update waits for, auto-update, the history; and the assistant's
// skills (GET /api/v1/skills). Pure helpers here; the page is views/settings/HubSettings.tsx.
import { t } from "../i18n";
import { api } from "./api";

export interface BusyItem {
  machine_id: string;
  machine: string;
  session_id?: string;
  title: string;
}

export interface HubUpdateStatus {
  version: string;
  build: number;
  channel: string; // release | docker | dev
  can_update: boolean;
  why_not?: string;
  latest?: {
    version: string;
    build: number;
    released?: string;
    notes?: string;
    newer: boolean;
    /** Went back before: not installed automatically. */
    bad: boolean;
    /** Why auto-update does not install this newer one. */
    held?: string;
  };
  state: string;
  progress?: string;
  busy: BusyItem[];
  error?: string;
  auto: boolean;
  auto_notice: boolean;
  last_check?: string;
  confirmed: boolean;
  image?: { name: string; tag: string; pinned: boolean };
}

export interface HubUpdateRecord {
  from_version: string;
  to_version: string;
  to_build: number;
  started?: string;
  ended?: string;
  result: string; // updated | rolled_back | failed | held
  reason?: string;
  auto: boolean;
}

export interface Skill {
  name: string;
  description?: string;
  source: string; // user | builtin
}

export const hubUpdate = () => api<HubUpdateStatus>("/hub/update");
export const checkHubUpdate = () => api<HubUpdateStatus>("/hub/update/check", { method: "POST" });
/** `now`: don't wait for the assistant and the agents to be idle. */
export const startHubUpdate = (now: boolean) => api<HubUpdateStatus>("/hub/update", { method: "POST", json: now ? { now: true } : {} });
export const setHubUpdateSettings = (s: { auto?: boolean; auto_notice_seen?: boolean }) => api<HubUpdateStatus>("/hub/update/settings", { method: "PUT", json: s });
export const hubUpdateHistory = () => api<{ history: HubUpdateRecord[] | null }>("/hub/update/history").then((r) => r.history ?? []);
export const skills = () => api<{ machine_id: string; machine?: string; skills: Skill[] | null }>("/skills");

export const DOCKER_COMMAND = "docker compose pull && docker compose up -d";

/** An update is under way (the progress row, no buttons). */
export function updateWorking(s: HubUpdateStatus): boolean {
  return ["downloading", "waiting_idle", "restarting", "confirming"].includes(s.state);
}

/** A state that ends an update: the history has a new line. */
export function updateFinished(prev: string | undefined, next: string): boolean {
  return prev !== next && ["updated", "rolled_back", "failed"].includes(next);
}

export function channelTitle(channel: string): string {
  return channel === "release" ? t("lib-hub-update.channel_release") : channel === "docker" ? "Docker" : t("lib-hub-update.channel_dev");
}

export function updateStateTitle(s: HubUpdateStatus): string {
  switch (s.state) {
    case "checking":
      return t("lib-hub-update.state_checking");
    case "downloading":
      return t("lib-hub-update.state_downloading");
    case "waiting_idle":
      return s.busy.length ? t("lib-hub-update.state_waiting_busy", { count: s.busy.length }) : t("lib-hub-update.state_waiting_idle");
    case "restarting":
      return t("lib-hub-update.state_restarting");
    case "confirming":
      return t("lib-hub-update.state_confirming");
    case "updated":
      return t("lib-hub-update.result_updated");
    case "rolled_back":
      return t("lib-hub-update.result_rolled_back");
    case "failed":
      return t("lib-hub-update.result_failed");
    case "available":
      return t("lib-hub-update.state_available");
    default:
      return t("lib-hub-update.state_current");
  }
}

export function recordTitle(result: string): string {
  return t(result === "updated" ? "lib-hub-update.result_updated" : result === "rolled_back" ? "lib-hub-update.result_rolled_back" : result === "held" ? "lib-hub-update.result_held" : "lib-hub-update.result_failed");
}

export function recordTone(result: string): "green" | "orange" | "red" {
  return result === "updated" ? "green" : result === "rolled_back" || result === "held" ? "orange" : "red";
}

/** What the 更新 group shows: a Docker hub's instructions, why it doesn't update itself, a newer release, an update
 *  under way, or 已是最新. */
export type UpdateView =
  | { kind: "docker" }
  | { kind: "cannot"; why: string }
  | { kind: "newer"; working: boolean }
  | { kind: "working" }
  | { kind: "current" };

export function updateView(s: HubUpdateStatus): UpdateView {
  if (s.channel === "docker") return { kind: "docker" };
  if (!s.can_update) return { kind: "cannot", why: s.why_not ? t("lib-hub-update.cannot_update_why", { why: s.why_not }) : t("lib-hub-update.cannot_update") };
  const working = updateWorking(s);
  if (s.latest?.newer) return { kind: "newer", working };
  return working ? { kind: "working" } : { kind: "current" };
}

/** The warning under a newer release: it went back before, or auto-update holds it back. */
export function latestWarning(s: HubUpdateStatus): string | null {
  const l = s.latest;
  if (!l?.newer) return null;
  if (l.bad) return t("lib-hub-update.warning_bad");
  if (l.held && s.auto) return t("lib-hub-update.warning_held", { why: l.held });
  return null;
}

/** The lines for a Docker hub: what to say and what to copy. */
export function dockerSteps(s: HubUpdateStatus): { headline?: string; steps: { text: string; copy: string }[] } {
  const l = s.latest;
  if (l?.newer) {
    if (s.image?.pinned) {
      return {
        headline: t("lib-hub-update.new_image", { version: l.version }),
        steps: [
          { text: t("lib-hub-update.docker_set_image"), copy: `${s.image.name}:${l.version}` },
          { text: t("lib-hub-update.docker_then_run"), copy: DOCKER_COMMAND },
        ],
      };
    }
    return { headline: t("lib-hub-update.new_image", { version: l.version }), steps: [{ text: t("lib-hub-update.docker_run"), copy: DOCKER_COMMAND }] };
  }
  return { steps: [{ text: t("lib-hub-update.docker_how"), copy: DOCKER_COMMAND }] };
}
