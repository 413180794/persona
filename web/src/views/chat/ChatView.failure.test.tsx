// @vitest-environment jsdom
// A refused send in a Persona conversation is a card above the composer (docs/assistant/onboarding.md 4.6); the next send that
// goes through takes it away, so it does not linger over a later setup step.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Outgoing } from "./Composer";
import type { Session } from "../../lib/types";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  g.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  g.IntersectionObserver = class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } };
});

const captured = vi.hoisted(() => ({
  onSend: undefined as ((out: Outgoing, fromBox?: boolean) => Promise<boolean>) | undefined,
  onEdit: undefined as (() => void) | undefined,
  resend: vi.fn(),
  session: { machine_id: "hub", session_id: "c1", title: "Conversation", cwd: "/code", status: "idle" } as Session,
}));

vi.mock("./Composer", async (importOriginal) => ({
  ...await importOriginal<typeof import("./Composer")>(),
  Composer: ({ onSend, onEdit, resendRef }: { onSend: (out: Outgoing, fromBox?: boolean) => Promise<boolean>; onEdit?: () => void; resendRef?: { current: (() => void) | null } }) => {
    captured.onSend = onSend;
    captured.onEdit = onEdit;
    if (resendRef) resendRef.current = captured.resend;
    return null;
  },
}));
// one transcript for every render: a new one each time would re-run the sending bubble's echo check forever
const transcript = vi.hoisted(() => ({ items: [], loading: false, error: "", hasMore: false, streaming: "" }));
vi.mock("./useTranscript", () => ({ useTranscript: () => transcript }));
vi.mock("../../lib/store", async (importOriginal) => {
  const store = await importOriginal<typeof import("../../lib/store")>();
  return {
    ...store,
    getSession: () => captured.session,
    useStore: <T,>(select: (s: ReturnType<typeof store.getState>) => T) => select({ ...store.getState(), sessions: { hub: [captured.session] } }),
  };
});
vi.mock("../../lib/panel", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../lib/panel")>(),
  usePanel: () => null,
}));
vi.mock("../../lib/router", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../lib/router")>(),
  useRoute: () => ({ name: "session", mid: "hub", id: "c1" }),
}));
vi.mock("../assistant/Persona", () => ({ usePersona: () => ({ name: "Persona" }) }));
vi.mock("../assistant/AssistantChat", () => ({
  AssistantFace: () => null,
  AssistantLauncherChip: () => null,
  CompactionStrip: () => null,
  useAssistantMenu: () => ({ items: [], dialogs: null }),
}));
vi.mock("../assistant/Tasks", () => ({
  DayLine: () => null,
  FeedLineView: () => null,
  QuickResultCard: () => null,
  RouteChip: () => null,
  TaskDock: () => null,
  TaskQuestions: () => null,
}));

// the wait for an ultracode change to go through (#3428): the test says when
const ultra = vi.hoisted(() => ({ release: undefined as ((through: boolean) => void) | undefined }));
vi.mock("../../lib/ultracode", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../lib/ultracode")>(),
  ultracodeQueueDone: () => new Promise<boolean>((resolve) => (ultra.release = resolve)),
}));

const { ChatView } = await import("./ChatView");
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
let reply: () => Response;
let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
};

beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => Promise.resolve(String(input).endsWith("/messages") ? reply() : json({}))));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("a refused send's card", () => {
  it("goes away once a later send goes through", async () => {
    act(() => root.render(<ChatView mid="hub" sid="c1" assistant />));
    await flush();
    reply = () => json({ error: "模型没接好", code: "model_missing", action: "connect_model" }, 409);
    await act(async () => void (await captured.onSend!({ text: "你好", images: [] })));
    await flush();
    expect(host.querySelector('[data-testid="readiness-failure"]')).not.toBeNull();
    reply = () => json({});
    await act(async () => void (await captured.onSend!({ text: "再来", images: [] })));
    await flush();
    expect(host.querySelector('[data-testid="readiness-failure"]')).toBeNull();
  });
});

describe("a message from the box that did not go, in an agent's chat (#4270)", () => {
  const card = () => host.querySelector('[data-testid="readiness-failure"]');
  const resendButton = () => Array.from(card()?.querySelectorAll("button") ?? []).find((b) => b.textContent === "重新发送");

  it("stays above the box with a 重新发送 that sends the box again", async () => {
    act(() => root.render(<ChatView mid="hub" sid="c1" />));
    await flush();
    reply = () => json({ error: "会话已结束" }, 400);
    await act(async () => void (await captured.onSend!({ text: "你好", images: [] }, true)));
    await flush();
    expect(card()?.textContent).toContain("这条没发出去（会话已结束）");
    await act(async () => void (await captured.onSend!({ text: "继续", images: [] }))); // another send, not from the box
    await flush();
    expect(card()).not.toBeNull(); // the box still holds the unsent words
    act(() => resendButton()!.click());
    expect(captured.resend).toHaveBeenCalledOnce();
  });

  it("goes away once you edit the box", async () => {
    act(() => root.render(<ChatView mid="hub" sid="c1" />));
    await flush();
    reply = () => json({ error: "会话已结束" }, 400);
    await act(async () => void (await captured.onSend!({ text: "你好", images: [] }, true)));
    await flush();
    expect(card()).not.toBeNull();
    act(() => captured.onEdit!());
    await flush();
    expect(card()).toBeNull();
  });
});

describe("a message the node held back for an ultracode change (#3428)", () => {
  const HELD = { code: "ultracode_pending", error: "ultracode 等这一轮结束后生效，请稍后再发送消息" };
  let bodies: { client_msg_id: string; text: string }[];
  beforeEach(() => {
    bodies = [];
    ultra.release = undefined;
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (!String(input).endsWith("/messages")) return Promise.resolve(json({}));
      bodies.push(JSON.parse(String(init?.body)));
      return Promise.resolve(reply());
    }));
  });

  it("waits in its bubble saying so, leaves the box free, and goes again with a new id once the change is through", async () => {
    act(() => root.render(<ChatView mid="hub" sid="c1" />));
    await flush();
    reply = () => json(HELD, 409);
    let went: boolean | undefined;
    await act(async () => void (went = await captured.onSend!({ text: "再看看测试", images: [] }, true)));
    await flush();
    expect(went).toBe(true); // the box is free: its stop button too, while the turn runs
    expect(host.textContent).toContain("等 ultracode 的切换在这一轮结束后生效，再发这条");
    expect(host.textContent).toContain("再看看测试");
    expect(host.querySelector('[data-testid="readiness-failure"]')).toBeNull();
    reply = () => json({});
    await act(async () => ultra.release!(true));
    await flush();
    expect(bodies.map((b) => b.text)).toEqual(["再看看测试", "再看看测试"]);
    expect(bodies[1].client_msg_id).not.toBe(bodies[0].client_msg_id);
    expect(host.textContent).not.toContain("等 ultracode 的切换");
  });

  it("not through in time: the words go back into the box and the card says why", async () => {
    act(() => root.render(<ChatView mid="hub" sid="c1" />));
    await flush();
    reply = () => json(HELD, 409);
    await act(async () => void (await captured.onSend!({ text: "再看看测试", images: [] }, true)));
    await flush();
    await act(async () => ultra.release!(false));
    await flush();
    expect(bodies).toHaveLength(1);
    expect(localStorage.getItem("draft:hub/c1")).toBe("再看看测试");
    expect(host.querySelector('[data-testid="readiness-failure"]')?.textContent).toContain("ultracode 等这一轮结束后生效");
  });
});
