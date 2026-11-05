import { describe, expect, it } from "vitest";
import { badgeText, detectTransitions, MIN_GAP_MS, pickEvent, rateLimiter, suppressed, tabTitle } from "./attention";
import { stateCounts } from "./sessions";
import { agentKey } from "./pins";
import type { AgentMutes } from "./agent-groups";
import type { AgentState, Session } from "./types";

const session = (id: string, state: AgentState, patch: Partial<Session> = {}): Session => ({
  machine_id: "m", session_id: id, title: id, cwd: "/tmp/p", status: state, state, updated_at: "2026-01-01T10:00:00Z", ...patch,
});
const noMutes: AgentMutes = { spaces: [], sessions: [], allowed: [] };
const prevOf = (...pairs: [string, AgentState][]) => new Map(pairs.map(([id, st]) => [`m/${id}`, st] as const));

describe("标签页标题", () => {
  it("需要你的数量放在前面，页面名保留在后面", () => {
    expect(tabTitle("设置", 3)).toBe("(3) 设置 · Persona");
    expect(tabTitle("Persona", 2)).toBe("(2) Persona");
  });
  it("0 个时不加前缀，和原来的标题一样", () => {
    expect(tabTitle("设置", 0)).toBe("设置 · Persona");
    expect(tabTitle("Persona", 0)).toBe("Persona");
    expect(tabTitle("", 0)).toBe("Persona");
  });
});

describe("需要你的数量", () => {
  it("用的是侧边栏角标的同一个数：安静任务的不计", () => {
    const all = [session("a", "needs_you"), session("b", "needs_you"), session("q", "needs_you", { quiet: true }), session("w", "working"), session("u", "unread")];
    expect(stateCounts(all).badge).toBe(2);
  });
});

describe("图标角标文字", () => {
  it("0 不画，1-9 写数字，更多写 9+", () => {
    expect(badgeText(0)).toBe("");
    expect(badgeText(1)).toBe("1");
    expect(badgeText(9)).toBe("9");
    expect(badgeText(10)).toBe("9+");
    expect(badgeText(120)).toBe("9+");
  });
});

describe("状态变化检测", () => {
  it("变成需要你时响，不管之前是什么状态", () => {
    for (const was of ["working", "idle", "unread"] as const) {
      const { events } = detectTransitions(prevOf(["a", was]), [session("a", "needs_you")], noMutes);
      expect(events).toEqual([{ kind: "needs", key: "m/a" }]);
    }
  });
  it("一直需要你不重复响", () => {
    expect(detectTransitions(prevOf(["a", "needs_you"]), [session("a", "needs_you")], noMutes).events).toEqual([]);
  });
  it("工作中 → 完成未看算完成", () => {
    expect(detectTransitions(prevOf(["a", "working"]), [session("a", "unread")], noMutes).events).toEqual([{ kind: "finished", key: "m/a" }]);
  });
  it("工作中 → 已看 不算（hub 也不推：看过了、或 hub 分诊为做完了）", () => {
    expect(detectTransitions(prevOf(["a", "working"]), [session("a", "idle")], noMutes).events).toEqual([]);
  });
  it("工作中 → 未运行 / 未知 不算；别的状态到完成未看也不算", () => {
    expect(detectTransitions(prevOf(["a", "working"]), [session("a", "offline")], noMutes).events).toEqual([]);
    expect(detectTransitions(prevOf(["a", "working"]), [session("a", "unknown")], noMutes).events).toEqual([]);
    expect(detectTransitions(prevOf(["a", "idle"]), [session("a", "unread")], noMutes).events).toEqual([]);
  });
  it("第一次看到的会话（刚打开页面、新会话）不响", () => {
    expect(detectTransitions(new Map(), [session("a", "needs_you"), session("b", "unread")], noMutes).events).toEqual([]);
  });
  it("被屏蔽通知的会话完成不响，但需要你照响（和筛选一致）", () => {
    const muted: AgentMutes = { spaces: [], sessions: ["m/a"], allowed: [] };
    expect(detectTransitions(prevOf(["a", "working"]), [session("a", "unread")], muted).events).toEqual([]);
    expect(detectTransitions(prevOf(["a", "working"]), [session("a", "needs_you")], muted).events).toEqual([{ kind: "needs", key: "m/a" }]);
  });
  it("安静任务的会话都不响（侧边栏也不计）", () => {
    expect(detectTransitions(prevOf(["a", "working"]), [session("a", "unread", { quiet: true })], noMutes).events).toEqual([]);
    expect(detectTransitions(prevOf(["a", "idle"]), [session("a", "needs_you", { quiet: true })], noMutes).events).toEqual([]);
  });
  it("返回新的状态表，下一次拿它比较", () => {
    const first = detectTransitions(prevOf(["a", "working"]), [session("a", "unread"), session("b", "working")], noMutes);
    expect(first.next.get("m/b")).toBe("working");
    expect(detectTransitions(first.next, [session("a", "unread"), session("b", "working")], noMutes).events).toEqual([]);
  });
  it("一批里需要你优先于完成", () => {
    const { events } = detectTransitions(prevOf(["a", "working"], ["b", "idle"]), [session("a", "unread"), session("b", "needs_you")], noMutes);
    expect(events).toHaveLength(2);
    expect(pickEvent(events)).toEqual({ kind: "needs", key: "m/b" });
    expect(pickEvent([])).toBeUndefined();
  });
});

describe("正在看的会话不响", () => {
  const a = agentKey({ machine_id: "m", session_id: "a" });
  it("窗口在前台且就是当前页的会话：不响", () => {
    expect(suppressed(a, { key: a, focused: true })).toBe(true);
  });
  it("窗口不在前台（切到别的标签页或应用）：响", () => {
    expect(suppressed(a, { key: a, focused: false })).toBe(false);
  });
  it("别的会话、或不在会话页：响", () => {
    expect(suppressed(a, { key: "m/b", focused: true })).toBe(false);
    expect(suppressed(a, { focused: true })).toBe(false);
  });
});

describe("限频", () => {
  it("3 秒内最多一声，过了又可以", () => {
    const allow = rateLimiter();
    expect(allow(1000)).toBe(true);
    expect(allow(1000 + MIN_GAP_MS - 1)).toBe(false);
    expect(allow(1000 + MIN_GAP_MS)).toBe(true);
  });
  it("被拦下的不重新计时", () => {
    const allow = rateLimiter(3000);
    allow(0);
    expect(allow(2000)).toBe(false);
    expect(allow(3000)).toBe(true);
  });
});
