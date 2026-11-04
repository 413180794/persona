// The unified inbox (docs/inbox.md): the hub's items (Feishu chats, mail threads, Feishu documents, removed posts, and
// the connectors' threads: GitHub, Telegram…, drawn from their generic `connector` block),
// kept in one module-level list so the sidebar's badge and the page share it. Pulled on first use, again on `inbox`
// pushes and on (re)connect. Marking done applies at once and is sent in the background; a failure puts it back.
//
// The pure parts (filters, sections, labels, the 交给 AI message, Feishu message parts) are plain functions so the
// tests can run them without a DOM.

import { useEffect, useRef, useSyncExternalStore } from "react";
import { api, enc, errorText, getToken } from "./api";
import type { AgentMutes } from "./agent-groups";
import { connectorOpenTitle } from "./connectors";
import { baseName } from "./format";
import type { Route } from "./router";
import { onHubEvent } from "./store";
import type { AssistantInfo, Interaction, Session } from "./types";
import { toast } from "../ui/atoms";

// ── wire types (docs/protocol.md「收件箱 API」) ─────────

export interface DocOption {
  name: string;
  color?: string;
}

export interface DocStatus {
  field: string;
  name: string;
  value: string;
  options: DocOption[];
}

export interface DocRow {
  ref: string;
  table?: string;
  title: string;
  fields?: { name: string; value: string }[];
  status?: DocStatus[];
}

export interface DocChange {
  ref: string;
  field: string;
  value: string;
}

export interface InboxDoc {
  url: string;
  link?: string;
  type: string; // docx | doc | sheet | base | other
  notice: string; // mention | comment | reply
  notice_id?: string;
  rows?: DocRow[];
  reply?: { kind: string; label: string };
  error?: string;
}

export interface InboxAgent {
  state: string; // skipped | queued | running | drafted | failed
  task?: string;
  reason?: string;
  machine_id?: string;
  session_id?: string;
  draft?: string;
  note?: string;
  error?: string;
  changes?: DocChange[];
}

export interface InboxItem {
  id: string;
  source: string; // lark | mail | a connector's id (github, telegram…)
  kind: string; // dm | mention | group | bot | mail | doc | moment
  account: string;
  account_name: string;
  title: string;
  sender?: string;
  avatar?: string;
  preview: string;
  count: number;
  started_at?: string;
  updated_at?: string;
  tier?: string;
  summary?: string;
  reason?: string;
  read?: boolean;
  done_at?: string | null;
  done_by?: string | null;
  /** What it holds now, as listed: a bulk 已处理 sends it back (an item with new messages since stays open). */
  version?: string;
  lark?: { feed_id: string; is_group: boolean; is_topic?: boolean; start_position?: string; mention_position?: string; at_all?: boolean };
  mail?: { account_id: string; mailbox: string; uids: number[]; message_id?: string; from?: { name?: string; email?: string }; direct?: boolean; thread?: string };
  doc?: InboxDoc;
  moment?: { post_id: string; notice?: string; reply_to?: string; my_post?: boolean; post_text?: string };
  /** A connector's thread (GitHub…): `source` is the connector's id, `account_name` its name. */
  connector?: InboxConnector;
  /** Only in GET /inbox/{id}: the conversation as text. */
  context?: string;
  agent?: InboxAgent;
  /** New foreign mail translated by the hub as it came in (for a device whose 设置 › 翻译 asks), by target language. */
  translations?: Record<string, { subject: string; preview: string }>;
}

export interface InboxConnector {
  thread: string;
  /** The row's tag: 请你 review. */
  label?: string;
  /** The thread on the web. */
  url?: string;
  /** A link the system opens instead (sms:…, tg://…): where the user answers it. */
  open_url?: string;
  /** Where a reply goes and who sees it. */
  reply_to?: string;
  can_reply?: boolean;
  rule_tier?: string;
}

export interface MailAccount {
  id: string;
  name?: string;
  email: string;
}

// ── one item ─────────────────────────────────────────────

export type Tier = "action" | "fyi" | "noise";
export const TIERS: { key: Tier; title: string }[] = [
  { key: "action", title: "需要你" },
  { key: "fyi", title: "知悉即可" },
  { key: "noise", title: "已折叠" },
];

/** Items not triaged yet count as 需要你 until the AI says otherwise. */
export function triage(it: InboxItem): Tier {
  return it.tier === "fyi" || it.tier === "noise" ? it.tier : "action";
}

export function isDone(it: InboxItem): boolean {
  return Boolean(it.done_at);
}

