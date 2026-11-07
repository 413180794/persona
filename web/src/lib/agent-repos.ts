import { byAttention, stateOf } from "./format";
import type { Machine, Session } from "./types";

/** Where a session's cwd sits: the repository it belongs to and the working tree inside it. */
export type Repo = { repo: string; name: string; worktree: string; main: boolean };

export type RepoWorktree = { key: string; name: string; path: string; main: boolean; sessions: Session[] };

/** One repository on one machine, with its working trees (the main checkout first, then by name). */
export type RepoTree = {
  key: string;
  machineId: string;
  name: string;
  repo: string;
  count: number;
  worktrees: RepoWorktree[];
};

/** One computer's section in the repository tree: its live repositories ordered by name. */
export interface RepoMachine {
  key: string;
  machine: Machine;
  foldKey: string;
  repos: RepoTree[];
  sessions: Session[];
  needs: number;
}

// The layouts a working tree commonly lives in (herdr, Claude Code, a home-level collection): the parts before the
// last one name the repository, everything from there on is the tree.
const WORKTREE_SEGMENTS = [["/.worktrees/"], ["/.claude/worktrees/"], ["/.herdr/worktrees/"]];

const segments = (path: string) => path.split("/").filter(Boolean);
const baseName = (path: string) => segments(path).pop() ?? "";

/**
 * The repository and working tree a cwd belongs to. The host may name the repository (the session's repo_root, #2840:
 * Paseo keeps its worktrees under its own folder): a cwd inside it is its main checkout, any other is one of its
 * working trees. Otherwise from the path alone (docs/web/web.md): a tree under one of the known layouts belongs to the
 * directory above that marker; any other path is its own repository's main checkout.
 */
export function repoOf(cwd: string, repoRoot?: string): Repo {
  if (!cwd) return { repo: "", name: "", worktree: "", main: true };
  const path = cwd.replace(/\/+$/, "");
  const root = (repoRoot ?? "").replace(/\/+$/, "");
  if (root) {
    const inside = path === root || path.startsWith(`${root}/`);
    return { repo: root, name: baseName(root), worktree: inside ? root : path, main: inside };
  }
  for (const [marker] of WORKTREE_SEGMENTS) {
    const at = path.indexOf(marker);
    if (at <= 0) continue;
    const rest = path.slice(at + marker.length).split("/").filter(Boolean);
    const repo = path.slice(0, at);
    // ~/.worktrees/<repo>/<tree>: the marker sits at the home level, so the repository is the first part after it and
    // the tree is what follows.
    if (rest.length >= 2) return { repo: `${repo}${marker}${rest[0]}`, name: rest[0]!, worktree: path, main: false };
    return { repo, name: baseName(repo), worktree: path, main: false };
  }
  return { repo: path, name: baseName(path), worktree: path, main: true };
}

const worktreeName = (path: string, repo: string) => (path === repo ? baseName(repo) : baseName(path));

/**
 * The sidebar's repository tree: every live agent under the repository its cwd names, that repository's working trees
 * under it. Repositories sort by name (then machine), a tree's main checkout comes first, the rest by name; agents
 * inside a tree keep the sidebar's own order (needs-you first).
 */
export function buildRepoTree(all: Session[], machines: Machine[]): RepoTree[] {
  const online = new Set(machines.filter((m) => m.online).map((m) => m.id));
  const repos = new Map<string, RepoTree>();
  for (const s of all) {
    if (s.assistant || !online.has(s.machine_id)) continue;
    const where = repoOf(s.cwd ?? "", s.repo_root);
    const key = `${s.machine_id}\u0000${where.repo}`;
    let tree = repos.get(key);
    if (!tree) {
      tree = { key, machineId: s.machine_id, name: where.name, repo: where.repo, count: 0, worktrees: [] };
      repos.set(key, tree);
    }
    let worktree = tree.worktrees.find((w) => w.path === where.worktree);
    if (!worktree) {
      worktree = { key: `${key}\u0000${where.worktree}`, name: worktreeName(where.worktree, where.repo), path: where.worktree, main: where.main, sessions: [] };
      tree.worktrees.push(worktree);
    }
    worktree.sessions.push(s);
    tree.count++;
  }
  const order = (m: string) => machines.findIndex((x) => x.id === m);
  return [...repos.values()]
    .map((tree) => {
      const worktrees = tree.worktrees
        .map((w) => ({ ...w, sessions: w.sessions.sort(byAttention) }))
        .sort((a, b) => Number(b.main) - Number(a.main) || a.name.localeCompare(b.name));
      return { ...tree, worktrees };
    })
    .sort((a, b) => a.name.localeCompare(b.name) || order(a.machineId) - order(b.machineId));
}

/**
 * The sidebar's repository tree partitioned by computer (#2108 / #2126): each computer with live agents gets its own
 * section, kept in the hub's machine order; repositories within each computer sort alphabetically by name.
 */
export function buildRepoMachineTree(all: Session[], machines: Machine[]): RepoMachine[] {
  const online = new Set(machines.filter((m) => m.online).map((m) => m.id));
  const byMachine = new Map<string, Session[]>();
  for (const s of all) {
    if (s.assistant || !online.has(s.machine_id)) continue;
    let list = byMachine.get(s.machine_id);
    if (!list) {
      list = [];
      byMachine.set(s.machine_id, list);
    }
    list.push(s);
  }

  const out: RepoMachine[] = [];
  for (const m of machines) {
    const sessions = byMachine.get(m.id);
    if (!sessions || sessions.length === 0) continue;
    const repos = new Map<string, RepoTree>();
    for (const s of sessions) {
      const where = repoOf(s.cwd ?? "", s.repo_root);
      const key = `${m.id}\u0000${where.repo}`;
      let tree = repos.get(key);
      if (!tree) {
        tree = { key, machineId: m.id, name: where.name, repo: where.repo, count: 0, worktrees: [] };
        repos.set(key, tree);
      }
      let worktree = tree.worktrees.find((w) => w.path === where.worktree);
      if (!worktree) {
        worktree = { key: `${key}\u0000${where.worktree}`, name: worktreeName(where.worktree, where.repo), path: where.worktree, main: where.main, sessions: [] };
        tree.worktrees.push(worktree);
      }
      worktree.sessions.push(s);
      tree.count++;
    }
    const repoTrees = [...repos.values()]
      .map((tree) => {
        const worktrees = tree.worktrees
          .map((w) => ({ ...w, sessions: w.sessions.sort(byAttention) }))
          .sort((a, b) => Number(b.main) - Number(a.main) || a.name.localeCompare(b.name));
        return { ...tree, worktrees };
      })
      .sort((a, b) => a.name.localeCompare(b.name));

    out.push({
      key: `repo-machine:${m.id}`,
      machine: m,
      foldKey: `repo-machine:${m.id}`,
      repos: repoTrees,
      sessions,
      needs: sessions.filter((s) => stateOf(s) === "needs_you").length,
    });
  }
  return out;
}

