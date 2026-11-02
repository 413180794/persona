import { describe, expect, it } from "vitest";
import { agentGrouping, buildAgentGroups, isQuiet, splitAgentSessions, type AgentMutes } from "./agent-groups";
import { byAttention } from "./format";
import type { AgentState, Machine, Session } from "./types";

const machines: Machine[] = [
  { id: "machine-b", name: "电脑 B", online: true },
  { id: "machine-a", name: "电脑 A", online: true },
  { id: "machine-off", name: "离线电脑", online: false },
];
const date = (hour: number) => `2026-01-01T${String(hour).padStart(2, "0")}:00:00Z`;
const session = (id: string, state: AgentState, patch: Partial<Session> = {}): Session => ({
  machine_id: "machine-a", session_id: id, title: id, cwd: "/tmp/project", status: state, state, updated_at: date(1), ...patch,
});
const ids = (sessions: Session[]) => sessions.map((s) => s.session_id);
const mutes: AgentMutes = { spaces: ["machine-a/项目"], sessions: ["machine-a/muted-working", "machine-a/muted-needs", "machine-a/muted-idle", "machine-a/muted-waiting"], allowed: ["machine-a/allowed"] };

describe("Agent 分组偏好", () => {
  it("默认按项目，并兼容丢失或未知的偏好", () => {
    expect(agentGrouping(null)).toBe("space");
    expect(agentGrouping("invalid")).toBe("space");
    for (const value of ["space", "state", "machine", "todo"]) expect(agentGrouping(value)).toBe(value);
  });
});

describe("iOS 标准的状态分组", () => {
  it("固定状态顺序，已看默认折叠，未知仍作为 live Agent", () => {
    const all = [session("idle", "idle"), session("working", "working"), session("unread", "unread"), session("needs", "needs_you"),
      session("waiting", "unknown", { loop: { kind: "cron" } }), session("unknown", "unknown"), session("muted-working", "working")];
    const groups = buildAgentGroups(all, machines, "state", mutes);
    expect(groups.map((g) => g.name)).toEqual(["需要你", "完成未看", "工作中", "待唤醒", "已屏蔽", "已看"]);
    expect(ids(groups[5].sessions)).toEqual(["idle", "unknown"]);
    expect(groups[5].folded).toBe(true);
    expect(groups.slice(0, 5).every((g) => !g.folded)).toBe(true);
  });

  it("屏蔽的需要你仍排第一，普通已看不进入已屏蔽", () => {
    const groups = buildAgentGroups([session("muted-needs", "needs_you"), session("muted-idle", "idle"), session("muted-working", "working"),
      session("muted-waiting", "idle", { background: [{ id: "task", kind: "shell" }] })], machines, "state", mutes);
    expect(groups.map((g) => g.name)).toEqual(["需要你", "已屏蔽", "已看"]);
    expect(ids(groups[0].sessions)).toEqual(["muted-needs"]);
    expect(ids(groups[1].sessions)).toEqual(["muted-waiting", "muted-working"]);
    expect(ids(groups[2].sessions)).toEqual(["muted-idle"]);
  });

  it("项目屏蔽按机器和名称生效，并支持 Agent 单独重新开启通知", () => {
    const all = [session("space-muted", "unread", { space_name: "项目" }), session("allowed", "unread", { space_name: "项目" }),
      session("other-machine", "unread", { machine_id: "machine-b", space_name: "项目" }), session("explicit-muted", "unread", { space_name: "项目" })];
    const groups = buildAgentGroups(all, machines, "state", { ...mutes, sessions: ["machine-a/explicit-muted"], allowed: [...mutes.allowed!, "machine-a/explicit-muted"] });
    expect(ids(groups[0].sessions)).toEqual(["allowed", "other-machine"]);
    expect(ids(groups[1].sessions)).toEqual(["explicit-muted", "space-muted"]);
  });

  it("空闲但有 loop、后台工作、运行中子 Agent 的会话归为待唤醒", () => {
    const all = [session("loop", "idle", { loop: { kind: "loop" } }), session("background", "unknown", { background: [{ id: "task", kind: "shell" }] }),
      session("subagent", "idle", { subagents: [{ id: "child", session_id: "child", running: true }] }),
      session("done", "idle", { background: [], subagents: [{ id: "child", session_id: "child", running: false }] })];
    expect(all.map(isQuiet)).toEqual([false, false, false, true]);
    const groups = buildAgentGroups(all, machines, "state");
    expect(ids(groups[0].sessions)).toEqual(["background", "loop", "subagent"]);
    expect(ids(groups[1].sessions)).toEqual(["done"]);
  });

  it("按时间降序，同时间按 ID，工作中使用启动时间且可回退更新时间", () => {
    const all = [session("a", "working", { state_since: date(2), updated_at: date(8) }), session("b", "working", { state_since: date(3), updated_at: date(4) }),
      session("c", "working", { updated_at: date(3) }), session("old", "working", { updated_at: undefined })];
    expect(ids(buildAgentGroups(all, machines, "state")[0].sessions)).toEqual(["b", "c", "a", "old"]);
    expect(ids(buildAgentGroups(all.map((s) => ({ ...s, updated_at: s.state_since ? date(20) : s.updated_at })), machines, "state")[0].sessions)).toEqual(["b", "c", "a", "old"]);
    expect(ids(all.slice().sort(byAttention))).toEqual(["b", "c", "a", "old"]);
  });

  it("只显示非空状态组", () => {
    expect(buildAgentGroups([], machines, "state")).toEqual([]);
    expect(buildAgentGroups([session("working", "working")], machines, "state").map((g) => g.name)).toEqual(["工作中"]);
  });
});

