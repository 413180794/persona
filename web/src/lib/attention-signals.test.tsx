// @vitest-environment jsdom
// The system notifications of useAttentionSignals (#2971): 需要你 at once, 做完 after the settle time and only for a
// long turn (设置 › 通知 只推长任务), and only while the page is not in front.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentState, Interaction, Session } from "./types";

let sessions: Session[] = [];
let mutes: { spaces: string[]; sessions: string[]; allowed: string[] } = { spaces: [], sessions: [], allowed: [] };
let interactions: Interaction[] = [];
vi.mock("./store", () => ({
  useStore: (select: (s: { loaded: boolean; interactions: Interaction[] }) => unknown) => select({ loaded: true, interactions }),
  getState: () => ({ mutes, interactions }),
}));
vi.mock("./sessions", () => ({ useCounts: () => ({ badge: 0 }), useAgentSessions: () => sessions }));
vi.mock("./api", () => ({
  api: async () => ({ input: true, done: "long", long_turn_seconds: 60 }),
  enc: encodeURIComponent,
}));

const { useAttentionSignals } = await import("./attention-signals");
const { SETTLE_MS } = await import("./attention");
const { _setTabId, markFront } = await import("./notify");

const T0 = Date.parse("2026-01-01T10:00:00Z");
const session = (id: string, state: AgentState, since?: string): Session => ({
  machine_id: "m", session_id: id, title: id, cwd: "/p", status: state, state, state_since: since, last_text: `${id} 的最后一句`,
});

const posted: { title: string; body?: string }[] = [];
const closed: string[] = [];
class FakeNotification {
  static permission = "granted";
  static requestPermission = async () => "granted";
  onclick: (() => void) | null = null;
  title: string;
  constructor(title: string, opts?: { body?: string }) {
    this.title = title;
    posted.push({ title, body: opts?.body });
  }
  close() {
    closed.push(this.title);
  }
}

function Probe() {
  useAttentionSignals("Persona", undefined);
  return null;
}

let root: Root;
let focused = false;
beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
  vi.setSystemTime(T0);
  vi.stubGlobal("Notification", FakeNotification);
  vi.spyOn(document, "hasFocus").mockImplementation(() => focused);
  localStorage.setItem("persona.systemNotify", "1");
  posted.length = 0;
  closed.length = 0;
  interactions = [];
  focused = false;
  mutes = { spaces: [], sessions: [], allowed: [] };
  _setTabId("tab-me");
  root = createRoot(document.createElement("div"));
});
afterEach(() => {
  act(() => root.unmount());
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
});

async function show(next: Session[], cards: Interaction[] = interactions) {
  sessions = next;
  interactions = cards;
  await act(async () => root.render(<Probe key="p" />));
}

describe("系统通知", () => {
  it("需要你马上发；做完的长任务等一会儿再发，短的不发", async () => {
    await show([session("long", "working", "2026-01-01T09:58:00Z"), session("short", "working", "2026-01-01T09:59:50Z"), session("ask", "working")]);
    await act(async () => {}); // the prefs load
    await show([session("long", "unread"), session("short", "unread"), session("ask", "needs_you")]);
    expect(posted).toEqual([{ title: "ask 需要你", body: "ask 的最后一句" }]);
    await act(async () => void vi.advanceTimersByTime(SETTLE_MS + 10));
    expect(posted.map((p) => p.title)).toEqual(["ask 需要你", "long 做完了"]);
  });

  it("页面在前台时不发", async () => {
    focused = true;
    await show([session("a", "working")]);
    await show([session("a", "needs_you")]);
    expect(posted).toEqual([]);
  });

  it("开关没开时不发", async () => {
    localStorage.removeItem("persona.systemNotify");
    await show([session("a", "working")]);
    await show([session("a", "needs_you")]);
    expect(posted).toEqual([]);
  });

  it("屏蔽了的 Agent 需要你时不发（和手机推送、Mac 一样）", async () => {
    mutes = { spaces: [], sessions: ["m/quiet"], allowed: [] };
    await show([session("quiet", "working"), session("loud", "working")]);
    await show([session("quiet", "needs_you"), session("loud", "needs_you")]);
    expect(posted.map((p) => p.title)).toEqual(["loud 需要你"]);
  });

  it("另一个标签页在前台时不发", async () => {
    _setTabId("tab-other");
    markFront(true);
    _setTabId("tab-me");
    await show([session("a", "working")]);
    await show([session("a", "needs_you")]);
    expect(posted).toEqual([]);
  });

  it("同一件事别的标签页刚发过就不再发", async () => {
    localStorage.setItem("persona.notified", JSON.stringify({ "m/a#needs": Date.now() }));
    await show([session("a", "working"), session("b", "working")]);
    await show([session("a", "needs_you"), session("b", "needs_you")]);
    expect(posted.map((p) => p.title)).toEqual(["b 需要你"]);
  });
});

