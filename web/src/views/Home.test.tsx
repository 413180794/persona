// The overview (总览) rendered statically (#2320): a waiting card is something to do, and 「全部标为已读」 is there whenever
// an agent is unread, though its tab is filed under 停在电脑上 or 正在工作.
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

const fake = vi.hoisted(() => ({ sessions: [] as Session[], interactions: [] as Interaction[], machines: [] as Machine[] }));

vi.mock("../lib/store", async (importOriginal) => {
  const real = await importOriginal<typeof import("../lib/store")>();
  return { ...real, useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T) => select({ ...real.getState(), loaded: true, machines: fake.machines, interactions: fake.interactions }) };
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
    expect(html).toContain("停在电脑上");
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
});
