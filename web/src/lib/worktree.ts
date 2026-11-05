// 新建 worktree… (#2431): the dialog's logic. A workspace's repository is read on the computer (worktree/prepare), the
// branch and the checkout path are typed, and the computer makes the worktree and opens it as a workspace
// (worktree/create). The path follows the branch until it is edited by hand, the way Moshi's dialog does. Both routes
// are for a full device only (server/internal/hub/panes.go).
import { t as tr } from "../i18n";
import { api, ApiError, enc, errorText } from "./api";
import type { MenuNode } from "./node-actions";
import { navigate } from "./router";

/** What worktree/prepare says of the workspace's repository (server/internal/proto/panes.go WorktreePrepare). */
export interface WorktreePrepare {
  root: string;
  repoName: string;
  /** The branch checked out in the workspace; "" on a detached HEAD. */
  branch: string;
  /** The directory the computer puts this repository's checkouts under; a checkout of a branch is `path/<slug>`. */
  path: string;
}

/** What worktree/create opened. */
export interface WorktreeCreated {
  workspace_id: string;
  tab_id?: string;
  pane?: string;
  /** The terminal to open ("pane:<id>") when the workspace has a pane. */
  terminal?: string;
  path: string;
  branch: string;
}

export const WORKTREE_COPY = {
  reading: tr("worktree.reading"),
  branchRequired: tr("worktree.branch_required"),
  pathRequired: tr("worktree.path_required"),
  failed: tr("worktree.failed"),
  needsUpdate: tr("worktree.needs_update"),
  readFailed: tr("worktree.read_failed"),
};

/** The directory name herdr gives a branch's checkout: lower case, every run of characters other than ASCII letters and
 *  digits one "-", none at either end ("Feat/A_b.c" is "feat-a-b-c"). The same rule as the node's WorktreeSlug. */
export function branchSlug(branch: string): string {
  return branch.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

/** The checkout path a branch gets under `base` ("" when the branch has no letters or digits to name a folder with). */
export function suggestedPath(base: string, branch: string): string {
  const slug = branchSlug(branch);
  if (!base || !slug) return "";
  const sep = base.includes("\\") && !base.includes("/") ? "\\" : "/";
  return `${base.replace(/[\\/]+$/, "")}${sep}${slug}`;
}

/** Why git would not take this name for a branch, or "" when it would (git check-ref-format --branch). The computer
 *  checks again; this is so a typo is told as it is typed. */
export function branchProblem(raw: string): string {
  const b = raw.trim();
  if (!b) return WORKTREE_COPY.branchRequired;
  if (b.startsWith("-")) return tr("worktree.branch_dash");
  if (b === "@" || /[\s~^:?*[\\\u0000-\u001f\u007f]/.test(b)) return tr("worktree.branch_chars");
  if (b.includes("..") || b.includes("@{") || b.includes("//")) return tr("worktree.branch_pattern");
  if (b.startsWith("/") || b.endsWith("/") || b.endsWith(".") || b.endsWith(".lock")) return tr("worktree.branch_edges");
  if (b.split("/").some((part) => part.startsWith(".") || part.endsWith(".lock"))) return tr("worktree.branch_parts");
  return "";
}

/** The form: the branch and the path, and whether the path was put in by hand (then the branch stops moving it). */
export interface WorktreeForm {
  branch: string;
  path: string;
  pathEdited: boolean;
}

export const emptyForm: WorktreeForm = { branch: "", path: "", pathEdited: false };

/** The branch was typed: the path follows it, unless it was edited by hand. */
export function typeBranch(form: WorktreeForm, base: string, branch: string): WorktreeForm {
  return { ...form, branch, path: form.pathEdited ? form.path : suggestedPath(base, branch.trim()) };
}

/** The path was typed. Emptied, it goes back to following the branch. */
export function typePath(form: WorktreeForm, base: string, path: string): WorktreeForm {
  if (path === "") return { ...form, path: suggestedPath(base, form.branch.trim()), pathEdited: false };
  return { ...form, path, pathEdited: true };
}

/** What stops the form being sent, "" when nothing does. */
export function formProblem(form: WorktreeForm): string {
  return branchProblem(form.branch) || (form.path.trim() ? "" : WORKTREE_COPY.pathRequired);
}

/** The error of a worktree call as the dialog says it: an older hub or computer that has no such call is told to update. */
export function worktreeProblem(e: unknown, what: string): { text: string; needsUpdate: boolean } {
  const message = errorText(e);
  const old =
    e instanceof ApiError
      ? e.status === 404 || e.status === 405 || e.status === 501 || e.code === "unknown_method" || /unknown method|^unsupported/i.test(message)
      : false;
  if (old) return { text: WORKTREE_COPY.needsUpdate, needsUpdate: true };
  return { text: message ? tr("worktree.failed_with", { what, message }) : what, needsUpdate: false };
}

export interface WorktreeTarget {
  mid: string;
  workspaceId: string;
  /** The host kind of the workspace ("herdr"); unset: the computer's own. */
  host?: string;
  label?: string;
}

const kindOf = (t: WorktreeTarget) => (t.host ? { kind: t.host } : {});

export function prepareWorktree(t: WorktreeTarget): Promise<WorktreePrepare> {
  return api<WorktreePrepare>(`/machines/${enc(t.mid)}/worktree/prepare`, { method: "POST", json: { workspace_id: t.workspaceId, ...kindOf(t) } });
}

export function createWorktree(t: WorktreeTarget, form: WorktreeForm): Promise<WorktreeCreated> {
  return api<WorktreeCreated>(`/machines/${enc(t.mid)}/worktree/create`, {
    method: "POST",
    json: { workspace_id: t.workspaceId, branch: form.branch.trim(), path: form.path.trim(), ...kindOf(t) },
  });
}

/** Opens the new workspace on the 终端 page (its first pane, else the machine's list). */
export function openCreated(mid: string, created: WorktreeCreated) {
  navigate(created.terminal ? { name: "terminal", mid, id: created.terminal } : { name: "terminal", mid });
}

// ── opening the dialog from a menu or the palette ────────

export interface WorktreeRequest {
  target: WorktreeTarget;
  /** The computer's tree changed (the new workspace is in it): read it again. */
  changed: () => void;
}

const openers = new Set<(r: WorktreeRequest) => void>();

/** Opens 新建 worktree… (the app shell listens) for a workspace node. */
export function openNewWorktree(node: Pick<MenuNode, "mid" | "workspaceId" | "host" | "label">, changed: () => void = () => {}) {
  if (!node.workspaceId) return;
  const request = { target: { mid: node.mid, workspaceId: node.workspaceId, host: node.host, label: node.label }, changed };
  openers.forEach((l) => l(request));
}

export function onOpenNewWorktree(l: (r: WorktreeRequest) => void) {
  openers.add(l);
  return () => {
    openers.delete(l);
  };
}
