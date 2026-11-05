import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hub = vi.hoisted(() => ({ listeners: new Map<string, (() => void)[]>() }));
vi.mock("./store", () => ({
  onHubEvent: (type: string, l: () => void) => {
    hub.listeners.set(type, [...(hub.listeners.get(type) ?? []), l]);
    return () => {};
  },
}));
vi.mock("../ui/atoms", () => ({ toast: vi.fn() }));

import {
  badgeCount, bulkMessage, bulkScope, bulkScopeName, canDraft, cardText, connectorReference, connectorSources, contextXML, openTitle, openURL, sourceLine, conversationRoute, docChangeLine, emojiLabel, fromLine, getInbox, handOffMessage, interactionAsk,
  interactionMuted, interactionTitle, itemPath, kindLabel, labelIsUrgent, mailAccounts, mergeMessages, momentText, nextLarkPosition, partRuns, reloadInbox,
  replyLarkMessage, resetInboxForTests, sections, sendLarkMessage, sendReply, setAllDone, setDocStatus, setDone, shortTime, triage, triageBanner, TRIAGE_REASONS, TRIAGE_SETTINGS,
  unsentReplies, waitingAgents, withToken, type InboxFilter, type InboxItem, type InboxTriage, type LarkMessage,
} from "./inbox";
import { toast } from "../ui/atoms";
import type { AgentMutes } from "./agent-groups";
import type { AssistantInfo, Interaction, Session } from "./types";

const at = (minutes: number) => new Date(Date.UTC(2026, 9, 30, 10, 0) - minutes * 60_000).toISOString();
const item = (id: string, patch: Partial<InboxItem> = {}): InboxItem => ({
  id, source: "lark", kind: "dm", account: "feishu", account_name: "飞书", title: id, preview: "内容", count: 1, updated_at: at(10), tier: "action", ...patch,
});
const all: InboxFilter = { source: "", kind: "", query: "" };
const ids = (list: { id: string }[]) => list.map((x) => x.id);

const dm = item("lark:f1:1", { kind: "dm", sender: "示例甲", lark: { feed_id: "f1", is_group: false, start_position: "1" }, summary: "问周五能否发布", updated_at: at(5) });
const mention = item("lark:f2:1", { kind: "mention", sender: "示例乙", lark: { feed_id: "f2", is_group: true, start_position: "1" }, updated_at: at(20) });
const atAll = item("lark:f4:1", { kind: "mention", lark: { feed_id: "f4", is_group: true, at_all: true }, tier: "fyi" });
const bot = item("lark:f5:1", { kind: "bot", tier: "noise" });
const group = item("lark:f3:1", { kind: "group", sender: "示例丙", lark: { feed_id: "f3", is_group: true }, tier: "fyi", updated_at: at(1) });
const doc = item("doc:n1:1", { kind: "doc", preview: "示例乙提及了你", doc: { url: "https://example.com/base/demo", type: "base", notice: "mention",
  rows: [{ ref: "t/r1", title: "压测报告", status: [{ field: "f1", name: "状态", value: "进行中", options: [{ name: "进行中" }, { name: "已完成" }] }] }] } });
const comment = item("doc:n2:1", { kind: "doc", doc: { url: "https://example.com/docx/demo", type: "docx", notice: "comment" } });
const moment = item("moment:p1:n1", { kind: "moment", sender: "示例丁", moment: { post_id: "p1", notice: "atInComment" }, tier: "fyi" });
const mail = item("mail:acc1:t1:501", { source: "mail", kind: "mail", account: "acc1", account_name: "个人邮箱", sender: "Lucy", title: "分享改期",
  mail: { account_id: "acc1", mailbox: "INBOX", uids: [500, 501], from: { name: "Lucy", email: "lucy@example.com" } }, tier: "" });
const mail2 = item("mail:acc2:t2:9", { source: "mail", kind: "mail", account: "acc2", account_name: "工作邮箱", tier: "noise",
  mail: { account_id: "acc2", mailbox: "INBOX", uids: [9] } });
const done = item("lark:f6:1", { done_at: at(30), done_by: "user" });
const doneLater = item("lark:f7:1", { done_at: at(2), done_by: "replied", tier: "noise" });

