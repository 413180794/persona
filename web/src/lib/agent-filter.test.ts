import { describe, expect, it } from "vitest";
import { applyFilter, arrangeSpaces, filterCounts, matchesFilter, matchingKeys, shownFilters, agentFilter, unreadOf, type SpaceLike } from "./agent-filter";
import type { AgentMutes } from "./agent-groups";
import { NO_PINS } from "./pins";
import type { AgentState, Session } from "./types";

const date = (min: number) => `2026-01-01T10:${String(min).padStart(2, "0")}:00Z`;
const session = (id: string, state: AgentState, patch: Partial<Session> = {}): Session => ({
  machine_id: "m", session_id: id, title: id, cwd: "/tmp/p", status: state, state, updated_at: date(1), ...patch,
});
const ids = (list: Session[]) => list.map((s) => s.session_id);
const none: AgentMutes = { spaces: [], sessions: [], allowed: [] };
const muted: AgentMutes = { spaces: ["m/安静"], sessions: ["m/muted-unread", "m/muted-needs", "m/muted-working"], allowed: [] };

describe("快速筛选（iOS AgentFilter）", () => {
  it("默认全部，未知的值也是全部", () => {
    expect(agentFilter(null)).toBe("all");
    expect(agentFilter("x")).toBe("all");
    expect(agentFilter("unread")).toBe("unread");
  });

  it("屏蔽的需要你仍算，屏蔽的未读 / 工作中不算", () => {
    const all = [session("needs", "needs_you"), session("muted-needs", "needs_you"), session("unread", "unread"), session("muted-unread", "unread"),
      session("working", "working"), session("muted-working", "working"), session("space-muted", "working", { space_name: "安静" }), session("idle", "idle")];
    expect(filterCounts(all, muted)).toEqual({ needs: 2, unread: 1, working: 1, feishu: 0, terminal: 0 });
    expect(all.filter((s) => matchesFilter(s, "all", muted))).toHaveLength(all.length);
    expect(ids(all.filter((s) => matchesFilter(s, "working", muted)))).toEqual(["working"]);
  });

  it("只露出有 Agent 的状态，选中的空了也留着", () => {
    expect(shownFilters("all", { needs: 0, unread: 2, working: 0, feishu: 0, terminal: 0 })).toEqual(["all", "unread"]);
    expect(shownFilters("needs", { needs: 0, unread: 0, working: 1, feishu: 0, terminal: 0 })).toEqual(["all", "needs", "working"]);
    expect(shownFilters("all", { needs: 0, unread: 0, working: 0, feishu: 0, terminal: 0 })).toEqual(["all"]);
  });

  it("选中时显示的留到再选一次：点开的未读变成已看，位置不变", () => {
    const before = [session("a", "unread"), session("b", "unread"), session("c", "working")];
    const kept = matchingKeys(before, "unread", none);
    expect([...kept]).toEqual(["m/a", "m/b"]);
    const after = [session("a", "idle"), session("b", "unread"), session("c", "working"), session("d", "unread")];
    expect(ids(applyFilter(after, "unread", none, kept))).toEqual(["a", "b", "d"]);
    expect(ids(applyFilter(after, "unread", none, new Set()))).toEqual(["b", "d"]);
    expect(applyFilter(after, "all", none, new Set())).toBe(after);
    expect(matchingKeys(after, "all", none).size).toBe(0);
  });

  it("全部标为已读只标完成未看的", () => {
    expect(ids(unreadOf([session("a", "unread"), session("b", "idle"), session("c", "needs_you"), session("d", "unread")]))).toEqual(["a", "d"]);
  });
});

describe("空间的顺序", () => {
  const sp = (name: string, sessions: Session[]): SpaceLike => ({ key: `m/${name}`, machineId: "m", name, sessions });
  const busy = (x: SpaceLike) => x.sessions.some((s) => s.state !== "idle");
  const a = sp("a", [session("a1", "idle", { updated_at: date(5) })]);
  const b = sp("b", [session("b1", "working", { state_since: date(2), updated_at: date(9) })]);
  const c = sp("c", [session("c1", "unread", { updated_at: date(7) })]);
  const d = sp("安静", [session("d1", "working", { updated_at: date(8) })]);

  it("置顶的空间在最前（屏蔽的不算），然后是忙的、屏蔽的忙的，安静的折起来", () => {
    const r = arrangeSpaces([a, b, d, c], { pins: { spaces: ["m/a", "m/安静"], sessions: [] }, mutes: muted, filtered: false, busy });
    expect(r.active.map((x) => x.name)).toEqual(["a", "b", "c", "安静"]);
    expect(r.idle).toEqual([]);
    const r2 = arrangeSpaces([a, b, d, c], { pins: NO_PINS, mutes: none, filtered: false, busy });
    expect(r2.active.map((x) => x.name)).toEqual(["b", "安静", "c"]);
    expect(r2.idle.map((x) => x.name)).toEqual(["a"]);
  });

  it("有筛选时全部列出、最近的在前，不折叠", () => {
    const r = arrangeSpaces([a, b, d, c], { pins: NO_PINS, mutes: none, filtered: true, busy });
    // b is working since :02 (its own lines do not move it), c finished at :07, the quiet one at :08
    expect(r.active.map((x) => x.name)).toEqual(["安静", "c", "a", "b"]);
    expect(r.idle).toEqual([]);
  });
});
