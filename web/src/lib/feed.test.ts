import { afterEach, describe, expect, it, vi } from "vitest";
import {
  answeredItems,
  cardSent,
  dayLabel,
  displayLines,
  draftPlaceholder,
  getFeedState,
  mergeInto,
  quickResult,
  removeTask,
  retryKey,
  retryTask,
  sendMessage,
  setRoute,
  startFailure,
  type FeedItem,
  type TaskPlan,
} from "./feed";
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

    it("retries it in place: one POST however many taps, the todo the hub answers put in at once (#1396)", async () => {
      let answer: (v: unknown) => void = () => {};
      const fetcher = vi.fn((_url: string, init?: RequestInit) =>
        init?.method === "POST" ? new Promise((r) => (answer = r)) : Promise.resolve(ok({ todos: [] })),
      );
      vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
      vi.stubGlobal("fetch", fetcher);
      const failed = todo({ start_error: "claude did not become ready within 1m0s", done_at: "2026-11-04T10:00:05Z" });
      const first = retryTask(failed);
      const second = retryTask(failed);
      expect(getFeedState().busy[retryKey("t1")]).toBe(true);
      answer(ok({ todo: { ...failed, start_error: "", done_at: null } }));
      await Promise.all([first, second]);
      const posts = fetcher.mock.calls.filter(([, init]) => init?.method === "POST");
      expect(posts).toHaveLength(1);
      expect(String(posts[0][0])).toMatch(/\/todos\/t1\/retry$/);
      expect(getFeedState().busy[retryKey("t1")]).toBe(false);
    });

    it("says why when the hub refuses the retry, and can be tapped again", async () => {
      vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 409, text: async () => JSON.stringify({ error: "「公司 Mac」现在离线，等它连上再重试" }) }));
      await expect(retryTask(todo({ start_error: "x", done_at: "2026-11-04T10:00:05Z" }))).rejects.toThrow("离线");
      expect(getFeedState().busy[retryKey("t1")]).toBe(false);
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

describe("an answered record (#1231)", () => {
  const answered = (body: Record<string, unknown>): FeedItem => ({ id: "a", conversation: "c1", at: ago(1), kind: "answered", ref: "t", body });

  it("has every question with its answer in full, newlines kept", () => {
    const it = answered({
      question: "先补哪一块？",
      answer: "重试；假服务器",
      items: [{ question: "先补哪一块？", answer: "重试\n和超时" }, { question: "请求怎么处理？", answer: "假服务器" }, { question: "", answer: "" }, "x"],
    });
    expect(answeredItems(it)).toEqual([
      { question: "先补哪一块？", answer: "重试\n和超时" },
      { question: "请求怎么处理？", answer: "假服务器" },
    ]);
  });

  it("shows a record from before it by its line, and nothing for an empty one", () => {
    expect(answeredItems(answered({ question: "使用 Bash", answer: "批准了" }))).toEqual([{ question: "使用 Bash", answer: "批准了" }]);
    expect(answeredItems(answered({ items: [] }))).toEqual([]);
  });
});

describe("a message card's 发送 (#1519)", () => {
  const ok = (body: unknown) => ({ ok: true, status: 200, text: async () => JSON.stringify(body) });
  afterEach(() => vi.unstubAllGlobals());
  const card = (id: string, body: Record<string, unknown> = {}): FeedItem => ({
    id, conversation: "c1", at: ago(5), kind: "message", ref: "example:1", body: { draft: "在修了", ...body },
  });

  it("reads sent from the card the hub keeps, with what went out (the draft for a card from before)", () => {
    expect(cardSent(card("m"))).toBeUndefined();
    expect(cardSent(card("m", { sent_at: "2026-11-04T10:23:00Z", sent: "修好了" }))).toEqual({ at: "2026-11-04T10:23:00Z", text: "修好了" });
    expect(cardSent(card("m", { sent_at: "2026-11-04T10:23:00Z" }))?.text).toBe("在修了");
  });

  it("takes a card's new body when the hub sends it again (sent on another device)", () => {
    const before = card("m");
    const list = [before];
    expect(mergeInto(list, [card("m")])).toBe(list); // the same: nothing redraws
    const sent = card("m", { sent_at: "2026-11-04T10:23:00Z" });
    expect(mergeInto(list, [sent])).toEqual([sent]);
  });

  it("finds a blank left to fill in", () => {
    expect(draftPlaceholder("确实是 bug，【预计今晚 / 几点前】修好")).toBe("【预计今晚 / 几点前】");
    expect(draftPlaceholder("MR 在 {{链接}}")).toBe("{{链接}}");
    expect(draftPlaceholder("确实是 bug，修好了告诉你")).toBeUndefined();
  });

  it("sends once however many taps, with the card, and draws it sent", async () => {
    let answer: (v: unknown) => void = () => {};
    const fetcher = vi.fn((_url: string, init?: RequestInit) => (init?.method === "POST" ? new Promise((r) => (answer = r)) : Promise.resolve(ok({}))));
    vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
    vi.stubGlobal("fetch", fetcher);
    const c = card("m1");
    const first = sendMessage(c, "修好了");
    const second = sendMessage(c, "修好了");
    expect(getFeedState().busy.m1).toBe(true);
    answer(ok({ id: "example:1", done_by: "replied" }));
    await Promise.all([first, second]);
    const posts = fetcher.mock.calls.filter(([, init]) => init?.method === "POST");
    expect(posts).toHaveLength(1);
    expect(String(posts[0][0])).toMatch(/\/inbox\/example:1\/reply$/);
    expect(JSON.parse(String(posts[0][1]?.body))).toEqual({ text: "修好了", card: "m1" });
    const now = getFeedState().feeds.c1?.find((i) => i.id === "m1");
    expect(now && cardSent(now)?.text).toBe("修好了");
    // Sent: a tap from a stale copy does nothing.
    await sendMessage(c, "修好了");
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(1);
  });

  it("does not send a draft with a blank, and can be tapped again after a failure", async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: false, status: 502, text: async () => JSON.stringify({ error: "github: rate limited" }) });
    vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
    vi.stubGlobal("fetch", fetcher);
    await expect(sendMessage(card("m2"), "【预计今晚 / 几点前】修好")).rejects.toThrow("【预计今晚 / 几点前】");
    expect(fetcher).not.toHaveBeenCalled();
    await expect(sendMessage(card("m2"), "今晚修好")).rejects.toThrow("rate limited");
    expect(getFeedState().busy.m2).toBe(false);
    expect(getFeedState().feeds.c1?.find((i) => i.id === "m2")).toBeUndefined();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(ok({})));
    await sendMessage(card("m2"), "今晚修好");
    expect(cardSent(getFeedState().feeds.c1!.find((i) => i.id === "m2")!)?.text).toBe("今晚修好");
  });
});