describe("一件事", () => {
  it("还没分拣的算需要你，角标只数未处理的需要你", () => {
    expect(triage(item("x", { tier: "" }))).toBe("action");
    expect(triage(item("x", { tier: undefined }))).toBe("action");
    expect(triage(group)).toBe("fyi");
    expect(badgeCount([dm, mention, doc, mail, group, done, doneLater, bot])).toBe(4);
  });

  it("类型标签：@我 红色，@所有人、评论、公司圈、机器人", () => {
    expect([dm, mention, atAll, bot, doc, comment, moment, mail].map(kindLabel)).toEqual(["私聊", "@我", "@所有人", "机器人", "@我", "评论", "@我", undefined]);
    expect(labelIsUrgent(mention)).toBe(true);
    expect(labelIsUrgent(doc)).toBe(true);
    expect(labelIsUrgent(moment)).toBe(true);
    expect(labelIsUrgent(comment)).toBe(false);
    expect(labelIsUrgent(dm)).toBe(false);
  });

  it("「发件人：内容」：私聊不重复发件人，文档写类型", () => {
    expect(fromLine(dm)).toBe("内容");
    expect(fromLine(group)).toBe("示例丙：内容");
    expect(fromLine(doc)).toBe("多维表格 · 示例乙提及了你");
    expect(fromLine(mail)).toBe("Lucy：内容");
    expect(fromLine(mail, "译文")).toBe("Lucy：译文"); // the hub's translation of a foreign mail's preview
    expect(fromLine(doc, "译文")).toBe("多维表格 · 示例乙提及了你");
    expect(fromLine(moment)).toBe("示例丁：内容");
  });

  it("AI 分身提议的改状态写成一行", () => {
    expect(docChangeLine({ ref: "t/r1", field: "f1", value: "已完成" }, doc.doc)).toBe("状态：进行中 → 已完成 · 压测报告");
    expect(docChangeLine({ ref: "gone", field: "f1", value: "已完成" }, doc.doc)).toBe("已完成");
  });
});

describe("筛选和分段", () => {
  const items = [dm, mention, atAll, bot, group, doc, comment, moment, mail, mail2, done, doneLater];

  it("按 tier 分段、新的在前；已处理按处理时间倒序", () => {
    const s = sections(items, all);
    expect(ids(s.tiers.action)).toEqual([dm.id, doc.id, comment.id, mail.id, mention.id]);
    expect(ids(s.tiers.fyi)).toEqual([group.id, atAll.id, moment.id]);
    expect(ids(s.tiers.noise)).toEqual([bot.id, mail2.id]);
    expect(ids(s.done)).toEqual([doneLater.id, done.id]);
  });

  it("来源：飞书按 source，邮箱按账号", () => {
    const feishu = sections(items, { ...all, source: "feishu" });
    expect(feishu.tiers.action.every((x) => x.source === "lark")).toBe(true);
    expect(ids(sections(items, { ...all, source: "acc2" }).tiers.noise)).toEqual([mail2.id]);
    expect(sections(items, { ...all, source: "acc2" }).tiers.action).toEqual([]);
  });

  it("@我 含文档和 @我 的公司圈，群聊含机器人，Agent 不含任何一件", () => {
    const m = sections(items, { ...all, kind: "mention" });
    expect([...ids(m.tiers.action), ...ids(m.tiers.fyi)].sort()).toEqual([atAll.id, comment.id, doc.id, mention.id, moment.id].sort());
    const g = sections(items, { ...all, kind: "group" });
    expect([...ids(g.tiers.fyi), ...ids(g.tiers.noise)].sort()).toEqual([bot.id, group.id].sort());
    const a = sections(items, { ...all, kind: "agent" });
    expect([...a.tiers.action, ...a.tiers.fyi, ...a.tiers.noise, ...a.done]).toEqual([]);
  });

  it("搜索标题、发件人、内容、摘要，不分大小写", () => {
    expect(ids(sections(items, { ...all, query: "周五" }).tiers.action)).toEqual([dm.id]);
    expect(ids(sections(items, { ...all, query: "lucy" }).tiers.action)).toEqual([mail.id]);
    expect(ids(sections(items, { ...all, query: "分享改期" }).tiers.action)).toEqual([mail.id]);
  });

  it("邮箱筛选：没有账号列表时取条目里的账号", () => {
    expect(mailAccounts([], items)).toEqual([{ id: "acc1", email: "个人邮箱" }, { id: "acc2", email: "工作邮箱" }]);
    expect(mailAccounts([{ id: "x", email: "me@example.com" }], items)).toEqual([{ id: "x", email: "me@example.com" }]);
  });
});

