// The overview (总览) rendered statically (#2320): a waiting card is something to do, and 「全部标为已读」 is there whenever
// an agent is unread, though its tab is filed under 卡住了，要你去看 or 正在工作.
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { Interaction, Machine, Session } from "../lib/types";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

vi.mock("react-dom", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-dom")>()),
  createPortal: (children: React.ReactNode) => children,
}));

const fake = vi.hoisted(() => ({ loaded: true, sessions: [] as Session[], interactions: [] as Interaction[], machines: [] as Machine[] }));

vi.mock("../lib/store", async (importOriginal) => {
  const real = await importOriginal<typeof import("../lib/store")>();
  return { ...real, useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T) => select({ ...real.getState(), loaded: fake.loaded, machines: fake.machines, interactions: fake.interactions }) };
});
vi.mock("../lib/sessions", async (importOriginal) => {
  const real = await importOriginal<typeof import("../lib/sessions")>();
  return { ...real, useAgentSessions: () => fake.sessions, useCounts: () => ({ ...real.stateCounts(fake.sessions), interactions: fake.interactions.length }) };
});

// the extensions' overview cards are not this test's
vi.mock("./extensions/ExtensionSlots", () => ({ OverviewCards: () => null }));

const { Home } = await import("./Home");

const mac: Machine = { id: "mac", name: "Mac", online: true };
let n = 0;
const agent = (slot: number, pane: number, state: Session["state"], o: Partial<Session> = {}): Session => ({
  machine_id: "mac",
  session_id: `s${++n}`,
  title: `agent ${n}`,
  cwd: "/code",
  status: "idle",
  state,
  space_kind: "herdr",
  space_id: "w1",
  space_name: "app",
  slot_id: `w1:t${slot}`,
  slot_index: slot,
  pane_id: `w1:p${pane}`,
  pane_index: pane,
  ...o,
});
const render = (sessions: Session[], interactions: Interaction[] = []) => {
  Object.assign(fake, { sessions, interactions, machines: [mac] });
  return renderToStaticMarkup(<Home onNewAgent={() => {}} onToggleSidebar={() => {}} />);
};
const markAll = (html: string) => html.match(/<button [^>]*title="把 (\d+) 个完成未看的会话标为已读"[^>]*>/)?.[1];

describe("the overview", () => {
  it("says what each waiting card asks, from whatever field has it (#3780)", () => {
    const a = agent(1, 0, "needs_you");
    const html = render([a], [
      { id: "q", kind: "question", machine_id: "mac", session_id: a.session_id, summary: "先做 A 还是 B？" } as Interaction,
      { id: "p", kind: "permission", machine_id: "mac", session_id: a.session_id, tool_name: "Bash", tool_input: { command: "swift test" } } as Interaction,
      { id: "l", kind: "plan", machine_id: "mac", session_id: a.session_id, plan: "# 计划" } as Interaction,
    ]);
    expect(html).toContain("先做 A 还是 B？");
    expect(html).toContain("swift test");
    expect(html).toContain("计划写好了，等你批准");
  });

  it("does not say nothing needs you above a waiting card", () => {
    const asking = agent(1, 0, "needs_you");
    const html = render([asking, agent(2, 0, "idle")], [{ id: "i1", kind: "permission", machine_id: "mac", session_id: asking.session_id, tool_name: "Bash" } as Interaction]);
    expect(html).toContain("需要你处理");
    expect(html).not.toContain("没有需要你处理的");
    expect(html).not.toContain("一切就绪");
    expect(render([agent(2, 0, "idle")])).toContain("没有需要你处理的");
  });

  it("offers 全部标为已读 beside the summary when the unread agents' tabs are working or need you", () => {
    const html = render([agent(1, 0, "working"), agent(1, 1, "unread"), agent(2, 0, "needs_you"), agent(2, 1, "unread")]);
    expect(html).not.toContain("做完了，还没看");
    expect(html).toContain("正在工作");
    expect(html).toContain("卡住了，要你去看");
    expect(markAll(html)).toBe("2");
  });

  it("keeps it in 做完了，还没看's header, for every unread agent", () => {
    const html = render([agent(1, 0, "working"), agent(1, 1, "unread"), agent(3, 0, "unread")]);
    const section = html.indexOf("做完了，还没看");
    expect(section).toBeGreaterThan(-1);
    expect(markAll(html)).toBe("2");
    expect(html.indexOf("全部标为已读")).toBeGreaterThan(section);
    expect(markAll(render([agent(1, 0, "working")]))).toBeUndefined();
  });

  it("says the headline counts agents and why the cards can be more (#2956)", () => {
    const a = agent(2, 0, "needs_you");
    const card = (id: string) => ({ id, machine_id: "mac", session_id: a.session_id, kind: "question" }) as Interaction;
    const two = render([a], [card("q1"), card("q2")]);
    expect(two).toContain("1 个 Agent 需要你");
    expect(two).toContain("同一个 Agent 可能连着问你几件事");
    // one card per agent: the numbers agree, nothing to explain
    expect(render([a], [card("q1")])).not.toContain("同一个 Agent 可能连着问你几件事");
    // two cards on one agent beside a stuck agent without a card: still one agent with several (#2956 review)
    const stuck = agent(3, 0, "needs_you");
    expect(render([a, stuck], [card("q1"), card("q2")])).toContain("同一个 Agent 可能连着问你几件事");
    // a card per agent, its agent not counted (an assistant session): nothing to explain
    const other = { id: "q3", machine_id: "mac", session_id: "assistant-1", kind: "question" } as Interaction;
    expect(render([a], [card("q1"), other])).not.toContain("同一个 Agent 可能连着问你几件事");
  });

  it("explains the agents that need you without a card (#2956)", () => {
    const html = render([agent(2, 0, "needs_you")]);
    expect(html).toContain("卡住了，要你去看");
    expect(html).toContain("这些 Agent 没发来要你处理的卡片，多半是出错或卡住了");
  });

  it("before the first snapshot shows only 加载中, not the agents and cards the socket has streamed in so far", () => {
    const asking = agent(1, 0, "needs_you");
    const ix = { id: "i1", kind: "permission", machine_id: "mac", session_id: asking.session_id, tool_name: "Bash" } as Interaction;
    fake.loaded = false;
    try {
      const early = render([asking, agent(2, 0, "working"), agent(3, 0, "unread")], [ix]);
      expect(early).toContain("加载中");
      for (const group of ["需要你处理", "卡住了，要你去看", "正在工作", "做完了，还没看", "电脑", "全部标为已读"]) expect(early).not.toContain(group);
    } finally {
      fake.loaded = true;
    }
    const html = render([asking, agent(2, 0, "working"), agent(3, 0, "unread")], [ix]);
    expect(html).not.toContain("加载中");
    for (const group of ["需要你处理", "正在工作", "做完了，还没看", "电脑"]) expect(html).toContain(group);
  });
});
