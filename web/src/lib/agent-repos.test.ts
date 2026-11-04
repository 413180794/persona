import { describe, expect, it } from "vitest";
import { buildRepoTree, repoOf, type RepoTree } from "./agent-repos";
import type { AgentState, Machine, Session } from "./types";

const machines: Machine[] = [
  { id: "machine-b", name: "电脑 B", online: true },
  { id: "machine-a", name: "电脑 A", online: true },
  { id: "machine-off", name: "离线电脑", online: false },
];
const date = (hour: number) => `2026-01-01T${String(hour).padStart(2, "0")}:00:00Z`;
const session = (id: string, cwd: string, patch: Partial<Session> = {}): Session => ({
  machine_id: "machine-a", session_id: id, title: id, cwd, status: "idle" as AgentState, state: "idle" as AgentState, updated_at: date(1), ...patch,
});

describe("从 cwd 推断仓库与工作树", () => {
  it("主工作树就是仓库目录本身", () => {
    expect(repoOf("/code/app")).toEqual({ repo: "/code/app", name: "app", worktree: "/code/app", main: true });
  });

  it("常见的工作树布局都归到仓库根", () => {
    for (const cwd of ["/code/app/.worktrees/fix-login", "/code/app/.claude/worktrees/fix-login"]) {
      const got = repoOf(cwd);
      expect(got.repo).toBe("/code/app");
      expect(got.name).toBe("app");
      expect(got.worktree).toBe(cwd);
      expect(got.main).toBe(false);
    }
  });

  it("~/.worktrees/<仓库>/<工作树> 这类集中布局也认得仓库", () => {
    const got = repoOf("/home/me/.worktrees/persona/fix-login");
    expect(got.repo).toBe("/home/me/.worktrees/persona");
    expect(got.name).toBe("persona");
    expect(got.worktree).toBe("/home/me/.worktrees/persona/fix-login");
    expect(got.main).toBe(false);
  });

  it("没有工作树标记时，目录本身就是仓库", () => {
    expect(repoOf("/code/app/server/internal")).toEqual({ repo: "/code/app/server/internal", name: "internal", worktree: "/code/app/server/internal", main: true });
    expect(repoOf("")).toEqual({ repo: "", name: "", worktree: "", main: true });
  });
});

const ids = (sessions: Session[]) => sessions.map((s) => s.session_id);
const names = (tree: RepoTree[]) => tree.map((r) => r.name);
const worktrees = (r: RepoTree) => r.worktrees.map((w) => `${w.name}${w.main ? "*" : ""}`);

describe("按仓库分组的树", () => {
  it("先给主工作树，再按名字给工作树", () => {
    const all = [
      session("main", "/code/app"),
      session("wt-b", "/code/app/.worktrees/fix-b"),
      session("wt-a", "/code/app/.worktrees/fix-a"),
      session("wt-a2", "/code/app/.worktrees/fix-a"),
    ];
    const tree = buildRepoTree(all, machines);
    expect(names(tree)).toEqual(["app"]);
    expect(worktrees(tree[0]!)).toEqual(["app*", "fix-a", "fix-b"]);
    expect(ids(tree[0]!.worktrees[1]!.sessions)).toEqual(["wt-a", "wt-a2"]);
    expect(tree[0]!.count).toBe(4);
  });

  it("一个 Agent 都没有的仓库不出现；仓库按名字排序", () => {
    const tree = buildRepoTree([session("z", "/code/zeta"), session("a", "/code/alpha")], machines);
    expect(names(tree)).toEqual(["alpha", "zeta"]);
  });

  it("离线机器的 Agent 不进树", () => {
    const tree = buildRepoTree([session("on", "/code/app"), session("off", "/code/app", { machine_id: "machine-off" })], machines);
    expect(ids(tree[0]!.worktrees[0]!.sessions)).toEqual(["on"]);
  });

  it("不同机器上的同名仓库各算一个，同名时按机器的顺序", () => {
    const tree = buildRepoTree([session("a", "/code/app"), session("b", "/code/app", { machine_id: "machine-b" })], machines);
    expect(tree.map((r) => `${r.machineId}/${r.name}`)).toEqual(["machine-b/app", "machine-a/app"]);
  });

  it("工作树里的 Agent 排序沿用 byAttention（需要你优先）", () => {
    const all = [
      session("idle", "/code/app/.worktrees/wt"),
      session("needs", "/code/app/.worktrees/wt", { state: "needs_you", status: "needs_you" }),
    ];
    expect(ids(buildRepoTree(all, machines)[0]!.worktrees[0]!.sessions)).toEqual(["needs", "idle"]);
  });

  it("助手会话不进树", () => {
    expect(buildRepoTree([session("assistant", "/code/app", { assistant: true })], machines)).toEqual([]);
  });
});
