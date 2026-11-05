// The chat's header and what sits under the chat on a phone (#1996), as markup: the browser checks are
// scripts/test/web_mobile_input_e2e.py.
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { Interaction, Session } from "../../lib/types";
import type { AnsweredCard } from "../../lib/store";
import { Header, spaceAgentsItem } from "./ChatView";
import { HEADER_SLOT, HEADER_SLOT_END, HEADER_SLOT_START } from "./header";
import { Composer } from "./Composer";
import { PendingCards } from "./Interactions";
import { QueueStrip } from "./QueueStrip";
import { AttachmentTray } from "../../ui/AttachmentTray";

// stores without a server snapshot: renderToStaticMarkup needs one
vi.mock("../../lib/links", async (orig) => ({ ...(await orig<typeof import("../../lib/links")>()), useContexts: () => [] }));
vi.mock("../../lib/assistant-chat", async (orig) => ({ ...(await orig<typeof import("../../lib/assistant-chat")>()), usePickedLauncher: () => undefined }));

const session = {
  machine_id: "m",
  session_id: "s",
  title: "补齐网络层的单元测试",
  state: "needs_you",
  space_id: "sp",
  cwd: "/tmp/x",
} as unknown as Session;
const sibling = { ...session, session_id: "s2" } as Session;

const header = (extra: Partial<Parameters<typeof Header>[0]> = {}) =>
  renderToStaticMarkup(<Header session={session} title="补齐网络层的单元测试" machineName="MacBook" siblings={[session, sibling]} onInterrupt={() => {}} onStopLoop={() => {}} onToggleSidebar={() => {}} {...extra} />);