export function isLark(it: InboxItem): boolean {
  return it.source === "lark";
}

/** The thread as people write it (example-org/example-repo#12), undefined when it is only an id. */
export function connectorReference(it: InboxItem): string | undefined {
  const t = it.connector?.thread;
  return t && t.includes("#") ? t : undefined;
}

/** Whether 「AI 草拟回复」 applies: a connector's thread only when the hub can answer it (a CI run cannot). */
export function canDraft(it: InboxItem): boolean {
  return !it.connector || !!it.connector.can_reply;
}

/** The button that opens a connector item's `open_url`, where the user answers it. */
export function openTitle(it: InboxItem): string {
  const url = it.connector?.open_url;
  if (url?.startsWith("sms:")) return "在信息里回复";
  return connectorOpenTitle(it.source) ?? (url ? `在 ${it.account_name || it.source} 里打开` : "打开");
}

/** A connector item's `open_url` as the browser opens it; an sms: one carries the AI 分身's draft as its body. */
export function openURL(it: InboxItem): string | undefined {
  const raw = it.connector?.open_url;
  if (!raw) return undefined;
  const draft = agentDraft(it);
  return raw.startsWith("sms:") && draft ? `${raw}&body=${encodeURIComponent(draft)}` : raw;
}

/** The short tag next to the title. */
export function kindLabel(it: InboxItem): string | undefined {
  if (it.connector) return it.connector.label || undefined;
  switch (it.kind) {
    case "dm":
      return "私聊";
    case "mention":
      return it.lark?.at_all ? "@所有人" : "@我";
    case "bot":
      return "机器人";
    case "doc":
      return it.doc?.notice === "comment" || it.doc?.notice === "reply" ? "评论" : "@我";
    case "moment":
      return it.moment?.notice?.startsWith("atIn") ? "@我" : "removed";
    default:
      return undefined;
  }
}

/** @我 is red (it is about you); every other tag is neutral. */
export function labelIsUrgent(it: InboxItem): boolean {
  return it.kind === "mention" || kindLabel(it) === "@我";
}

export function docTypeName(type?: string): string {
  switch (type) {
    case "sheet":
      return "表格";
    case "base":
      return "多维表格";
    case "docx":
    case "doc":
      return "文档";
    default:
      return "云文档";
  }
}

/** Who wrote what: the row's line under the summary (or its second line before triage); `preview` is the hub's
 * translation of a foreign mail's when it is shown. */
export function fromLine(it: InboxItem, preview?: string): string {
  if (it.doc) return `${docTypeName(it.doc.type)} · ${it.preview}`; // the preview names who: 李雷提及了你
  if (it.moment) return `${it.sender ?? ""}：${it.preview}`;
  if (it.connector) {
    // example-org/example-repo#12 · bob：请看一下
    const who = [it.sender ?? "", it.preview].filter(Boolean).join("：");
    return [connectorReference(it) ?? "", who].filter(Boolean).join(" · ");
  }
  if (preview && it.sender) return `${it.sender}：${preview}`;
  // a Feishu direct chat's title already is the sender
  if (!it.sender || (isLark(it) && it.lark && !it.lark.is_group)) return it.preview;
  return `${it.sender}：${it.preview}`;
}

/** Where it came from, for the detail page's subtitle. */
export function sourceLine(it: InboxItem): string {
  if (it.doc) return [docTypeName(it.doc.type), it.preview].filter(Boolean).join(" · ");
  if (it.moment) return it.moment.my_post ? "removed · 你的帖子" : "removed";
  if (it.connector) return [it.account_name || it.source, connectorReference(it)].filter(Boolean).join(" · ");
  if (isLark(it)) return ["飞书", it.lark?.is_group ? "群聊" : it.kind === "bot" ? "机器人" : "私聊"].join(" · ");
  const from = it.mail?.from;
  return [it.account_name, from?.name && from.email ? `${from.name} <${from.email}>` : from?.email || it.sender].filter(Boolean).join(" · ");
}

export function agentDraft(it: InboxItem): string | undefined {
  return it.agent?.state === "drafted" && it.agent.draft ? it.agent.draft : undefined;
}

export function agentWorking(it: InboxItem): boolean {
  return it.agent?.state === "queued" || it.agent?.state === "running";
}