describe("等你的 Agent", () => {
  const ix = (id: string, patch: Partial<Interaction> = {}): Interaction => ({ id, machine_id: "m1", session_id: id, kind: "question", created_at: at(5), ...patch });
  const sessions: Record<string, Session[]> = {
    m1: [
      { machine_id: "m1", session_id: "s-space", title: "", cwd: "/tmp/a", status: "blocked", space_name: "项目" },
      { machine_id: "m1", session_id: "s-allowed", title: "", cwd: "/tmp/a", status: "blocked", space_name: "项目" },
      { machine_id: "m1", session_id: "s-jarvis", title: "", cwd: "/tmp/j", status: "blocked", assistant: true },
    ],
  };
  const mutes: AgentMutes = { spaces: ["m1/项目"], sessions: ["m1/s-muted"], allowed: ["m1/s-allowed"] };

  it("免打扰的不算：会话自己，或所在空间（单独开了通知的除外）", () => {
    expect(interactionMuted(ix("s-muted"), sessions, mutes)).toBe(true);
    expect(interactionMuted(ix("s-space"), sessions, mutes)).toBe(true);
    expect(interactionMuted(ix("s-allowed"), sessions, mutes)).toBe(false);
    expect(interactionMuted(ix("s-free"), sessions, mutes)).toBe(false);
  });

  it("只在没选来源时出现，Agent 筛选只看它们，老的在前，能搜", () => {
    const list = [ix("s-new", { created_at: at(1), session_title: "新的" }), ix("s-old", { created_at: at(9), summary: "要不要合并" }), ix("s-muted")];
    expect(ids(waitingAgents(list, sessions, mutes, all))).toEqual(["s-old", "s-new"]);
    expect(ids(waitingAgents(list, sessions, mutes, { ...all, kind: "agent" }))).toEqual(["s-old", "s-new"]);
    expect(waitingAgents(list, sessions, mutes, { ...all, source: "feishu" })).toEqual([]);
    expect(waitingAgents(list, sessions, mutes, { ...all, kind: "dm" })).toEqual([]);
    expect(ids(waitingAgents(list, sessions, mutes, { ...all, query: "合并" }))).toEqual(["s-old"]);
  });

  it("等什么、问什么", () => {
    expect(interactionTitle(ix("a"))).toBe("Claude 在问你");
    expect(interactionTitle(ix("a", { kind: "plan" }))).toBe("计划等你审批");
    expect(interactionTitle(ix("a", { tool_name: "ask_user" }))).toBe("Persona 在问你");
    expect(interactionTitle(ix("a", { agent: "codex", kind: "permission" }))).toBe("Codex 需要你授权");
    expect(interactionAsk(ix("a", { questions: [{ question: "用哪种格式？", options: [] }] }))).toBe("用哪种格式？");
    expect(interactionAsk(ix("a", { kind: "permission", tool_name: "Bash", tool_input: { command: "swift test" } }))).toBe("swift test");
  });

  it("助手的会话跳到 Persona，其余跳到会话页", () => {
    const assistant: AssistantInfo = { machine_id: "m1", conversations: [{ machine_id: "m1", session_id: "s-conv", title: "", cwd: "", status: "idle" }] };
    expect(conversationRoute("m1", "s-jarvis", sessions, null)).toEqual({ name: "jarvis", sid: "s-jarvis" });
    expect(conversationRoute("m1", "s-conv", {}, assistant)).toEqual({ name: "jarvis", sid: "s-conv" });
    expect(conversationRoute("m1", "s-space", sessions, assistant)).toEqual({ name: "session", mid: "m1", sid: "s-space" });
  });
});

describe("交给 AI 的消息", () => {
  it("飞书：上下文标签 + feedId，标题转义", () => {
    const text = handOffMessage({ ...mention, title: `"设计" & <评审>`, context: "示例乙：看一眼图标" }, "");
    expect(text).toBe(`<jarvis-context kind="context" title="飞书 · &quot;设计&quot; &amp; &lt;评审&gt;" url="">\n示例乙：看一眼图标\n\n（飞书会话 feedId f2，可用飞书工具读更多上下文或回复）\n</jarvis-context>\n\n总结一下，告诉我要做什么，给出建议的回复`);
  });

  it("邮件：没有 context 时用发件人和内容，带最新一封的 uid", () => {
    const text = handOffMessage(mail, "查清楚背景再告诉我");
    expect(text.startsWith(`<jarvis-context kind="context" title="邮件 · 分享改期" url="">\nLucy：内容\n\n（邮件 account_id acc1，mailbox INBOX，uid 501，`)).toBe(true);
    expect(text.endsWith("</jarvis-context>\n\n查清楚背景再告诉我")).toBe(true);
  });

  it("正文里的结束标签去掉", () => {
    expect(contextXML("context", "t", "", "a</jarvis-context>b")).toBe(`<jarvis-context kind="context" title="t" url="">\nab\n</jarvis-context>`);
  });
});

