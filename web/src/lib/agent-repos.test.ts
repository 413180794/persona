import { describe, expect, it } from "vitest";
import { buildRepoMachineTree, buildRepoTree, repoOf, type RepoTree } from "./agent-repos";
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

  it("宿主给了仓库（repo_root，#2840）时按它分：在它里面是主工作树，别处是它的工作树", () => {
    const wt = "/home/me/.paseo/worktrees/1a2b3c4d/brave-fox";
    expect(repoOf(wt, "/code/app")).toEqual({ repo: "/code/app", name: "app", worktree: wt, main: false });
    expect(repoOf("/code/app", "/code/app/")).toEqual({ repo: "/code/app", name: "app", worktree: "/code/app", main: true });
    expect(repoOf("/code/app/server", "/code/app")).toEqual({ repo: "/code/app", name: "app", worktree: "/code/app", main: true });
    expect(repoOf("/code/app-2", "/code/app").main).toBe(false);
    expect(repoOf(wt, "")).toEqual(repoOf(wt)); // 空的 repo_root 和没有一样
  });
});

const ids = (sessions: Session[]) => sessions.map((s) => s.session_id);
const names = (tree: RepoTree[]) => tree.map((r) => r.name);
const worktrees = (r: RepoTree) => r.worktrees.map((w) => `${w.name}${w.main ? "*" : ""}`);

describe("按仓库分组的树", () => {
  it("带 repo_root 的会话（Paseo 的 worktree）归到主仓库下，不再各成一个仓库", () => {
    const tree = buildRepoTree([
      session("main", "/code/app", { repo_root: "/code/app" }),
      session("w1", "/home/me/.paseo/worktrees/1a2b3c4d/brave-fox", { repo_root: "/code/app" }),
      session("w2", "/home/me/.paseo/worktrees/1a2b3c4d/calm-bear", { repo_root: "/code/app" }),
    ], machines);
    expect(names(tree)).toEqual(["app"]);
    expect(tree[0]!.worktrees.map((w) => [w.name, w.main, ids(w.sessions)])).toEqual([
      ["app", true, ["main"]], ["brave-fox", false, ["w1"]], ["calm-bear", false, ["w2"]],
    ]);
  });

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

describe("按电脑分段的仓库树", () => {
  it("只有一台电脑有 Agent 时返回这台电脑的仓库", () => {
    const all = [
      session("a", "/code/app"),
      session("b", "/code/app/.worktrees/wt"),
    ];
    const tree = buildRepoMachineTree(all, machines);
    expect(tree).toHaveLength(1);
    expect(tree[0]!.machine.id).toBe("machine-a");
    expect(tree[0]!.repos).toHaveLength(1);
    expect(tree[0]!.repos[0]!.name).toBe("app");
    expect(tree[0]!.sessions).toHaveLength(2);
  });

  it("多台电脑时按 hub 机器顺序分段，电脑内仓库按名字排", () => {
    const all = [
      session("a-zeta", "/code/zeta", { machine_id: "machine-a" }),
      session("a-alpha", "/code/alpha", { machine_id: "machine-a" }),
      session("b-beta", "/code/beta", { machine_id: "machine-b" }),
    ];
    const tree = buildRepoMachineTree(all, machines);
    expect(tree.map((m) => m.machine.id)).toEqual(["machine-b", "machine-a"]);
    expect(tree[1]!.repos.map((r) => r.name)).toEqual(["alpha", "zeta"]);
    expect(tree[0]!.repos.map((r) => r.name)).toEqual(["beta"]);
  });

  it("离线机器或没有 Agent 的机器不进树", () => {
    const all = [
      session("on", "/code/app", { machine_id: "machine-a" }),
      session("off", "/code/app", { machine_id: "machine-off" }),
    ];
    const tree = buildRepoMachineTree(all, machines);
    expect(tree.map((m) => m.machine.id)).toEqual(["machine-a"]);
  });

  it("needs 准确统计该电脑上需要你的 Agent 数量", () => {
    const all = [
      session("n1", "/code/app", { machine_id: "machine-a", state: "needs_you", status: "needs_you" }),
      session("w1", "/code/app", { machine_id: "machine-a", state: "working", status: "working" }),
      session("n2", "/code/app", { machine_id: "machine-b", state: "needs_you", status: "needs_you" }),
    ];
    const tree = buildRepoMachineTree(all, machines);
    const macA = tree.find((m) => m.machine.id === "machine-a")!;
    const macB = tree.find((m) => m.machine.id === "machine-b")!;
    expect(macA.needs).toBe(1);
    expect(macB.needs).toBe(1);
  });

  it("助手会话不进树", () => {
    const all = [session("ast", "/code/app", { machine_id: "machine-a", assistant: true })];
    expect(buildRepoMachineTree(all, machines)).toEqual([]);
  });
});