describe("Header", () => {
  it("puts the switch it is given in the middle on a narrow screen, in place of the title", () => {
    const html = header({ topCenter: <span data-switch>助手 | Agents</span> });
    expect(html).toContain("data-switch");
    expect(html).toMatch(/class="flex min-w-0 flex-1 justify-center lg:hidden"><span data-switch/);
    expect(html).toMatch(/class="min-w-0 flex-1 max-lg:hidden"/);
    // the buttons either side sit in slots of one width
    expect(html).toContain(`class="${HEADER_SLOT} ${HEADER_SLOT_START} lg:contents"`);
    expect(html).toContain(`class="${HEADER_SLOT} ${HEADER_SLOT_END} lg:contents"`);
  });

  it("is as before without one", () => {
    const html = header();
    expect(html).not.toContain("justify-center lg:hidden");
    expect(html).not.toContain("max-lg:hidden");
    expect(html).not.toContain("max-lg:w-20");
  });

  it("leaves the title room on a phone: no state pill, 同空间 only from ⋯ (#3576)", () => {
    const html = header();
    expect(html).toMatch(/<span class="hidden @min-\[800px\]:contents"><span[^>]*>.*?需要你/);
    // the 同空间 button is there from sm up, gone below
    expect(html).toMatch(/<button[^>]*aria-label="同空间 1"[^>]*class="[^"]*\bmax-sm:hidden\b/);
    expect(html).toContain("<span>同空间</span>");
  });

  it("marks ⋯ on a phone when a sibling needs you, as the 同空间 button does wider", () => {
    expect(header()).toMatch(/aria-label="更多"[^]*?<span aria-hidden="true" class="[^"]*bg-red[^"]*sm:hidden"/);
    const calm = { ...sibling, state: "idle" } as Session;
    const quiet = { ...session, state: "idle" } as Session;
    expect(header({ session: quiet, siblings: [quiet, calm] })).not.toMatch(/bg-red[^"]*sm:hidden/);
  });
});

describe("spaceAgentsItem, ⋯'s 同空间 row on a phone (#3576)", () => {
  it("counts the others, opens the space's menu and is red when one needs you", () => {
    let opened = 0;
    const item = spaceAgentsItem([sibling], () => opened++);
    expect(item.label).toBe("同空间 1");
    expect(item.icon).toBe("layers");
    expect(item.state).toBe("needs_you");
    void item.onSelect();
    expect(opened).toBe(1);
    expect(spaceAgentsItem([{ ...sibling, state: "working" } as Session], () => {}).state).toBeUndefined();
  });
});

describe("Header, the 终端 | 对话 switch (#2419)", () => {
  it("has the two views in that order, the one showing checked", () => {
    const html = header({ view: "chat", onView: () => {} });
    expect(html).toMatch(/role="radiogroup" aria-label="终端或对话"/);
    expect(html.indexOf("终端</button>")).toBeLessThan(html.indexOf("对话</button>"));
    expect(html).toMatch(/aria-checked="true"[^>]*>对话</);
    expect(html).toMatch(/aria-checked="false"[^>]*>终端</);
    expect(header({ view: "term", onView: () => {} })).toMatch(/aria-checked="true"[^>]*>终端</);
  });
  it("is not there without a terminal to switch to, and the old terminal button is gone", () => {
    expect(header()).not.toContain("终端或对话");
    // the only 终端 button is the phone's one-button switch below
    expect(header({ view: "chat", onView: () => {} }).match(/aria-label="终端"/g)).toHaveLength(1);
    expect(header()).not.toContain('aria-label="终端"');
  });

  it("is one button to the other view on a phone, the two-part switch from sm up (#3576)", () => {
    const chat = header({ view: "chat", onView: () => {} });
    expect(chat).toMatch(/<span class="hidden sm:contents"><div[^>]*role="radiogroup" aria-label="终端或对话"/);
    expect(chat).toMatch(/<span class="relative flex shrink-0 sm:hidden"><button[^>]*aria-label="终端"/);
    const term = header({ view: "term", onView: () => {} });
    expect(term).toMatch(/<span class="relative flex shrink-0 sm:hidden"><button[^>]*aria-label="对话"/);
    expect(header({ view: "term", onView: () => {}, viewDot: true })).toMatch(/aria-label="对话"[^]*?<\/button><span aria-hidden="true" class="pointer-events-none absolute[^"]*bg-red"/);
  });
  it("marks 对话 when the terminal shows and something wants an answer", () => {
    expect(header({ view: "term", onView: () => {}, viewDot: true })).toMatch(/对话<span aria-hidden="true" class="absolute/);
    expect(header({ view: "term", onView: () => {} })).not.toMatch(/对话<span/);
  });
});

const ix = (id: string, kind: Interaction["kind"], extra: Partial<Interaction> = {}) =>
  ({ id, machine_id: "m", session_id: "s", kind, tool_name: "Bash", ...extra }) as Interaction;
const three = [ix("a", "plan", { plan: "## 计划" }), ix("b", "question", { questions: [{ question: "用哪个框架？", options: [{ label: "A" }] }] as Interaction["questions"] }), ix("c", "permission")];
const cards = (o: { narrow: boolean; keyboard?: boolean; interactions?: Interaction[]; answered?: AnsweredCard[] }) =>
  renderToStaticMarkup(<PendingCards interactions={o.interactions ?? three} answered={o.answered ?? []} narrow={o.narrow} keyboard={Boolean(o.keyboard)} onDismissKeyboard={() => {}} />);

describe("PendingCards", () => {
  it("shows every card on a wide screen, in no box of their own", () => {
    const html = cards({ narrow: false });
    expect(html).not.toContain("data-chat-cards");
    expect(html).not.toContain("还有");
    expect(html).toContain("计划待批准");
    expect(html).toContain("用哪个框架？");
    expect(html).toContain("允许使用 Bash？");
  });

  it("scrolls them in a box of their own on a narrow one, the first open and 还有 N 件 for the rest", () => {
    const html = cards({ narrow: true });
    expect(html).toMatch(/data-chat-cards[^>]*overflow-y-auto/);
    expect(html).toContain("还有 2 件");
    // the first card's fold is open, the others' closed (inert)
    const folds = [...html.matchAll(/<div class="grid [^"]*"([^>]*)>/g)].map((m) => m[1]);
    expect(folds).toHaveLength(2);
    expect(folds[0]).not.toContain("inert");
    expect(folds[1]).toContain("inert");
  });

  it("folds them into one line while the keyboard is up", () => {
    const html = cards({ narrow: true, keyboard: true });
    expect(html).toContain("3 件待处理");
    expect(html).not.toContain("还有");
    const folds = [...html.matchAll(/<div class="grid [^"]*"([^>]*)>/g)].map((m) => m[1]);
    expect(folds.every((f) => f.includes("inert"))).toBe(true);
  });

  it("is one line on a phone on its side", () => {
    const html = renderToStaticMarkup(<PendingCards interactions={three} answered={[]} narrow short keyboard={false} onDismissKeyboard={() => {}} />);
    expect(html).toContain("3 件待处理");
    expect(html).not.toContain("还有");
  });

  it("is nothing without cards", () => {
    expect(cards({ narrow: true, interactions: [] })).toBe("");
  });
});

describe("Composer", () => {
  afterEach(() => vi.unstubAllGlobals());
  it("caps the text box by the visible height", () => {
    vi.stubGlobal("localStorage", { getItem: () => null });
    const html = renderToStaticMarkup(<Composer draftKey="t" placeholder="给 Agent 发消息…" onSend={async () => true} />);
    expect(html).toMatch(/<textarea[^>]*style="max-height:min\(240px, calc\(var\(--kb-h\) \* 0.35\)\)"/);
  });
});

describe("tap areas", () => {
  afterEach(() => vi.unstubAllGlobals());
  const hit = (html: string, label: string) => new RegExp(`<button[^>]*aria-label="${label}"[^>]*class="touch-hit `).test(html) || new RegExp(`<button[^>]*class="touch-hit [^"]*"[^>]*aria-label="${label}"`).test(html);

  it("covers the composer's row: + @ / and send", () => {
    vi.stubGlobal("localStorage", { getItem: () => null });
    const html = renderToStaticMarkup(<Composer draftKey="t" placeholder="…" machineId="m" mention={{ self: "t", files: () => [] }} onSend={async () => true} />);
    for (const label of ["添加图片或文件", "引用文件或会话", "命令", "发送"]) expect(hit(html, label), label).toBe(true);
  });

  it("covers a card's ×, a question's options and its arrows", () => {
    const html = cards({ narrow: false });
    expect(hit(html, "交回电脑处理")).toBe(true);
    expect(html).toMatch(/<button[^>]*aria-pressed="false"[^>]*class="touch-hit /);
    expect(hit(html, "下一题") || hit(html, "发送回答")).toBe(true);
  });

  it("covers an attachment's remove button", () => {
    const html = renderToStaticMarkup(<AttachmentTray attachments={[{ id: "a", name: "a.png", url: "blob:a" }]} onRemove={() => {}} />);
    expect(hit(html, "移除 a.png")).toBe(true);
    // its tile does not clip the area
    expect(html).not.toMatch(/group\/att relative overflow-hidden/);
  });
});

describe("QueueStrip", () => {
  const queued = { ...session, queued: ["顺便把截图换掉", "跑一遍 UI 测试"] } as Session;
  it("is one line that opens the messages on a phone on its side", () => {
    const html = renderToStaticMarkup(<QueueStrip session={queued} draftKey="t" canEdit compact />);
    expect(html).toContain('aria-expanded="false"');
    expect(html).toContain("排队中 2 条");
    expect(html).toMatch(/<div class="grid [^"]*"[^>]*inert/);
  });

  it("lists them as before otherwise", () => {
    const html = renderToStaticMarkup(<QueueStrip session={queued} draftKey="t" canEdit />);
    expect(html).not.toContain("aria-expanded");
    expect(html).toContain("跑一遍 UI 测试");
  });
});

describe("Header in a task's panel (#3333)", () => {
  const todoSession = { ...session, todo: { id: "t1", title: "代理的待办" } } as unknown as Session;
  const agent = (key: string) => ({ key, label: key, onSelect: () => {} });

  it("is the panel's only header: the task's title and subtitle, a close button, no 同空间", () => {
    const html = header({ session: todoSession, panel: { onClose: () => {}, title: "修登录崩溃", sub: "在做 · MacBook", agents: [agent("a")] } });
    expect(html).toContain(">修登录崩溃</h1>");
    expect(html).toContain("在做 · MacBook");
    expect(html).toContain('aria-label="关闭面板"');
    expect(html).not.toContain("同空间");
    // the agent's own todo line would repeat the task
    expect(html).not.toContain("代理的待办");
    // one agent: nothing to switch
    expect(html).not.toContain('aria-label="切换 Agent"');
  });

  it("has a ⌄ beside the title for a task with several agents", () => {
    const html = header({ panel: { onClose: () => {}, title: "修登录崩溃", agents: [agent("a"), agent("b")] } });
    expect(html).toContain('aria-label="切换 Agent"');
  });

  it("is as before outside a panel", () => {
    const html = header();
    expect(html).not.toContain('aria-label="关闭面板"');
    expect(html).toContain("同空间");
  });
});