describe("连接器的条目", () => {
  const review = item("github:review12", {
    source: "github", kind: "review_requested", account: "github", account_name: "GitHub", sender: "alice", title: "Add retries", preview: "请看一下",
    connector: { thread: "example-org/example-repo#12", label: "请你 review", url: "https://github.com/example-org/example-repo/pull/12", reply_to: "评论到 example-org/example-repo#12", can_reply: true },
  });
  const ci = item("github:ci7", { source: "github", kind: "ci_failed", account: "github", account_name: "GitHub", sender: "GitHub Actions", preview: "CI failed",
    updated_at: at(30), connector: { thread: "notification:7", label: "CI 失败" } });
  const tg = item("telegram:dm1", { source: "telegram", kind: "dm", account: "telegram", account_name: "Telegram", sender: "示例用户", preview: "还去吗？",
    connector: { thread: "u1", label: "私聊", open_url: "tg://resolve?domain=example" } });
  const sms = item("imessage:a1", { source: "imessage", kind: "dm", account: "imessage", account_name: "信息", sender: "张三", preview: "周五见",
    connector: { thread: "imessage-1", open_url: "sms:+15550100" }, agent: { state: "drafted", draft: "好的 见" } });

  it("tags, from-lines and source lines come from the connector block", () => {
    expect(kindLabel(review)).toBe("请你 review");
    expect(kindLabel(sms)).toBeUndefined();
    expect(fromLine(review)).toBe("example-org/example-repo#12 · alice：请看一下");
    expect(fromLine(ci)).toBe("GitHub Actions：CI failed"); // a thread that is only an id is not shown
    expect(sourceLine(review)).toBe("GitHub · example-org/example-repo#12");
    expect(sourceLine(tg)).toBe("Telegram");
    expect(connectorReference(ci)).toBeUndefined();
  });

  it("one filter chip per connector, and the chip filters by it", () => {
    expect(connectorSources([dm, review, tg, ci, mail])).toEqual([{ id: "github", name: "GitHub" }, { id: "telegram", name: "Telegram" }]);
    expect(ids(sections([dm, review, tg, ci], { ...all, source: "github" }).tiers.action)).toEqual([review.id, ci.id]);
    expect(ids(sections([dm, review], { ...all, source: "feishu" }).tiers.action)).toEqual([dm.id]);
  });

  it("drafts only what the hub can answer; the rest open where they are answered", () => {
    expect([review, ci, tg, dm].map(canDraft)).toEqual([true, false, false, true]);
    expect(openTitle(tg)).toBe("在 Telegram 里回复");
    expect(openTitle(sms)).toBe("在信息里回复");
    expect(openTitle(ci)).toBe("打开");
    // a plugin's connector with a link of its own names the connector, never Telegram
    expect(openTitle(item("example:1", { source: "example", account_name: "示例", connector: { thread: "t1", open_url: "https://example.com/t1" } }))).toBe("在 示例 里打开");
    expect(openURL(sms)).toBe("sms:+15550100&body=%E5%A5%BD%E7%9A%84%20%E8%A7%81");
    expect(openURL(tg)).toBe("tg://resolve?domain=example");
    expect(openURL(ci)).toBeUndefined();
  });

  it("hands off with the connector's name and link", () => {
    const text = handOffMessage({ ...review, context: "PR 的描述" }, "");
    expect(text).toContain('title="GitHub · Add retries"');
    expect(text).toContain("（GitHub：https://github.com/example-org/example-repo/pull/12）");
  });

  it("asks the hub for the connectors' items too", async () => {
    resetInboxForTests();
    vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
    const fetcher = vi.fn().mockResolvedValue({ ok: true, status: 200, text: async () => JSON.stringify({ items: [review] }) });
    vi.stubGlobal("fetch", fetcher);
    await reloadInbox();
    expect(fetcher.mock.calls[0][0]).toBe("/api/v1/inbox?sources=all");
    expect(getInbox().items.map((x) => x.id)).toEqual([review.id]);
    vi.unstubAllGlobals();
  });
});

describe("小工具", () => {
  it("路径里的冒号原样保留，其余编码", () => {
    expect(itemPath("lark:f1:100")).toBe("/inbox/lark:f1:100");
    expect(itemPath("mail:acc/1:t 1:5")).toBe("/inbox/mail:acc%2F1:t%201:5");
  });

  it("hub 的图片带上 token", () => {
    expect(withToken("/api/v1/lark/media/k", "jd_x")).toBe("/api/v1/lark/media/k?token=jd_x");
    expect(withToken("/api/v1/lark/media/k?size=48", "jd_x")).toBe("/api/v1/lark/media/k?size=48&token=jd_x");
    expect(withToken("https://example.com/a.png", "jd_x")).toBe("https://example.com/a.png");
    expect(withToken(undefined, "jd_x")).toBeUndefined();
  });

  it("时间：今天、昨天、本周、今年、往年", () => {
    const now = new Date(2026, 9, 30, 15, 0);
    expect(shortTime(new Date(2026, 9, 30, 9, 5).toISOString(), now)).toBe("09:05");
    expect(shortTime(new Date(2026, 9, 29, 23, 0).toISOString(), now)).toBe("昨天");
    expect(shortTime(new Date(2026, 9, 27, 12, 0).toISOString(), now)).toBe("周二");
    expect(shortTime(new Date(2026, 6, 1, 12, 0).toISOString(), now)).toBe("7月1日");
    expect(shortTime(new Date(2025, 0, 2, 12, 0).toISOString(), now)).toBe("2025-01-02");
    expect(shortTime("0001-01-01T00:00:00Z", now)).toBe("");
    expect(shortTime(undefined, now)).toBe("");
  });
});

