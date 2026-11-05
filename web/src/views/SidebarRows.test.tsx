// The open conversation's row in the sidebar (#2283): marked for assistive tech, its own fill, its actions without hover.
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { AgentSession } from "../lib/agents";
import type { Session } from "../lib/types";
import { setAgentDetails, setContextAsUsed } from "../lib/context-usage";
import { AgentRow, SessionRow } from "./Sidebar";

// the sidebar's modules read the colour scheme and this browser's settings when they load
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

const session: Session = { machine_id: "mac", session_id: "s1", title: "整理文档", cwd: "/code", status: "idle", state: "idle" };
const rowProps = { s: session, k: "s:mac/s1", reg: () => () => {}, hover: () => {}, to: { name: "session", mid: "mac", sid: "s1" } as const, go: () => () => {}, onMenu: () => {} };
const link = (html: string) => html.match(/<a [^>]*>/)![0];
const more = (html: string) => html.match(/<button [^>]*aria-label="Agent 的操作"[^>]*>/)![0];

describe("an agent's row", () => {
  it("is aria-current with the open row's fill when it is open", () => {
    const html = renderToStaticMarkup(<SessionRow {...rowProps} active />);
    expect(link(html)).toContain('aria-current="page"');
    expect(link(html)).toMatch(/class="[^"]*\bbg-hover-2\b/);
  });
  it("is neither when it is not", () => {
    const html = renderToStaticMarkup(<SessionRow {...rowProps} active={false} />);
    expect(link(html)).not.toContain("aria-current");
    expect(link(html)).not.toContain("bg-hover-2");
  });
  it("keeps its ⋯ shown when open, on hover only otherwise", () => {
    expect(more(renderToStaticMarkup(<SessionRow {...rowProps} active />))).toMatch(/class="[^"]*\bflex\b/);
    expect(more(renderToStaticMarkup(<SessionRow {...rowProps} active={false} />))).toMatch(/class="[^"]*\bhidden\b/);
  });
});

describe("an agent's row with 「Agent 行显示详情」 (#2430)", () => {
  const detailed: Session = { ...session, model: "claude-opus-5-5", context_tokens: 78000, context_window: 200000 };
  const render = (s: Session, extra = {}) => renderToStaticMarkup(<SessionRow {...rowProps} s={s} active={false} {...extra} />);
  afterEach(() => {
    setAgentDetails(false);
    setContextAsUsed(false);
  });
  it("is byte for byte the same as before while the setting is off", () => {
    expect(render(detailed)).toBe(render(session)); // the model and context change nothing
    expect(render(detailed)).not.toContain("opus 5.5");
  });
  it("adds a second line with folder, model and what is left when on", () => {
    setAgentDetails(true);
    const html = render(detailed);
    expect(html).toContain("opus 5.5");
    expect(html).toContain("61%");
    expect(html).toContain("code");
  });
  it("shows what is used with 「上下文显示为已用」 and warns at 80%", () => {
    setAgentDetails(true);
    setContextAsUsed(true);
    expect(render(detailed)).toContain("39%");
    expect(render({ ...detailed, context_tokens: 170000 })).toContain("text-orange");
  });
  it("draws no second line for a session that says nothing, or for a conversation of the assistant", () => {
    setAgentDetails(true);
    const plain = { ...session, cwd: "" };
    expect(render(plain)).not.toContain("flex-col");
    expect(render(detailed, { showState: false })).not.toContain("opus 5.5");
  });
});

describe("a personal agent's row", () => {
  const s = { id: "a1", title: "周报助手", provider: "lark", unreadCount: 0, rankTimeMs: 0 } as AgentSession;
  const props = { s, reg: () => () => {}, hover: () => {}, go: () => () => {}, pinned: false, onMenu: () => {} };
  const pin = (html: string) => html.match(/<button [^>]*aria-label="置顶"[^>]*>/)![0];
  it("is aria-current with the open row's fill and its pin shown when open", () => {
    const html = renderToStaticMarkup(<AgentRow {...props} active />);
    expect(link(html)).toContain('aria-current="page"');
    expect(link(html)).toMatch(/class="[^"]*\bbg-hover-2\b/);
    expect(pin(html)).toMatch(/class="[^"]*\bopacity-100\b/);
  });
  it("shows its pin on hover only otherwise", () => {
    const html = renderToStaticMarkup(<AgentRow {...props} active={false} />);
    expect(link(html)).not.toContain("aria-current");
    expect(pin(html)).toMatch(/class="[^"]*\bopacity-0\b/);
  });
});