describe("iOS 标准的机器分组", () => {
  it("沿用机器数组顺序，组内按状态、时间、ID 排序", () => {
    const all = [session("idle", "idle"), session("working", "working", { state_since: date(8) }), session("unread", "unread"), session("needs", "needs_you"),
      session("b", "unread", { machine_id: "machine-b", updated_at: date(3) }), session("a", "unread", { machine_id: "machine-b", updated_at: date(3) })];
    const groups = buildAgentGroups(all, machines, "machine");
    expect(groups.map((g) => g.name)).toEqual(["电脑 B", "电脑 A"]);
    expect(ids(groups[0].sessions)).toEqual(["a", "b"]);
    expect(ids(groups[1].sessions)).toEqual(["needs", "unread", "working", "idle"]);
  });

  it("不同机器上的相同 session ID 不丢失，也不要求项目元数据", () => {
    const groups = buildAgentGroups([session("same", "working"), session("same", "working", { machine_id: "machine-b" })], machines, "machine");
    expect(groups).toHaveLength(2);
    expect(groups.every((g) => g.sessions.length === 1)).toBe(true);
  });
});

describe("所有分组共用 live 与最近的边界", () => {
  it("助手不混入 Agents，离线或未知机器的会话只进入最近", () => {
    const all = [session("live", "unknown"), session("assistant", "working", { assistant: true }), session("offline", "offline"),
      session("machine-offline", "working", { machine_id: "machine-off" }), session("unknown-machine", "working", { machine_id: "unknown" })];
    const lists = splitAgentSessions(all, machines);
    expect(ids(lists.live)).toEqual(["live"]);
    expect(ids(lists.recent)).toEqual(["offline", "machine-offline", "unknown-machine"]);
    for (const grouping of ["state", "machine"] as const) expect(ids(buildAgentGroups(all, machines, grouping).flatMap((g) => g.sessions))).toEqual(["live"]);
  });

  it("最近按更新时间倒序并限制 40 条，不修改输入数据", () => {
    const all = Array.from({ length: 50 }, (_, i) => session(String(i).padStart(2, "0"), "offline", { updated_at: date(i % 24) }));
    const before = ids(all);
    const { recent } = splitAgentSessions(all, machines);
    expect(recent).toHaveLength(40);
    expect(recent[0].updated_at).toBe(date(23));
    expect(ids(all)).toEqual(before);
  });
});