describe("飞书消息", () => {
  it("卡片简化成标题和文字，不要按钮", () => {
    const meta = { title: "构建通知", elements: [
      { tag: "div", text: { content: "版本 2.3.0 的候选包已经构建完成" } },
      { tag: "div", fields: [{ is_short: true, text: { content: "分支：release" } }, { is_short: true, text: { content: "耗时：12 分钟" } }] },
      { tag: "hr" },
      { tag: "action", actions: [{ tag: "button", text: { content: "查看详情" } }] },
    ] };
    expect(cardText(meta)).toEqual({ title: "构建通知", lines: ["版本 2.3.0 的候选包已经构建完成", "分支：release", "耗时：12 分钟"] });
    expect(cardText({ header: { title: { content: "标题" } }, body: { elements: [{ tag: "markdown", content: "**正文**" }] } })).toEqual({ title: "标题", lines: ["**正文**"] });
    expect(cardText(undefined)).toEqual({ title: "", lines: [] });
  });

  it("行内的连在一起，图片、卡片单独一块", () => {
    const runs = partRuns([{ kind: "text", text: "看" }, { kind: "at", text: "@我" }, { kind: "image", url: "/api/v1/lark/media/a" }, { kind: "link", text: "链接", url: "https://example.com" }, { kind: "card" }]);
    expect(runs.map((r) => [r.inline, r.parts.length])).toEqual([[true, 2], [false, 1], [true, 1], [false, 1]]);
  });

  it("按位置排序、同 id 只留一份", () => {
    const m = (id: string, position: string, text = ""): LarkMessage => ({ id, position, text });
    const merged = mergeMessages([m("a", "9"), m("b", "10")], [m("c", "100"), m("b", "10", "新")]);
    expect(merged.map((x) => x.id)).toEqual(["a", "b", "c"]);
    expect(merged[1].text).toBe("新");
  });

  it("表情没有图时写成 [名字]", () => {
    expect(emojiLabel("Lark_Emoji_SMILE_0")).toBe("[SMILE]");
    expect(emojiLabel("THUMBSUP")).toBe("[THUMBSUP]");
    expect(emojiLabel("X", "[微笑]")).toBe("[微笑]");
    expect(momentText({ segments: [{ kind: "text", text: "好" }, { kind: "emoji", key: "THUMBSUP" }] })).toBe("好[THUMBSUP]");
    expect(momentText({ text: "纯文字" })).toBe("纯文字");
  });
});

describe("全部标为已处理的文案", () => {
  it("范围和件数", () => {
    expect(bulkScope(["", ""], "", 12)).toBe("全部 · 12 件");
    expect(bulkScope(["飞书", "@我"], "", 3)).toBe("飞书 · @我 · 3 件");
    expect(bulkScope([], " 周报 ", 2)).toBe("搜索「周报」 · 2 件");
    expect(bulkScope(["飞书"], "", 0)).toBe("没有未处理的");
  });
  it("范围的名字不带件数", () => {
    expect(bulkScopeName(["飞书", ""], "周报")).toBe("飞书 · 搜索「周报」");
    expect(bulkScopeName([], "")).toBe("全部");
  });
  it("结果", () => {
    expect(bulkMessage("dismiss", { changed: [dm], unchanged: [], skipped: [{ id: "b", reason: "changed" }] })).toBe("已忽略 1 件，1 件有新消息，留在收件箱");
    expect(bulkMessage(true, { changed: [], unchanged: ["a"], skipped: [] })).toBe("没有要标的");
    expect(bulkMessage(false, { changed: [], unchanged: [], skipped: [{ id: "a", reason: "missing" }] })).toBe("没有要移回的");
  });
});

