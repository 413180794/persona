// @vitest-environment jsdom
// A waiting todo says what its agent asks (#4111): the list row and the detail's agent row carry the card's question
// or command, not the agent's last words; without last words the line still shows.
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { getState } from "../lib/store";
import type { Todo } from "../lib/todos";
import type { Interaction, Session } from "../lib/types";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

vi.mock("react-dom", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-dom")>()),
  createPortal: (children: React.ReactNode) => children,
}));

const { TodosView } = await import("./Todos");

const now = new Date().toISOString();
const todo: Todo = { id: "t1", title: "整理发布清单", created_at: now, updated_at: now, agents: [{ machine_id: "m1", session_id: "s1" }] } as Todo;
const session = (o: Partial<Session> = {}) => ({ machine_id: "m1", session_id: "s1", title: "写清单", cwd: "/work/app", status: "blocked", state: "needs_you", updated_at: now, ...o }) as Session;
const permission: Interaction = { id: "ix1", machine_id: "m1", session_id: "s1", kind: "permission", tool_name: "Bash", tool_input: { command: "swift test" } };

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };
afterEach(() => seed(before));

const render = (s: Session, ixs: Interaction[]) => {
  seed({ todos: [todo], todosLoaded: true, sessions: { m1: [s] }, interactions: ixs });
  return renderToStaticMarkup(<TodosView />);
};

describe("a waiting todo's row", () => {
  it("says the command its agent waits on, not its last words", () => {
    const html = render(session({ last_text: "我先跑一下测试" }), [permission]);
    expect(html).toContain("swift test");
    expect(html).not.toContain("我先跑一下测试");
  });

  it("shows the line even when the agent has no last words", () => {
    expect(render(session(), [permission])).toContain("swift test");
  });

  it("keeps the last words without a card", () => {
    expect(render(session({ last_text: "我先跑一下测试" }), [])).toContain("我先跑一下测试");
  });
});
