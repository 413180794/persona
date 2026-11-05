// The 个人 Agent list is pulled again on every {"type":"agents"} event: one that arrives while a pull is on the way
// may announce a change that pull was answered before, so it must still lead to another pull.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { agentsChanged, refreshAgentSessions, resetAgents, useAgentList, type AgentSession } from "./agents";

vi.mock("react", async (orig) => ({
  ...(await orig<typeof import("react")>()),
  useSyncExternalStore: (_sub: unknown, get: () => unknown) => get(),
}));

const session = (id: string, unread: number): AgentSession => ({ id, source: "removedbridge", sessionKey: id, provider: "p", title: id, canSend: true, unreadCount: unread, rankTimeMs: 1 });

type Answer = (sessions: AgentSession[]) => void;
let answers: Answer[] = [];

beforeEach(() => {
  answers = [];
  vi.stubGlobal("localStorage", { getItem: () => "tok", setItem: () => {}, removeItem: () => {} });
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (String(url).endsWith("/agents")) return Promise.resolve(new Response(JSON.stringify({ sources: [] })));
      return new Promise<Response>((done) => answers.push((sessions) => done(new Response(JSON.stringify({ sessions })))));
    }),
  );
});

afterEach(() => {
  resetAgents();
  vi.unstubAllGlobals();
});

const flush = () => new Promise((r) => setTimeout(r, 0));

describe("refreshAgentSessions", () => {
  it("pulls again for an agents event that came while a pull was on the way", async () => {
    const first = refreshAgentSessions();
    await flush();
    agentsChanged([]); // a new message: the pull already out may have been answered before it
    answers[0]([session("a", 0)]);
    await first;
    await flush();
    expect(answers.length).toBe(2);
    answers[1]?.([session("a", 1)]);
    await flush();
    await flush();
    expect(useAgentList().sessions.map((s) => s.unreadCount)).toEqual([1]);
  });

  it("pulls once more however many events came meanwhile", async () => {
    const first = refreshAgentSessions();
    await flush();
    for (let i = 0; i < 5; i++) agentsChanged([]);
    answers[0]([session("a", 0)]);
    await first;
    await flush();
    expect(answers.length).toBe(2);
    answers[1]([session("a", 3)]);
    await flush();
    await flush();
    expect(answers.length).toBe(2);
    expect(useAgentList().sessions.map((s) => s.unreadCount)).toEqual([3]);
  });
});