/** The AI 分身's proposed status move as a line ("状态：进行中 → 已完成 · 写文档"). */
export function docChangeLine(change: DocChange, doc?: InboxDoc): string {
  const row = doc?.rows?.find((r) => r.ref === change.ref);
  const st = row?.status?.find((s) => s.field === change.field);
  if (!row || !st) return change.value;
  return `${st.name}：${st.value || "未设置"} → ${change.value} · ${row.title}`;
}

/** The part of an address before the @ (chips stay short), unless that alone says nothing ("c@…"). */
export function mailShort(email: string): string {
  const name = email.split("@")[0] ?? email;
  return name.length >= 3 ? name : email;
}

/** Item ids are `lark:<feed>:<pos>` / `mail:<acc>:<thread>:<uid>`: the colons stay as they are in the path. */
export function itemPath(id: string): string {
  return `/inbox/${enc(id).replace(/%3A/gi, ":")}`;
}

/** Media under /api/ for an <img>: the token goes in the query (an <img> can't send a header). */
export function withToken(url: string | undefined, token = getToken()): string | undefined {
  if (!url) return undefined;
  if (!url.startsWith("/api/")) return url;
  return `${url}${url.includes("?") ? "&" : "?"}token=${encodeURIComponent(token)}`;
}

/** 18:20 today, 昨天, 周三 within the week, 10月28日 this year, else 2025-10-28 (LarkTime.short). */
export function shortTime(iso?: string, now = new Date()): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return "";
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((day(now) - day(d)) / 86400_000);
  if (days <= 0) return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  if (days === 1) return "昨天";
  if (now.getTime() - d.getTime() < 6 * 86400_000) return ["周日", "周一", "周二", "周三", "周四", "周五", "周六"][d.getDay()];
  if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}月${d.getDate()}日`;
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

// ── filters and sections ─────────────────────────────────

/** source: "" all, "feishu", a mail account id or a connector's id; kind: "" all, mention, dm, group, mail, agent. */
export interface InboxFilter {
  source: string;
  kind: string;
  query: string;
}

export const KIND_FILTERS: { key: string; label: string }[] = [
  { key: "mention", label: "@我" },
  { key: "dm", label: "私聊" },
  { key: "group", label: "群聊" },
  { key: "mail", label: "邮件" },
  { key: "agent", label: "Agent" },
];

function hasText(fields: (string | undefined)[], query: string): boolean {
  const q = query.trim().toLowerCase();
  return !q || fields.some((f) => f?.toLowerCase().includes(q));
}

export function matches(it: InboxItem, f: InboxFilter): boolean {
  if (f.source === "feishu" && !isLark(it)) return false;
  if (f.source && f.source !== "feishu" && it.account !== f.source && it.mail?.account_id !== f.source) return false;
  if (f.kind === "agent") return false; // only the waiting agents
  if (f.kind) {
    const ok = it.kind === f.kind || (f.kind === "group" && it.kind === "bot") || (f.kind === "mention" && (it.kind === "doc" || (it.kind === "moment" && kindLabel(it) === "@我")));
    if (!ok) return false;
  }
  return hasText([it.title, it.sender, it.preview, it.summary], f.query);
}

const byUpdated = (a: InboxItem, b: InboxItem) => (b.updated_at ?? "").localeCompare(a.updated_at ?? "") || a.id.localeCompare(b.id);
const byDoneAt = (a: InboxItem, b: InboxItem) => (b.done_at ?? "").localeCompare(a.done_at ?? "") || a.id.localeCompare(b.id);

export interface InboxSections {
  tiers: Record<Tier, InboxItem[]>;
  done: InboxItem[];
}

/** The open items by triage tier (newest first) and the done ones (most recently done first). */
export function sections(items: InboxItem[], f: InboxFilter): InboxSections {
  const out: InboxSections = { tiers: { action: [], fyi: [], noise: [] }, done: [] };
  for (const it of items) {
    if (!matches(it, f)) continue;
    if (isDone(it)) out.done.push(it);
    else out.tiers[triage(it)].push(it);
  }
  for (const t of TIERS) out.tiers[t.key].sort(byUpdated);
  out.done.sort(byDoneAt);
  return out;
}

/** The sidebar badge: open items that need the user. */
export function badgeCount(items: InboxItem[]): number {
  return items.filter((it) => !isDone(it) && triage(it) === "action").length;
}

/** The connectors with items in the list (GitHub…), one filter chip each, in the order they first come. */
export function connectorSources(items: InboxItem[]): { id: string; name: string }[] {
  const seen = new Map<string, string>();
  for (const it of items) if (it.connector && !seen.has(it.account)) seen.set(it.account, it.account_name || it.source);
  return [...seen].map(([id, name]) => ({ id, name }));
}

/** Mail accounts for the filter chips: the hub's list, else the ones the items name. */
export function mailAccounts(accounts: MailAccount[], items: InboxItem[]): MailAccount[] {
  if (accounts.length) return accounts;
  const seen = new Map<string, MailAccount>();
  for (const it of items) if (it.source === "mail" && !seen.has(it.account)) seen.set(it.account, { id: it.account, email: it.account_name || it.account });
  return [...seen.values()];
}

// ── agents waiting on the user (they head 需要你) ────────

/** Muted: the agent itself, or its space unless the agent was turned back on there (the hub's rule). */
export function interactionMuted(ix: Interaction, sessions: Record<string, Session[]>, mutes: AgentMutes): boolean {
  const key = `${ix.machine_id}/${ix.session_id}`;
  if (mutes.sessions.includes(key)) return true;
  if (mutes.allowed?.includes(key)) return false;
  const space = (sessions[ix.machine_id] ?? []).find((s) => s.session_id === ix.session_id)?.space_name;
  return Boolean(space && mutes.spaces.includes(`${ix.machine_id}/${space}`));
}

/** What it waits for: 「Claude 在问你」, 「计划等你审批」…; Persona's ask_user and other agents by name. */
export function interactionTitle(ix: Interaction): string {
  if (ix.tool_name === "ask_user") return "Persona 在问你";
  const name = ix.agent === "codex" ? "Codex" : ix.agent === "traex" ? "TRAEX" : ix.agent && ix.agent !== "claude" ? ix.agent : "";
  if (name && ix.kind === "question") return `${name} 在问你`;
  if (name && ix.kind === "permission") return `${name} 需要你授权`;
  return { question: "Claude 在问你", plan: "计划等你审批", permission: "需要你授权", screen: "终端等你处理" }[ix.kind];
}

/** What it asks, in a line. */
export function interactionAsk(ix: Interaction): string {
  const input = ix.tool_input as { description?: unknown; command?: unknown } | undefined;
  const str = (v: unknown) => (typeof v === "string" ? v : "");
  switch (ix.kind) {
    case "question":
      return ix.questions?.[0]?.question || ix.summary || "";
    case "permission":
      return ix.summary || str(input?.description) || str(input?.command) || ix.tool_name || "";
    case "screen":
      return ix.dialog?.question || ix.summary || "";
    default:
      return ix.summary || "";
  }
}

export function interactionAgentTitle(ix: Interaction): string {
  return ix.session_title?.trim() || baseName(ix.cwd) || "Agent";
}

export function waitingAgents(interactions: Interaction[], sessions: Record<string, Session[]>, mutes: AgentMutes, f: InboxFilter): Interaction[] {
  if (f.source || (f.kind && f.kind !== "agent")) return [];
  return interactions
    .filter((ix) => !interactionMuted(ix, sessions, mutes) && hasText([ix.session_title, ix.summary, interactionTitle(ix), interactionAsk(ix)], f.query))
    .sort((a, b) => (a.created_at ?? "").localeCompare(b.created_at ?? "") || a.id.localeCompare(b.id));
}

/** A conversation's page: the assistant's are in Persona (/j/<sid>), an agent's is /s/<machine>/<sid>. */
export function conversationRoute(mid: string, sid: string, sessions: Record<string, Session[]>, assistant: AssistantInfo | null): Route {
  const s = (sessions[mid] ?? []).find((x) => x.session_id === sid);
  const isAssistant = s?.assistant || (assistant?.machine_id === mid && assistant.conversations.some((c) => c.session_id === sid));
  return isAssistant ? { name: "jarvis", sid } : { name: "session", mid, sid };
}

// ── 交给 AI ──────────────────────────────────────────────

export const HANDOFF_PRESETS = ["总结一下，告诉我要做什么，给出建议的回复", "查清楚背景再告诉我", "直接帮我处理，回复前先问我"];

function escAttr(s: string) {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/** `<jarvis-context kind="…" title="…" url="…">` the way the app writes it (ContextRef.xml). */
export function contextXML(kind: string, title: string, url: string, text: string): string {
  return `<jarvis-context kind="${kind}" title="${escAttr(title)}" url="${escAttr(url)}">\n${text.replace(/<\/jarvis-context>/g, "")}\n</jarvis-context>`;
}

/** The item as context the chat shows as a chip, then what to do with it (InboxHandOffSheet.message). */
export function handOffMessage(it: InboxItem, ask: string): string {
  const title = (it.connector ? `${it.account_name || it.source} · ` : isLark(it) ? "飞书 · " : "邮件 · ") + it.title;
  let text = it.context?.trim() ? it.context : [it.sender ?? "", it.preview].filter(Boolean).join("：");
  if (it.lark) text += `\n\n（飞书会话 feedId ${it.lark.feed_id}，可用飞书工具读更多上下文或回复）`;
  if (it.connector?.url) text += `\n\n（${it.account_name || it.source}：${it.connector.url}）`;
  const uid = it.mail?.uids[it.mail.uids.length - 1];
  if (it.mail && uid !== undefined) text += `\n\n（邮件 account_id ${it.mail.account_id}，mailbox ${it.mail.mailbox}，uid ${uid}，可用 mail_read 读全文、mail_send 回复）`;
  return `${contextXML("context", title, "", text)}\n\n${ask.trim() || HANDOFF_PRESETS[0]}`;
}

// ── Feishu messages (docs/protocol.md「飞书 API」) ──────

export interface LarkPart {
  kind: string;
  text?: string;
  url?: string;
  refId?: string;
  metadata?: Record<string, unknown>;
}

export interface LarkMessage {
  id: string;
  position: string;
  sender_id?: string;
  sender_name?: string;
  sender_avatar?: string;
  sender_type?: string;
  is_mine?: boolean;
  type?: string;
  text?: string;
  created_at?: string;
  recalled?: boolean;
  edited?: boolean;
  reply_count?: number;
  thread_reply_count?: number;
  at_me?: boolean;
  /** Read state (others' messages) and Feishu's unread badge at this message: what the read report carries. */
  is_read?: boolean;
  badge_count?: number;
  parts?: LarkPart[];
  reply_to?: { id?: string; sender_name?: string; text?: string; recalled?: boolean };
  sending?: boolean;
  failed?: boolean;
  client_id?: string;
  /** lark-connector's capabilities as the hub passes them on: whether a Pin may be added / removed (lark-pins.ts). */
  can_pin?: boolean;
  can_unpin?: boolean;
  deleted?: boolean;
  local_state?: string;
  /** The thread a message is in; "root", "reply" or "" (a plain message). */
  thread_id?: string;
  thread_kind?: string;
  /** An encrypted message's public face is in text / parts already; its presence marks it encrypted. */
  olk?: unknown;
}

export interface LarkPage {
  messages: LarkMessage[];
  has_older?: boolean;
  has_newer?: boolean;
  older_cursor?: string;
  newer_cursor?: string;
  /** A topic group: the messages are its topics' first messages (the hub finds out without being told). */
  topic?: boolean;
}

/** Oldest first by position (numeric when both are), one copy per id: a later page wins. */
export function mergeMessages(have: LarkMessage[], batch: LarkMessage[]): LarkMessage[] {
  const byId = new Map(have.map((m) => [m.id, m]));
  for (const m of batch) byId.set(m.id, m);
  return [...byId.values()].sort((a, b) => comparePosition(a.position, b.position));
}

export function comparePosition(a: string, b: string): number {
  const x = Number(a), y = Number(b);
  if (Number.isFinite(x) && Number.isFinite(y)) return x - y;
  return a.localeCompare(b);
}

/** Inline parts flow in one paragraph; the others (pictures, files, code, cards, system lines) stand alone. */
export function isInlinePart(p: LarkPart): boolean {
  switch (p.kind) {
    case "text":
      return !p.metadata?.markdown && !p.metadata?.system;
    case "at":
    case "link":
    case "docs":
    case "table":
      return true;
    case "emotion":
      return !p.url;
    default:
      return false;
  }
}

/** Parts in runs: consecutive inline ones together, each block part on its own. */
export function partRuns(parts: LarkPart[]): { inline: boolean; parts: LarkPart[] }[] {
  const out: { inline: boolean; parts: LarkPart[] }[] = [];
  for (const p of parts) {
    const inline = isInlinePart(p);
    const last = out[out.length - 1];
    if (inline && last?.inline) last.parts.push(p);
    else out.push({ inline, parts: [p] });
  }
  return out;
}

/** `[微笑]`-style text for an emoji key without its picture. */
export function emojiLabel(key: string, text?: string): string {
  if (text) return text;
  let k = key.replace(/^Lark_Emoji_/, "");
  const us = k.lastIndexOf("_");
  if (us > 0) k = k.slice(0, us);
  return `[${k}]`;
}

const CARD_SKIP = new Set(["button", "action", "overflow", "select_static", "select_person", "date_picker", "picker_time", "picker_datetime", "hr", "img"]);

/** A card simplified to its title and lines of text (no buttons: the page only reads). */
export function cardText(meta: Record<string, unknown> | undefined): { title: string; lines: string[] } {
  if (!meta) return { title: "", lines: [] };
  const str = (v: unknown): string => (typeof v === "string" ? v : v && typeof v === "object" && typeof (v as { content?: unknown }).content === "string" ? (v as { content: string }).content : "");
  const header = meta.header as Record<string, unknown> | undefined;
  const title = str(meta.title) || str(header?.title) || "";
  const lines: string[] = [];
  const walk = (node: unknown) => {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!node || typeof node !== "object") return;
    const o = node as Record<string, unknown>;
    if (typeof o.tag === "string" && CARD_SKIP.has(o.tag)) return;
    const own = str(o.text) || (typeof o.content === "string" ? o.content : "");
    if (own.trim()) lines.push(own.trim());
    for (const [k, v] of Object.entries(o)) if (k !== "text" && k !== "content" && v && typeof v === "object") walk(v);
  };
  walk(meta.elements ?? (meta.body as Record<string, unknown> | undefined)?.elements ?? meta.body);
  return { title, lines: lines.filter((l, i) => l !== title && l !== lines[i - 1]) };
}

// ── removed ───────────────────────────────────────────────

export interface MomentContent {
  text?: string;
  segments?: { kind?: string; text?: string; key?: string; emojiKey?: string }[];
}

export interface MomentPost {
  id: string;
  userName?: string;
  userAvatarUrl?: string;
  content?: MomentContent;
  images?: { url?: string; width?: number; height?: number }[];
  reactionGroups?: { type: string; count: number }[];
  reactionCount?: number;
  commentCount?: number;
  createTime?: number;
  categoryNames?: string[];
}

export interface MomentComment {
  id: string;
  userName?: string;
  userAvatarUrl?: string;
  content?: MomentContent;
  createTime?: number;
  replyUserName?: string;
  reactionCount?: number;
}

/** A post / comment's text, emoji as `[key]`. */
export function momentText(c?: MomentContent): string {
  if (!c?.segments?.length) return c?.text ?? "";
  return c.segments.map((s) => (s.kind === "emoji" ? emojiLabel(s.key ?? s.emojiKey ?? "", s.text) : s.text ?? "")).join("");
}

// ── the list, shared by the page and the sidebar badge ───

interface InboxState {
  items: InboxItem[];
  loaded: boolean;
  loading: boolean;
  error: string;
  /** Items whose reply is being sent (the row shows a spinner, a second tap does nothing). */
  sending: string[];
  /** Status moves being written, by "<item>|<row>|<field>". */
  moving: string[];
}

let state: InboxState = { items: [], loaded: false, loading: false, error: "", sending: [], moving: [] };
const listeners = new Set<() => void>();
/** Replies that did not go out, by item: the draft dialog opens with that text again. */
export const unsentReplies = new Map<string, string>();

function set(patch: Partial<InboxState>) {
  state = { ...state, ...patch };
  listeners.forEach((l) => l());
}

export function getInbox(): InboxState {
  return state;
}

function patchItem(id: string, fn: (it: InboxItem) => InboxItem) {
  set({ items: state.items.map((it) => (it.id === id ? fn(it) : it)) });
}

/** Puts the hub's copy of one item in place (a fresh read after a status move, a reply). */
export function replaceItem(item: InboxItem) {
  if (item?.id) patchItem(item.id, () => item);
}

let started = false;
let inFlight: Promise<void> | null = null;
let again = false;

function start() {
  if (started) return;
  started = true;
  onHubEvent("inbox", () => void reloadInbox());
  onHubEvent("hello", () => void reloadInbox());
  void reloadInbox();
}

/** Pulls the list; a request asked for while one is out runs once more after it (bursts of pushes collapse). */
export function reloadInbox(): Promise<void> {
  if (inFlight) {
    again = true;
    return inFlight;
  }
  inFlight = (async () => {
    set({ loading: true });
    try {
      do {
        again = false;
        const r = await api<{ items?: InboxItem[] }>("/inbox?sources=all");
        set({ items: r.items ?? [], loaded: true, error: "" });
      } while (again);
    } catch (e) {
      set({ error: errorText(e), loaded: true });
    } finally {
      set({ loading: false });
      inFlight = null;
    }
  })();
  return inFlight;
}

/** 刷新: the hub collects Feishu and mail now, then lists. */
export async function refreshInbox(): Promise<void> {
  set({ loading: true });
  try {
    const r = await api<{ items?: InboxItem[] }>("/inbox/refresh?sources=all", { method: "POST" });
    set({ items: r.items ?? [], loaded: true, error: "" });
  } catch (e) {
    toast(`没刷新成功：${errorText(e)}`, "error");
  } finally {
    set({ loading: false });
  }
}

/** 已处理 / 移回: shown at once, sent in the background; a failure puts the item back. */
export async function setDone(item: InboxItem, done: boolean): Promise<void> {
  const before = state.items.find((x) => x.id === item.id);
  if (!before) return;
  patchItem(item.id, (it) => ({ ...it, done_at: done ? new Date().toISOString() : null, done_by: done ? "user" : null }));
  try {
    await api(`${itemPath(item.id)}/done`, { method: "POST", json: { done } });
  } catch (e) {
    patchItem(item.id, () => before);
    toast(`${done ? "没标成已处理" : "没移回"}：${errorText(e)}`, "error");
  }
}

/** What a bulk 已处理 / 撤销 did (POST /api/v1/inbox/done): the items it moved as the hub has them now, the ones
 * already so, and the ones it left (`changed`: new messages since the list; `missing`: gone from the hub). */
export type BulkResult = { changed: InboxItem[]; unchanged: string[]; skipped: { id: string; reason: string }[] };

/** The toast after a bulk request. */
export function bulkMessage(done: boolean, r: BulkResult): string {
  const moved = r.changed.length;
  const fresh = r.skipped.filter((s) => s.reason === "changed").length;
  const head = done ? (moved ? `已将 ${moved} 件标为已处理` : "没有要标的") : (moved ? `已移回 ${moved} 件` : "没有要移回的");
  return fresh ? `${head}，${fresh} 件有新消息，留在收件箱` : head;
}

/** What 全部标为已处理 covers, for its label: the filters' names and the search, or 全部. */
export function bulkScope(filters: string[], query: string, count: number): string {
  const parts = filters.filter(Boolean);
  if (query.trim()) parts.push(`搜索「${query.trim()}」`);
  return count ? `${parts.length ? parts.join(" · ") : "全部"} · ${count} 件` : "没有未处理的";
}

/** 全部标为已处理 (the view's open items), 全部看过 / 全部清除 (a section), and their 撤销: one request for them all,
 * each with the version it was listed at, so the hub leaves one that took new messages since open (it goes back here
 * too, and the list is pulled again). Shown at once; a failure puts them all back. Filing ends with 撤销 on the toast. */
export async function setAllDone(items: InboxItem[], done: boolean, undoable = true): Promise<void> {
  const seen = new Set<string>();
  const targets = items
    .map((it) => state.items.find((x) => x.id === it.id))
    .filter((it): it is InboxItem => !!it && isDone(it) !== done && !seen.has(it.id) && !!seen.add(it.id));
  if (!targets.length) return;
  const before = new Map(targets.map((it) => [it.id, it]));
  const now = new Date().toISOString();
  set({ items: state.items.map((it) => (before.has(it.id) ? { ...it, done_at: done ? now : null, done_by: done ? "user" : null } : it)) });
  try {
    const r = await api<BulkResult>("/inbox/done", {
      method: "POST",
      json: { done, items: targets.map((it) => ({ id: it.id, version: it.version })) },
    });
    const fresh = new Map((r.changed ?? []).map((it) => [it.id, it]));
    const left = new Set((r.skipped ?? []).map((s) => s.id));
    set({ items: state.items.map((it) => fresh.get(it.id) ?? (left.has(it.id) ? before.get(it.id) ?? it : it)) });
    if ((r.skipped ?? []).some((s) => s.reason === "changed")) void reloadInbox();
    const moved = r.changed ?? [];
    toast(bulkMessage(done, r), "ok", undoable && moved.length ? { label: "撤销", run: () => void setAllDone(moved, !done, false) } : undefined);
  } catch (e) {
    set({ items: state.items.map((it) => before.get(it.id) ?? it) });
    toast(`${done ? "没标成已处理" : "没移回"}：${errorText(e)}`, "error");
  }
}

/** 发送: the hub replies in the chat / by mail / in the document (moving the rows' status in `changes` first) and
 * files the item as done. Filed at once; a failure puts the item back and keeps the text for the draft dialog. */
export async function sendReply(item: InboxItem, text: string, changes: DocChange[] = []): Promise<boolean> {
  if (state.sending.includes(item.id)) return false;
  const before = state.items.find((x) => x.id === item.id);
  set({ sending: [...state.sending, item.id] });
  patchItem(item.id, (it) => ({ ...it, done_at: new Date().toISOString(), done_by: "replied" }));
  unsentReplies.delete(item.id);
  try {
    const r = await api<InboxItem>(`${itemPath(item.id)}/reply`, { method: "POST", json: { text, changes: changes.length ? changes : undefined } });
    if (r?.id === item.id) replaceItem(r);
    toast("已发送，标为已处理", "ok");
    return true;
  } catch (e) {
    if (before) patchItem(item.id, () => before);
    if (text) unsentReplies.set(item.id, text);
    toast(`回复没发出去：${errorText(e)}`, "error");
    return false;
  } finally {
    set({ sending: state.sending.filter((x) => x !== item.id) });
  }
}

export function movingKey(item: InboxItem, row: DocRow, status: DocStatus) {
  return `${item.id}|${row.ref}|${status.field}`;
}

/** Moves a row's status in its document: shown at once, written by the hub, the item replaced by the hub's fresh
 * read; a failure puts the old value back. */
export async function setDocStatus(item: InboxItem, row: DocRow, status: DocStatus, value: string): Promise<void> {
  const key = movingKey(item, row, status);
  if (value === status.value || state.moving.includes(key)) return;
  const before = state.items.find((x) => x.id === item.id);
  set({ moving: [...state.moving, key] });
  patchItem(item.id, (it) => ({
    ...it,
    doc: it.doc && {
      ...it.doc,
      rows: it.doc.rows?.map((r) => (r.ref !== row.ref ? r : { ...r, status: r.status?.map((s) => (s.field === status.field ? { ...s, value } : s)) })),
    },
  }));
  try {
    const fresh = await api<InboxItem>(`${itemPath(item.id)}/doc/status`, { method: "POST", json: { ref: row.ref, field: status.field, value } });
    if (fresh?.id === item.id) replaceItem(fresh);
    toast(`「${status.name}」改成了${value}`, "ok");
  } catch (e) {
    if (before) patchItem(item.id, () => before);
    toast(`没改成：${errorText(e)}`, "error");
  } finally {
    set({ moving: state.moving.filter((x) => x !== key) });
  }
}

// Selectors may build new arrays: a result shallow-equal to the last one is the last one (see store.ts).
function same(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((x, i) => Object.is(x, b[i]));
  return false;
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
};

export function useInbox<T>(select: (s: InboxState) => T): T {
  useEffect(start, []);
  const last = useRef<{ v: T } | null>(null);
  return useSyncExternalStore(subscribe, () => {
    const v = select(state);
    if (last.current && same(last.current.v, v)) return last.current.v;
    last.current = { v };
    return v;
  });
}

/** How many unhandled「需要你」items the inbox has (the sidebar's badge). */
export function useInboxBadge(): number {
  return useInbox((s) => badgeCount(s.items));
}

/** Tests only: back to an empty, unstarted list. */
export function resetInboxForTests() {
  state = { items: [], loaded: false, loading: false, error: "", sending: [], moving: [] };
  started = false;
  inFlight = null;
  again = false;
  unsentReplies.clear();
}

/** Sends a message to a Feishu conversation via the native hub API. */
export async function sendLarkMessage(feed: string, text: string, clientID?: string): Promise<{ message_id?: string }> {
  return api<{ message_id?: string }>(`/lark/conversations/${enc(feed)}/messages`, {
    method: "POST",
    json: { text, client_id: clientID },
    token: getToken(),
  });
}

/** Replies to a Feishu message (either quoted in chat or in its topic thread). */
export async function replyLarkMessage(
  messageID: string,
  text: string,
  thread = false,
  clientID?: string,
): Promise<{ message_id?: string }> {
  return api<{ message_id?: string }>(`/lark/messages/${enc(messageID)}/replies`, {
    method: "POST",
    json: { text, thread, client_id: clientID },
    token: getToken(),
  });
}

/** Computes the next position string for an optimistic LarkMessage in the list. */
export function nextLarkPosition(messages: LarkMessage[]): string {
  let max = 0;
  for (const m of messages) {
    const n = Number(m.position);
    if (Number.isFinite(n) && n > max) max = n;
  }
  return String(max > 0 ? max + 1 : Date.now());
}