describe("已在需要你时又来一张卡（#4058）", () => {
  const ask: Interaction = { id: "q1", machine_id: "m", session_id: "a", kind: "question", created_at: "2026-01-01T10:00:00Z", questions: [{ question: "用哪个框架？" } as never] };
  const perm: Interaction = { id: "p1", machine_id: "m", session_id: "a", kind: "permission", created_at: "2026-01-01T10:00:05Z", tool_name: "Bash", tool_input: { command: "go test ./..." } };

  it("第二张卡也弹，写的是这张卡；同一张卡不重复弹", async () => {
    await show([session("a", "working")]);
    await show([session("a", "needs_you")], [ask]);
    expect(posted).toEqual([{ title: "a 在问你", body: "用哪个框架？" }]);
    await show([session("a", "needs_you")], [ask, perm]);
    expect(posted.at(-1)).toEqual({ title: "a 要你批准", body: "go test ./..." });
    await show([session("a", "needs_you")], [perm, ask]); // a snapshot again, the same cards
    await show([{ ...session("a", "needs_you"), last_text: "新的一句" }]);
    expect(posted).toHaveLength(2);
  });

  it("通知说的那张卡答掉了就收回，会话还在等另一张也收回", async () => {
    await show([session("a", "needs_you")], [ask]);
    await show([session("a", "needs_you")], [ask, perm]);
    expect(posted.map((p) => p.title)).toEqual(["a 要你批准"]);
    await show([session("a", "needs_you")], [ask]);
    expect(closed).toEqual(["a 要你批准"]);
    await show([session("a", "needs_you")], [ask]);
    expect(closed).toHaveLength(1);
  });

  it("需要你先到、卡后到（Codex 授权）：通知改写成这张卡，不算第二次", async () => {
    await show([session("a", "working")]);
    await show([session("a", "needs_you")], []);
    expect(posted.map((p) => p.title)).toEqual(["a 需要你"]);
    vi.setSystemTime(T0 + 10_000);
    await show([session("a", "needs_you")], [perm]);
    expect(posted.at(-1)).toEqual({ title: "a 要你批准", body: "go test ./..." });
    await show([session("a", "needs_you")], [perm, { ...ask, id: "q2" }]);
    expect(posted.at(-1)?.title).toBe("a 在问你");
  });

  it("刚打开页面时已有的卡不弹", async () => {
    await show([session("a", "needs_you")], [ask, perm]);
    expect(posted).toEqual([]);
  });

  it("屏蔽了的 Agent 新卡不弹", async () => {
    mutes = { spaces: [], sessions: ["m/a"], allowed: [] };
    await show([session("a", "needs_you")], [ask]);
    await show([session("a", "needs_you")], [ask, perm]);
    expect(posted).toEqual([]);
  });

  describe("授权卡的通知带「允许 / 拒绝」（#4464）", () => {
    let shown: { title: string; ix?: string }[];
    beforeEach(() => {
      shown = [];
      (FakeNotification as unknown as { maxActions: number }).maxActions = 2;
      vi.stubGlobal("matchMedia", () => ({ matches: true }));
      const reg = { showNotification: async (title: string, o: { data?: { ix?: string } }) => void shown.push({ title, ix: o.data?.ix }), getNotifications: async () => [] };
      vi.stubGlobal("navigator", { ...navigator, serviceWorker: { controller: {}, ready: Promise.resolve(reg), getRegistration: async () => reg, addEventListener() {}, removeEventListener() {} } });
    });
    afterEach(() => delete (FakeNotification as unknown as { maxActions?: number }).maxActions);

    it("能批的会话：经 service worker 弹，带卡片 id", async () => {
      await show([session("a", "working")]);
      await show([session("a", "needs_you")], [perm]);
      await act(async () => {});
      expect(shown).toEqual([{ title: "a 要你批准", ix: "p1" }]);
      expect(posted).toEqual([]);
    });

    it("要先填内容的授权卡（fields）：不带按钮", async () => {
      await show([session("a", "working")]);
      await show([session("a", "needs_you")], [{ ...perm, fields: [{ key: "pw", label: "密码", secret: true }] }]);
      await act(async () => {});
      expect(shown).toEqual([]);
      expect(posted.map((p) => p.title)).toEqual(["a 要你批准"]);
    });

    it("不能批的会话（approve=false）：不带按钮，照旧弹普通通知", async () => {
      const locked = { ...session("a", "needs_you"), source_kind: "codex_history", capabilities: { approve: false } } as Session;
      await show([{ ...locked, state: "working", status: "working" }]);
      await show([locked], [perm]);
      await act(async () => {});
      expect(shown).toEqual([]);
      expect(posted.map((p) => p.title)).toEqual(["a 要你批准"]);
    });
  });
});
