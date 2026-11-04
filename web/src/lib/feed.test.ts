import { afterEach, describe, expect, it, vi } from "vitest";
import { dayLabel, displayLines, getFeedState, mergeInto, quickResult, removeTask, setRoute, startFailure, type FeedItem, type TaskPlan } from "./feed";
import { getPanel, openPanel } from "./panel";
import type { Todo } from "./todos";
import type { ChatLine, Segment } from "./turns";
import type { Item } from "./types";

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

function seg(id: string, userMin: number, steps: Segment["steps"] = []): ChatLine {
  const s = { id, user: { id: id + "u", text: "m", images: [], ts: ago(userMin), contexts: [] }, steps, start: ago(userMin), tools: 0, thoughts: 0, replies: 0, folds: [] } as Segment;
  return { kind: "segment", id, seg: s, live: false };
}

const feed = (id: string, min: number, kind: FeedItem["kind"] = "task"): FeedItem => ({ id, conversation: "c1", at: ago(min), kind, ref: "t" });

describe("the assistant's feed among the chat's lines (docs/unified-entry.md)", () => {
  it("keeps one copy of each item, oldest first", () => {
    const a = feed("a", 5), b = feed("b", 10);
    expect(mergeInto([a], [b, a]).map((i) => i.id)).toEqual(["b", "a"]);
  });

  it("puts an item at the end of the exchange it happened in, the rest last, with a day line per day", () => {
    const lines = [seg("s1", 60 * 26), seg("s2", 30)];
    const out = displayLines(lines, [feed("early", 60 * 26 - 1), feed("mid", 20)]);
    const ids = out.map((l) => l.id);
    expect(ids.filter((i) => i.startsWith("day-"))).toHaveLength(2);
    expect(ids.filter((i) => !i.startsWith("day-"))).toEqual(["s1", "feed-early", "s2", "feed-mid"]);
  });

  it("draws a reminder or an event the phone added after its exchange, not a read or a failure", () => {
    const use = (name: string, id: string) => ({ id, seq: 1, role: "assistant", kind: "tool_use", tool_name: name }) as Item;
    const res = (text: string, err = false) => ({ id: "r", seq: 2, role: "user", kind: "tool_result", text, is_error: err }) as Item;
    const ok = quickResult(use("mcp__jarvis__phone_reminders_create", "u1"), res(JSON.stringify({ id: "R1", title: "打电话", due: "2026-11-07T10:00:00+08:00", list: "提醒事项" })));
    expect(ok).toMatchObject({ kind: "reminder", title: "打电话", place: "提醒事项" });
    expect(quickResult(use("mcp__jarvis__phone_reminders_list", "u2"), res(JSON.stringify({ id: "x" })))).toBeUndefined();
    expect(quickResult(use("phone_calendar_create_event", "u3"), res("denied", true))).toBeUndefined();
    const step = { kind: "tool" as const, id: "u1", use: use("phone_reminders_create", "u1"), result: res(JSON.stringify({ id: "R1", title: "t" })), images: [] };
    const out = displayLines([seg("s1", 5, [step])], []).map((l) => l.id);
    expect(out.filter((i) => !i.startsWith("day-"))).toEqual(["s1", "quick-u1"]);
  });

  it("names today and yesterday", () => {
    const now = new Date();
    expect(dayLabel(now, now)).toMatch(/^今天 · /);
    const y = new Date(now);
    y.setDate(now.getDate() - 1);
    expect(dayLabel(y, now)).toMatch(/^昨天 · /);
  });
});

describe("a task whose agent did not start (#1077, #1142)", () => {
  const todo = (patch: Partial<Todo> = {}): Todo => ({
    id: "t1", title: "设置页按钮对齐", created_at: "2026-11-04T10:00:00Z", updated_at: "2026-11-04T10:00:00Z", origin: "c1", plan_id: "p1", agents: [], ...patch,
  });
  const plans: Record<string, TaskPlan> = {
    p1: { id: "p1", conversation: "c1", state: "started", tasks: [{ title: "设置页按钮对齐", prompt: "", machine: "box", cwd: "/code/app", todo_id: "t1", error: "rpc: connection closed" }] },
  };

  it("says why from the todo, else from its plan while it has no agent", () => {
    expect(startFailure(todo({ start_error: "launcher not found" }), {})).toBe("launcher not found");
    expect(startFailure(todo(), plans)).toBe("rpc: connection closed"); // a hub before #1077 left the todo open
    expect(startFailure(todo({ agents: [{ machine_id: "box", session_id: "s1", linked_at: "" }] }), plans)).toBeUndefined();
    expect(startFailure(todo(), {})).toBeUndefined(); // may still be starting
  });

  describe("removing it", () => {
    const ok = (body: unknown) => ({ ok: true, status: 200, text: async () => JSON.stringify(body) });
    afterEach(() => vi.unstubAllGlobals());
    const stub = () => {
      const fetcher = vi.fn().mockResolvedValue(ok({ ok: true, todos: [] }));
      vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
      vi.stubGlobal("fetch", fetcher);
      return fetcher;
    };

    it("deletes a task that never had an agent at once, and drops its route and its panel", async () => {
      const fetcher = stub();
      setRoute("c1", "t1");
      openPanel({ kind: "task", todo: "t1" });
      const ask = vi.fn((_message: string) => true);
      expect(await removeTask(todo({ start_error: "x", done_at: "2026-11-04T10:00:05Z" }), ask)).toBe(true);
      expect(ask).not.toHaveBeenCalled();
      expect(fetcher.mock.calls.some(([url, init]) => String(url).endsWith("/todos/t1") && init?.method === "DELETE")).toBe(true);
      expect(getFeedState().routes.c1).toBeUndefined();
      expect(getPanel()).toBeNull();
    });

    it("asks first when its agents ran, and keeps it when the user says no", async () => {
      const fetcher = stub();
      const ran = todo({ agents: [{ machine_id: "box", session_id: "s1", linked_at: "" }] });
      expect(await removeTask(ran, () => false)).toBe(false);
      expect(fetcher).not.toHaveBeenCalled();
      const ask = vi.fn((_message: string) => true);
      expect(await removeTask(ran, ask)).toBe(true);
      expect(ask.mock.calls[0][0]).toContain("Agents 页");
      expect(fetcher.mock.calls.some(([, init]) => init?.method === "DELETE")).toBe(true);
    });
  });
});
