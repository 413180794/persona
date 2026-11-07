// @vitest-environment jsdom
// Continue from the overview (#6449): a finished agent's row takes its next piece of work in one line, kept in the chat's
// outbox. An empty line sends nothing; a failed send says why and its resend is the outbox's Retry of that message; the
// agent is the one named when the line opened; an input method's Enter is not a send; the keyboard lands back on the button.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { OutboxEntry } from "../lib/outbox";
import type { Machine, Session } from "../lib/types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const fake = vi.hoisted(() => {
  const entries = new Map<string, OutboxEntry>();
  let n = 0;
  const result = { ok: true, why: "" };
  const go = (sid: string, id: string, text: string) => {
    entries.set(id, { id, text, images: [], at: 0, state: result.ok ? "sent" : "failed", error: result.ok ? undefined : result.why });
    return { id, sent: Promise.resolve(result.ok), sid };
  };
  return {
    sessions: [] as Session[],
    entries,
    result,
    queue: vi.fn((_mid: string, sid: string, out: { text: string }) => go(sid, `m${++n}`, out.text)),
    retry: vi.fn((_mid: string, sid: string, id: string) => go(sid, id, entries.get(id)!.text)),
    discard: vi.fn((_mid: string, _sid: string, id: string) => void entries.delete(id)),
  };
});

vi.mock("../lib/store", async (importOriginal) => {
  const real = await importOriginal<typeof import("../lib/store")>();
  const mac: Machine = { id: "mac", name: "Mac", online: true };
  return { ...real, useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T) => select({ ...real.getState(), loaded: true, machines: [mac], interactions: [] }) };
});
vi.mock("../lib/sessions", async (importOriginal) => {
  const real = await importOriginal<typeof import("../lib/sessions")>();
  return { ...real, useAgentSessions: () => fake.sessions, useCounts: () => ({ ...real.stateCounts(fake.sessions), interactions: 0 }) };
});
vi.mock("../lib/outbox", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../lib/outbox")>()),
  queueToSession: fake.queue,
  retryOutboxEntry: fake.retry,
  discardOutbox: fake.discard,
  outboxEntry: (_mid: string, _sid: string, id: string) => fake.entries.get(id),
}));
vi.mock("./extensions/ExtensionSlots", () => ({ OverviewCards: () => null }));

const { Home } = await import("./Home");

const finished = (o: Partial<Session> = {}): Session => ({
  machine_id: "mac", session_id: "s1", title: "fix the login page", cwd: "/code", status: "idle", state: "unread",
  space_kind: "herdr", space_id: "w1", space_name: "app", slot_id: "w1:t1", slot_index: 1, pane_id: "w1:p1", pane_index: 1, ...o,
}) as Session;

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  fake.entries.clear();
  fake.result.ok = true;
  fake.result.why = "";
  for (const f of [fake.queue, fake.retry, fake.discard]) f.mockClear();
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const mount = (s: Session[]) => {
  fake.sessions = s;
  act(() => root.render(<Home onNewAgent={() => {}} onToggleSidebar={() => {}} />));
};
const button = (label: string) => [...host.querySelectorAll("button")].find((b) => b.textContent?.trim().startsWith(label));
const input = () => host.querySelector<HTMLInputElement>("[data-continue] input");
const open = () => act(() => button("继续")!.click());
const type = (text: string) => {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input()!, text);
    input()!.dispatchEvent(new Event("input", { bubbles: true }));
  });
};
const submit = async () => {
  await act(async () => host.querySelector("[data-continue] form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
};
const status = () => host.querySelector("[data-continue] [role=status]")?.textContent ?? "";

describe("Continue on a finished agent's row", () => {
  it("sends one line to that agent, says it went, and gives the keyboard back to the button", async () => {
    mount([finished()]);
    expect(button("继续")!.textContent).toContain("fix the login page"); // its screen-reader name tells the rows apart
    open();
    type("  also add a test  ");
    await submit();
    expect(fake.queue).toHaveBeenCalledWith("mac", "s1", { text: "also add a test" });
    expect(input()).toBeNull();
    expect(status()).toContain("已发出");
    expect(document.activeElement).toBe(button("继续"));
  });

  it("sends nothing for an empty line", async () => {
    mount([finished()]);
    open();
    type("   ");
    expect(button("发送")!.disabled).toBe(true);
    await submit();
    expect(fake.queue).not.toHaveBeenCalled();
  });

  it("says why it failed, resends as the outbox's Retry, and sends a changed line as new", async () => {
    fake.result.ok = false;
    fake.result.why = "computer offline";
    mount([finished()]);
    open();
    type("go on");
    await submit();
    expect(input()?.value).toBe("go on");
    expect(status()).toContain("computer offline");
    // the same words again: the Retry of that message, not a second one beside it
    await submit();
    expect(fake.queue).toHaveBeenCalledTimes(1);
    const first = fake.queue.mock.results[0]!.value.id;
    expect(fake.retry).toHaveBeenCalledWith("mac", "s1", first);
    // other words: the failed one is dropped, these go as a new message
    fake.result.ok = true;
    type("go on, carefully");
    await submit();
    expect(fake.discard).toHaveBeenCalledWith("mac", "s1", first);
    expect(fake.queue).toHaveBeenLastCalledWith("mac", "s1", { text: "go on, carefully" });
    expect(status()).toContain("已发出");
  });

  it("keeps to the agent named when it opened, in a tab of several", async () => {
    const a = finished({ session_id: "a", agent: "codex", updated_at: "2026-11-07T10:00:00Z" });
    const b = finished({ session_id: "b", pane_id: "w1:p2", pane_index: 2, agent: "claude", updated_at: "2026-11-07T09:00:00Z" });
    mount([a, b]);
    open();
    expect(input()!.placeholder).toContain("Codex");
    type("next step");
    mount([a, { ...b, updated_at: "2026-11-07T11:00:00Z" }]); // b finished again: it leads the row now
    await submit();
    expect(fake.queue).toHaveBeenCalledWith("mac", "a", { text: "next step" });
  });

  it("leaves an input method's Enter to the input method", () => {
    mount([finished()]);
    open();
    type("ni");
    const e = new KeyboardEvent("keydown", { key: "Enter", keyCode: 229, bubbles: true, cancelable: true });
    act(() => input()!.dispatchEvent(e));
    expect(e.defaultPrevented).toBe(true); // the form would otherwise take it as a submit
  });

  it("is not offered on an agent that cannot take messages", () => {
    mount([finished({ source_kind: "codex_history", capabilities: {} } as Partial<Session>)]);
    expect(host.textContent).toContain("fix the login page");
    expect(button("继续")).toBeUndefined();
  });
});