describe("列表状态", () => {
  const ok = (body: unknown) => ({ ok: true, status: 200, text: async () => JSON.stringify(body) });
  const fail = (status: number, error: string) => ({ ok: false, status, text: async () => JSON.stringify({ error }) });
  let fetcher: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    resetInboxForTests();
    vi.mocked(toast).mockClear();
    vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
    fetcher = vi.fn().mockResolvedValueOnce(ok({ items: [dm, doc, mail] }));
    vi.stubGlobal("fetch", fetcher);
    await reloadInbox();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("记下分拣的状态；老 hub 没有就是 null，恢复 ok 后横幅消失", async () => {
    expect(getInbox().triage).toBeNull();
    const waiting: InboxTriage = { state: "unconfigured", waiting: 3, sources: [{ source: "lark", name: "飞书", state: "unconfigured", waiting: 3 }] };
    fetcher.mockResolvedValueOnce(ok({ items: [dm], triage: waiting }));
    await reloadInbox();
    expect(getInbox().triage).toEqual(waiting);
    expect(triageBanner(getInbox().triage)?.text).toBe("有 3 条飞书消息在等分拣：还没配分拣模型");
    fetcher.mockResolvedValueOnce(ok({ items: [dm], triage: { state: "ok", waiting: 0 } }));
    await reloadInbox();
    expect(triageBanner(getInbox().triage)).toBeNull();
    fetcher.mockResolvedValueOnce(ok({ items: [dm] }));
    await reloadInbox();
    expect(getInbox().triage).toBeNull();
  });

  it("拉列表；请求进行中再要一次会在之后补一次", async () => {
    expect(getInbox().items.map((x) => x.id)).toEqual([dm.id, doc.id, mail.id]);
    fetcher.mockResolvedValueOnce(ok({ items: [dm] })).mockResolvedValueOnce(ok({ items: [mail] }));
    const a = reloadInbox();
    const b = reloadInbox();
    await Promise.all([a, b]);
    expect(fetcher).toHaveBeenCalledTimes(3);
    expect(getInbox().items.map((x) => x.id)).toEqual([mail.id]);
  });

  it("标已处理先改本地，失败恢复并提示", async () => {
    let finish: (v: unknown) => void = () => {};
    fetcher.mockReturnValueOnce(new Promise((r) => (finish = r)));
    const p = setDone(dm, true);
    expect(getInbox().items[0].done_at).toBeTruthy();
    expect(getInbox().items[0].done_by).toBe("user");
    finish(fail(500, "boom"));
    await p;
    expect(getInbox().items[0].done_at).toBeUndefined();
    expect(toast).toHaveBeenCalledWith("没标成已处理：boom", "error");
    const [url, init] = fetcher.mock.calls[1];
    expect(url).toBe("/api/v1/inbox/lark:f1:1/done");
    expect(JSON.parse(init.body)).toEqual({ done: true });
  });

  it("全部标为已处理：一次请求带版本；有新消息的留着；撤销移回；失败全部恢复", async () => {
    const versioned = [{ ...dm, version: "v1" }, { ...doc, version: "v2" }, { ...mail, version: "v3" }];
    fetcher.mockReset().mockResolvedValueOnce(ok({ items: versioned }));
    await reloadInbox();
    const [a, b] = getInbox().items;
    // The hub files a; b took a new message since the list.
    fetcher.mockResolvedValueOnce(ok({ changed: [{ ...a, done_at: at(0), done_by: "user" }], unchanged: [], skipped: [{ id: b.id, reason: "changed" }] }))
      .mockResolvedValueOnce(ok({ items: versioned }));
    await setAllDone([a, b, a], true);
    const [url, init] = fetcher.mock.calls[1];
    expect(url).toBe("/api/v1/inbox/bulk");
    expect(JSON.parse(init.body)).toEqual({ action: "done", items: [{ id: a.id, version: "v1" }, { id: b.id, version: "v2" }] });
    expect(fetcher).toHaveBeenCalledTimes(3); // the list again, for b's new message
    const [message, tone, action] = vi.mocked(toast).mock.lastCall!;
    expect([message, tone]).toEqual(["已将 1 件标为已处理，1 件有新消息，留在收件箱", "ok"]);
    expect(action?.label).toBe("撤销");

    // 撤销 sends back only what was filed.
    vi.mocked(toast).mockClear();
    fetcher.mockReset().mockResolvedValueOnce(ok({ changed: [a], unchanged: [], skipped: [] }));
    action!.run();
    await vi.waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual({ action: "reopen", items: [{ id: a.id, version: "v1" }] });
    expect(vi.mocked(toast).mock.lastCall).toEqual(["已移回 1 件", "ok", undefined]);

    // A failure puts every item back.
    fetcher.mockReset().mockResolvedValueOnce(fail(502, "boom"));
    await setAllDone(getInbox().items, true);
    expect(getInbox().items.every((x) => !x.done_at)).toBe(true);
    expect(toast).toHaveBeenLastCalledWith("没标成已处理：boom", "error");
  });

  it("全部忽略：done_by=dismissed，撤销移回；失败恢复", async () => {
    const [a, b] = getInbox().items;
    fetcher.mockResolvedValueOnce(ok({ action: "dismiss", changed: [{ ...a, done_at: at(0), done_by: "dismissed" }, { ...b, done_at: at(0), done_by: "dismissed" }], unchanged: [], skipped: [], ids: [a.id, b.id] }));
    const p = setAllDone([a, b], "dismiss");
    expect(getInbox().items[0].done_by).toBe("dismissed");
    await p;
    expect(JSON.parse(fetcher.mock.calls[1][1].body).action).toBe("dismiss");
    const [message, , action] = vi.mocked(toast).mock.lastCall!;
    expect(message).toBe("已忽略 2 件");
    fetcher.mockResolvedValueOnce(ok({ action: "reopen", changed: [a, b], unchanged: [], skipped: [], ids: [a.id, b.id] }));
    action!.run();
    await vi.waitFor(() => expect(getInbox().items[0].done_by).toBeFalsy());
    expect(JSON.parse(fetcher.mock.calls[2][1].body).action).toBe("reopen");

    fetcher.mockResolvedValueOnce(fail(502, "boom"));
    await setAllDone([a], "dismiss");
    expect(getInbox().items[0].done_at).toBeFalsy();
    expect(toast).toHaveBeenLastCalledWith("没忽略成：boom", "error");
  });

  it("老 hub 没有 /inbox/bulk：已处理退回 /inbox/done，忽略照实报错", async () => {
    const [a] = getInbox().items;
    fetcher.mockResolvedValueOnce(fail(405, "method not allowed")).mockResolvedValueOnce(ok({ changed: [{ ...a, done_at: at(0), done_by: "user" }], unchanged: [], skipped: [] }));
    await setAllDone([a], "done");
    expect(fetcher.mock.calls[2][0]).toBe("/api/v1/inbox/done");
    expect(JSON.parse(fetcher.mock.calls[2][1].body)).toEqual({ done: true, items: [{ id: a.id }] });
    expect(getInbox().items[0].done_by).toBe("user");

    const [, b] = getInbox().items;
    fetcher.mockResolvedValueOnce(fail(404, "not found"));
    await setAllDone([b], "dismiss");
    expect(fetcher).toHaveBeenCalledTimes(4);
    expect(getInbox().items[1].done_at).toBeFalsy();
  });

  it("发送：成功标为已处理；失败放回、留下草稿", async () => {
    fetcher.mockResolvedValueOnce(ok({ ...mail, done_at: at(0), done_by: "replied" }));
    expect(await sendReply(mail, "好的")).toBe(true);
    expect(getInbox().items[2].done_by).toBe("replied");
    expect(JSON.parse(fetcher.mock.calls[1][1].body)).toEqual({ text: "好的" });
    expect(toast).toHaveBeenLastCalledWith("已发送，标为已处理", "ok");

    fetcher.mockResolvedValueOnce(fail(502, "lark-connector: send timed out"));
    expect(await sendReply(dm, "能赶上")).toBe(false);
    expect(getInbox().items[0].done_at).toBeUndefined();
    expect(unsentReplies.get(dm.id)).toBe("能赶上");
    expect(getInbox().sending).toEqual([]);
  });

  it("文档的改动随发送一起带上", async () => {
    fetcher.mockResolvedValueOnce(ok(doc));
    await sendReply(doc, "", [{ ref: "t/r1", field: "f1", value: "已完成" }]);
    expect(JSON.parse(fetcher.mock.calls[1][1].body)).toEqual({ text: "", changes: [{ ref: "t/r1", field: "f1", value: "已完成" }] });
  });

  it("改状态：先改本地，换成 hub 重读的那份；失败改回", async () => {
    const row = doc.doc!.rows![0], st = row.status![0];
    const fresh = { ...doc, summary: "重读过", doc: { ...doc.doc!, rows: [{ ...row, status: [{ ...st, value: "已完成" }] }] } };
    fetcher.mockResolvedValueOnce(ok(fresh));
    const p = setDocStatus(doc, row, st, "已完成");
    expect(getInbox().items[1].doc!.rows![0].status![0].value).toBe("已完成");
    expect(getInbox().moving).toEqual([`${doc.id}|t/r1|f1`]);
    await p;
    expect(getInbox().items[1].summary).toBe("重读过");
    expect(getInbox().moving).toEqual([]);

    fetcher.mockResolvedValueOnce(fail(409, "文档刚被改过"));
    await setDocStatus(getInbox().items[1], row, { ...st, value: "已完成" }, "进行中");
    expect(getInbox().items[1].doc!.rows![0].status![0].value).toBe("已完成");
    expect(toast).toHaveBeenLastCalledWith("没改成：文档刚被改过", "error");
  });

  it("发送飞书原生消息与回复接口正确构造请求", async () => {
    fetcher.mockResolvedValueOnce(ok({ message_id: "m_new_123" }));
    const res = await sendLarkMessage("feed-xyz", "你好", "client-1");
    expect(res).toEqual({ message_id: "m_new_123" });
    expect(fetcher.mock.calls[1][0]).toBe("/api/v1/lark/conversations/feed-xyz/messages");
    expect(JSON.parse(fetcher.mock.calls[1][1].body)).toEqual({ text: "你好", client_id: "client-1" });

    fetcher.mockResolvedValueOnce(ok({ message_id: "m_reply_456" }));
    const rep = await replyLarkMessage("root-msg", "收到", false, "client-2");
    expect(rep).toEqual({ message_id: "m_reply_456" });
    expect(fetcher.mock.calls[2][0]).toBe("/api/v1/lark/messages/root-msg/replies");
    expect(JSON.parse(fetcher.mock.calls[2][1].body)).toEqual({ text: "收到", thread: false, client_id: "client-2" });

    // Another Feishu account's chat names it (docs/feishu.md「多账号」).
    fetcher.mockResolvedValueOnce(ok({ message_id: "m_other" }));
    await sendLarkMessage("feed-xyz", "你好", "client-3", 2);
    expect(fetcher.mock.calls[3][0]).toBe("/api/v1/lark/conversations/feed-xyz/messages?ol_account=2");
    fetcher.mockResolvedValueOnce(ok({ message_id: "m_other_reply" }));
    await replyLarkMessage("root-msg", "收到", true, "client-4", 2);
    expect(fetcher.mock.calls[4][0]).toBe("/api/v1/lark/messages/root-msg/replies?ol_account=2");
  });

  it("nextLarkPosition 能够基于现有消息计算下一个位置", () => {
    expect(nextLarkPosition([])).toBeTruthy();
    expect(nextLarkPosition([{ id: "1", position: "10" }])).toBe("11");
    expect(nextLarkPosition([{ id: "1", position: "10" }, { id: "2", position: "25" }])).toBe("26");
  });
});

