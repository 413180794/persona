// 操控电脑 on a Mac's page (docs/computer-use.md, iOS ComputerSection): what the hub says the Mac has of Cua Driver
// (GET/PUT /api/v1/machines/{mid}/computer, POST …/computer/{install|grant|cancel}), and what the section shows for it.

export interface ComputerJob {
  /** install | grant */
  kind: string;
  /** running | done | failed | cancelled */
  state: string;
  step?: string;
  error?: string;
  started?: string;
}

export interface MachineComputer {
  enabled: boolean;
  installed?: boolean;
  version?: string;
  daemon?: boolean;
  /** granted | missing | unknown */
  accessibility?: string;
  screen_recording?: string;
  /** active | locked | away */
  desktop?: string;
  screen_sharing?: boolean;
  ready?: boolean;
  note?: string;
  checked?: string;
  job?: ComputerJob;
}

export type ComputerAction = "install" | "grant" | "cancel";

/** The macOS permissions it still lacks, by their names in System Settings. */
export function missingPermissions(c: MachineComputer): string[] {
  const out: string[] = [];
  if (c.accessibility === "missing") out.push("辅助功能");
  if (c.screen_recording === "missing") out.push("屏幕录制");
  return out;
}

/** macOS shows its permission dialogs on the Mac itself. */
export const GRANT_HINT = "那台 Mac 上弹出了授权窗口：在它的屏幕上点「允许」";
export const NO_SCREEN_SHARING = "在那台 Mac 的「系统设置 › 通用 › 共享」里打开「屏幕共享」，这里就能看它的画面、远程点「允许」。";

/** What the section shows below the switch (nothing while it is off). */
export type ComputerView =
  | { kind: "off" }
  | { kind: "offline" }
  | { kind: "running"; text: string; cancel: boolean; screen: boolean }
  | { kind: "install"; text: string; retry: boolean }
  | { kind: "granted"; screen: boolean } // the grant's dialogs are up on the Mac
  | { kind: "grant"; text: string }
  | { kind: "ready"; text: string; screen: boolean }
  | { kind: "unknown"; text: string };

export function computerView(c: MachineComputer, online: boolean): ComputerView {
  if (!c.enabled) return { kind: "off" };
  if (!online) return { kind: "offline" };
  const job = c.job;
  const screen = !!c.screen_sharing;
  if (job?.state === "running") {
    const grant = job.kind === "grant";
    return { kind: "running", text: grant ? GRANT_HINT : job.step || "正在安装", cancel: job.kind === "install", screen: grant && screen };
  }
  if (!c.installed) {
    const failed = job?.kind === "install" && job.state === "failed";
    return { kind: "install", text: failed ? `没装上：${job.error || "安装器出错了"}` : "这台 Mac 上还没有装 Cua Driver", retry: job?.state === "failed" };
  }
  const missing = missingPermissions(c);
  if (c.daemon && missing.length) {
    if (job?.kind === "grant" && job.state === "done") return { kind: "granted", screen };
    const failed = job?.kind === "grant" && job.state === "failed" ? job.error : undefined;
    return { kind: "grant", text: failed || `Cua Driver 还没有${missing.join("、")}权限` };
  }
  if (c.ready) return { kind: "ready", text: "现在可以操控" + (c.version ? ` · Cua Driver ${c.version}` : ""), screen };
  return { kind: "unknown", text: c.note || "还不知道能不能操控" };
}

/** Asked again every few seconds: a job runs, or macOS's dialogs wait on the Mac after a grant. */
export function computerWatching(c: MachineComputer, online: boolean): boolean {
  if (!online || !c.enabled || !c.job) return false;
  return c.job.state === "running" || (c.job.kind === "grant" && c.job.state === "done" && missingPermissions(c).length > 0);
}

/** The web's live screen of the machine (web/src/screen): a signed-in browser opens it with its own token. */
export function screenPath(mid: string): string {
  return `/screen/${encodeURIComponent(mid)}`;
}
