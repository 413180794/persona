// 设置 › hub (docs/hub-update.md「App 里」, the app's HubUpdateModel / HubUpdateStatus): the hub's version and channel,
// checking for and installing a release, what an update waits for, auto-update, the history; and the assistant's
// skills (GET /api/v1/skills). Pure helpers here; the page is views/settings/HubSettings.tsx.
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
  return channel === "release" ? "发布版" : channel === "docker" ? "Docker" : "开发版";
}

export function updateStateTitle(s: HubUpdateStatus): string {
  switch (s.state) {
    case "checking":
      return "正在检查";
    case "downloading":
      return "正在下载";
    case "waiting_idle":
      return s.busy.length ? `等 ${s.busy.length} 个在忙的结束` : "等助手和 Agent 空闲";
    case "restarting":
      return "正在重启，会断开一会儿";
    case "confirming":
      return "新版本正在确认";
    case "updated":
      return "已更新";
    case "rolled_back":
      return "已回滚";
    case "failed":
      return "没更新成功";
    case "available":
      return "有新版本";
    default:
      return "已是最新";
  }
}

export function recordTitle(result: string): string {
  return result === "updated" ? "已更新" : result === "rolled_back" ? "已回滚" : result === "held" ? "没有自动装" : "没更新成功";
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
  if (!s.can_update) return { kind: "cannot", why: s.why_not ? `这个 hub 不会自己更新：${s.why_not}。` : "这个 hub 不会自己更新。" };
  const working = updateWorking(s);
  if (s.latest?.newer) return { kind: "newer", working };
  return working ? { kind: "working" } : { kind: "current" };
}

/** The warning under a newer release: it went back before, or auto-update holds it back. */
export function latestWarning(s: HubUpdateStatus): string | null {
  const l = s.latest;
  if (!l?.newer) return null;
  if (l.bad) return "这一版上次装完起不来，已经回滚过；自动更新不会再装它。";
  if (l.held && s.auto) return `自动更新不会装这一版：${l.held}。确认没问题可以手动更新。`;
  return null;
}

/** The lines for a Docker hub: what to say and what to copy. */
export function dockerSteps(s: HubUpdateStatus): { headline?: string; steps: { text: string; copy: string }[] } {
  const l = s.latest;
  if (l?.newer) {
    if (s.image?.pinned) {
      return {
        headline: `新镜像 ${l.version}`,
        steps: [
          { text: "在 hub 所在的机器上，把 compose 文件旁边 .env 里的 JARVIS_IMAGE 改成：", copy: `${s.image.name}:${l.version}` },
          { text: "再在那个目录里运行：", copy: DOCKER_COMMAND },
        ],
      };
    }
    return { headline: `新镜像 ${l.version}`, steps: [{ text: "在 hub 所在的机器上，compose 文件所在的目录里运行：", copy: DOCKER_COMMAND }] };
  }
  return { steps: [{ text: "Docker 部署的 hub 由新镜像更新：有新镜像时在 compose 文件所在的目录里运行", copy: DOCKER_COMMAND }] };
}