describe("等分拣的横幅（#1773）", () => {
  const src = (source: string, name: string, waiting: number, state = "behind") => ({ source, name, state, waiting });
  it("一个来源写出它的名字，几个来源不写；原因是 hub 的", () => {
    expect(triageBanner({ state: "unconfigured", waiting: 3, sources: [src("lark", "飞书", 3, "unconfigured")] })).toEqual({ text: "有 3 条飞书消息在等分拣：还没配分拣模型", configure: true });
    expect(triageBanner({ state: "behind", waiting: 12, sources: [src("lark", "飞书", 7), src("mail", "邮件", 5)] })).toEqual({ text: "有 12 条消息在等分拣：新消息太多，正在排队", configure: false });
    expect(triageBanner({ state: "error", reason: "模型没有额度", waiting: 2, sources: [src("github", "GitHub", 2, "error")] })?.text).toBe("有 2 条GitHub消息在等分拣：模型没有额度");
    // a source without a name, or one that waits for nothing, is not named
    expect(triageBanner({ state: "timeout", waiting: 4, sources: [src("x", "", 4, "timeout")] })?.text).toBe("有 4 条消息在等分拣：分拣模型太慢了");
    expect(triageBanner({ state: "paused", waiting: 4, sources: [src("lark", "飞书", 4, "paused"), src("mail", "邮件", 0, "ok")] })?.text).toBe("有 4 条飞书消息在等分拣：分拣模型暂停了");
    expect(triageBanner({ state: "paused", waiting: 1 })?.text).toBe("有 1 条消息在等分拣：分拣模型暂停了");
  });
  it("每个状态有自己的话；不认识的状态又没有原因只说条数", () => {
    for (const [state, words] of Object.entries(TRIAGE_REASONS)) {
      expect(triageBanner({ state, waiting: 1 })).toEqual({ text: `有 1 条消息在等分拣：${words}`, configure: state === "unconfigured" });
    }
    expect(Object.keys(TRIAGE_REASONS)).toEqual(["unconfigured", "paused", "error", "timeout", "behind"]);
    expect(triageBanner({ state: "later", waiting: 2 })).toEqual({ text: "有 2 条消息在等分拣", configure: false });
    expect(triageBanner({ state: "error", reason: "  ", waiting: 2 })?.text).toBe("有 2 条消息在等分拣：分拣模型出错了");
  });
  it("ok、没有等的、老 hub 都不显示", () => {
    expect(triageBanner(null)).toBeNull();
    expect(triageBanner(undefined)).toBeNull();
    expect(triageBanner({ state: "ok", waiting: 0 })).toBeNull();
    expect(triageBanner({ state: "ok", waiting: 3 })).toBeNull();
    expect(triageBanner({ state: "unconfigured", waiting: 0 })).toBeNull();
    expect(triageBanner({} as InboxTriage)).toBeNull();
  });
  it("去配置打开信息筛选的模型配置", () => {
    expect(TRIAGE_SETTINGS).toBe("/settings?tab=model-config&purpose=inbox.triage");
  });
});
