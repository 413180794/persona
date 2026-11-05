// The unified inbox (docs/inbox.md): the hub's items (Feishu chats, mail threads, Feishu documents, 公司圈 posts, and
// the connectors' threads: GitHub, Telegram…, drawn from their generic `connector` block),
// kept in one module-level list so the sidebar's badge and the page share it. Pulled on first use, again on `inbox`
// pushes and on (re)connect. Marking done applies at once and is sent in the background; a failure puts it back.
//
// The pure parts (filters, sections, labels, the 交给 AI message, Feishu message parts) are plain functions so the
// tests can run them without a DOM.

import { formatDate, t } from "../i18n";
import { useEffect, useRef, useSyncExternalStore } from "react";
import { api, ApiError, enc, errorText, getToken } from "./api";
import { BASE } from "./base";
import type { AgentMutes } from "./agent-groups";
import { connectorOpenTitle } from "./connectors";
import { larkPath } from "./lark-accounts";
import { baseName } from "./format";
import type { Route } from "./router";
import { isResumed } from "./reconnect";
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
  lark?: {
    feed_id: string; is_group: boolean; is_topic?: boolean; start_position?: string; mention_position?: string; at_all?: boolean;
    /** The lark-connector account the chat is in; `ol_account` only when that is not the hub's own (docs/feishu.md「多账号」). */
    account_id?: number; ol_account?: number;
  };
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
  /** What it grew out of (docs/inbox.md「线索」): another item, a todo or an agent session ("<machine>/<session>"). */
  parent?: InboxParent;
  /** What it can be muted by (docs/inbox.md「静音」): its repository, chat or sender in its source. */
  mutes?: InboxMuteOption[];
}

/** One thing an item can be muted by: a repository, a chat or a sender of its source. */
export interface InboxMuteOption {
  tag: string; // repo | chat | sender
  value: string;
  name: string;
}

/** A mute the user set (GET /api/v1/inbox/mutes): nothing of `value` under `tag` in `source` comes into the inbox. */
export interface InboxMute extends InboxMuteOption {
  id: string;
  source: string;
  source_name?: string;
  created_at?: string;
}

/** The menu's words for a mute option (iOS MuteOption.title). */
export function muteTitle(o: Pick<InboxMuteOption, "tag">): string {
  return t(o.tag === "repo" ? "lib-inbox.mute_repo" : o.tag === "chat" ? "lib-inbox.mute_chat" : "lib-inbox.mute_sender");
}

/** The name under the menu's words, on one line: a repository without its owner (example-repo). */
export function muteShortName(o: InboxMuteOption): string {
  return o.tag === "repo" ? o.name.split("/").pop() || o.name : o.name;
}

/** What kind of thing a mute is, for people: 仓库 / 会话 / 发件人. */
export function muteTagName(tag: string): string {
  return t(tag === "repo" ? "lib-inbox.mute_tag_repo" : tag === "chat" ? "lib-inbox.mute_tag_chat" : "lib-inbox.mute_tag_sender");
}

/** The 已静音 list's second line: the source's name, then 仓库 / 会话 / 发件人. */
export function muteLine(m: InboxMute): string {
  return [m.source_name || m.source, muteTagName(m.tag)].join(" · ");
}

/** The mutes, newest first (iOS InboxMutesView). */
export function sortMutes(list: InboxMute[]): InboxMute[] {
  // By time, not by string: RFC3339Nano drops trailing zeros, so "…:05Z" and "…:05.1Z" compare the wrong way.
  const ts = (m: InboxMute) => (m.created_at ? Date.parse(m.created_at) || 0 : 0);
  return [...list].sort((a, b) => ts(b) - ts(a));
}

export type InboxParentKind = "inbox" | "todo" | "session";
export interface InboxParent { kind: InboxParentKind; id: string; by?: "continues" | "connector" | "user" }

/** GET /api/v1/inbox/tree/{kind}/{id}: the node's ancestors (nearest first; the last may be a todo or a session,
 * with no item) and the items right under it. */
export interface InboxTree {
  node: InboxParent;
  item?: InboxItem;
  ancestors: (InboxParent & { item?: InboxItem })[];
  children: InboxItem[];
}

export function fetchInboxTree(kind: InboxParentKind, id: string): Promise<InboxTree> {
  const path = kind === "session" ? id.split("/").map(enc).join("/") : enc(id).replace(/%3A/gi, ":");
  return api<InboxTree>(`/inbox/tree/${kind}/${path}`);
}

/** Where a node of a line opens. */
export function parentRoute(p: InboxParent): Route | undefined {
  if (p.kind === "inbox") return { name: "inbox", id: p.id };
  if (p.kind === "todo") return { name: "todos", id: p.id };
  const i = p.id.indexOf("/");
  return i > 0 && i < p.id.length - 1 ? { name: "session", mid: p.id.slice(0, i), sid: p.id.slice(i + 1) } : undefined;
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
  { key: "action", title: t("lib-inbox.tier_action") },
  { key: "fyi", title: t("lib-inbox.tier_fyi") },
  { key: "noise", title: t("lib-inbox.tier_noise") },
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
  if (url?.startsWith("sms:")) return t("lib-inbox.reply_in_messages");
  return connectorOpenTitle(it.source) ?? (url ? t("lib-inbox.open_in", { name: it.account_name || it.source }) : t("lib-inbox.open"));
}

/** A connector item's `open_url` as the browser opens it; an sms: one carries the AI 分身's draft as its body. */
export function openURL(it: InboxItem): string | undefined {
  const raw = it.connector?.open_url;
  if (!raw) return undefined;
  const draft = agentDraft(it);
  return raw.startsWith("sms:") && draft ? `${raw}&body=${encodeURIComponent(draft)}` : raw;
}

const AT_ME = t("lib-inbox.kind_at_me");

/** The short tag next to the title. */
export function kindLabel(it: InboxItem): string | undefined {
  if (it.connector) return it.connector.label || undefined;
  switch (it.kind) {
    case "dm":
      return t("lib-inbox.kind_dm");
    case "mention":
      return it.lark?.at_all ? t("lib-inbox.kind_at_all") : AT_ME;
    case "bot":
      return t("lib-inbox.kind_bot");
    case "doc":
      return it.doc?.notice === "comment" || it.doc?.notice === "reply" ? t("lib-inbox.kind_comment") : AT_ME;
    case "moment":
      return it.moment?.notice?.startsWith("atIn") ? AT_ME : t("lib-inbox.moments");
    default:
      return undefined;
  }
}

/** @我 is red (it is about you); every other tag is neutral. */
export function labelIsUrgent(it: InboxItem): boolean {
  return it.kind === "mention" || kindLabel(it) === AT_ME;
}

export function docTypeName(type?: string): string {
  switch (type) {
    case "sheet":
      return t("lib-inbox.doc_sheet");
    case "base":
      return t("lib-inbox.doc_base");
    case "docx":
    case "doc":
      return t("lib-inbox.doc_doc");
    default:
      return t("lib-inbox.doc_cloud");
  }
}

/** Who wrote what: the row's line under the summary (or its second line before triage); `preview` is the hub's
 * translation of a foreign mail's when it is shown. */
export function fromLine(it: InboxItem, preview?: string): string {
  if (it.doc) return `${docTypeName(it.doc.type)} · ${it.preview}`; // the preview names who: 李雷提及了你
  if (it.moment) return t("lib-inbox.sender_line", { sender: it.sender ?? "", text: it.preview });
  if (it.connector) {
    // example-org/example-repo#12 · bob：请看一下
    const who = [it.sender ?? "", it.preview].filter(Boolean).join(t("lib-inbox.sender_separator"));
    return [connectorReference(it) ?? "", who].filter(Boolean).join(" · ");
  }
  if (preview && it.sender) return t("lib-inbox.sender_line", { sender: it.sender, text: preview });
  // a Feishu direct chat's title already is the sender
  if (!it.sender || (isLark(it) && it.lark && !it.lark.is_group)) return it.preview;
  return t("lib-inbox.sender_line", { sender: it.sender, text: it.preview });
}

/** Where it came from, for the detail page's subtitle. */
export function sourceLine(it: InboxItem): string {
  if (it.doc) return [docTypeName(it.doc.type), it.preview].filter(Boolean).join(" · ");
  if (it.moment) return it.moment.my_post ? t("lib-inbox.moments_my_post") : t("lib-inbox.moments");
  if (it.connector) return [it.account_name || it.source, connectorReference(it)].filter(Boolean).join(" · ");
  // 飞书 · <name> while the hub reads more than one Feishu account
  // i18n-ignore: the hub names its Lark accounts 「飞书 · <name>」
  if (isLark(it)) return [it.account_name?.startsWith("飞书 · ") ? it.account_name : t("lib-inbox.lark"), it.lark?.is_group ? t("lib-inbox.kind_group") : it.kind === "bot" ? t("lib-inbox.kind_bot") : t("lib-inbox.kind_dm")].join(" · ");
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
  return t("lib-inbox.doc_change", { field: st.name, from: st.value || t("lib-inbox.not_set"), to: change.value, row: row.title });
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

/** Media under /api/ for an <img>: the token goes in the query (an <img> can't send a header), and the path under the
 * sub-path the page is served at (lib/base.ts). */
export function withToken(url: string | undefined, token = getToken(), base = BASE): string | undefined {
  if (!url) return undefined;
  if (!url.startsWith("/api/")) return url;
  return `${base}${url}${url.includes("?") ? "&" : "?"}token=${encodeURIComponent(token)}`;
}

/** 18:20 today, 昨天, 周三 within the week, 10月28日 this year, else 2025-10-28 (LarkTime.short). */
export function shortTime(iso?: string, now = new Date()): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return "";
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((day(now) - day(d)) / 86400_000);
  if (days <= 0) return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  if (days === 1) return t("lib-inbox.yesterday");
  if (now.getTime() - d.getTime() < 6 * 86400_000) return formatDate(d, { weekday: "short" });
  if (d.getFullYear() === now.getFullYear()) return formatDate(d, { month: "short", day: "numeric" });
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
  { key: "mention", label: AT_ME },
  { key: "dm", label: t("lib-inbox.kind_dm") },
  { key: "group", label: t("lib-inbox.kind_group") },
  { key: "mail", label: t("lib-inbox.kind_mail") },
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
    const ok = it.kind === f.kind || (f.kind === "group" && it.kind === "bot") || (f.kind === "mention" && (it.kind === "doc" || (it.kind === "moment" && kindLabel(it) === AT_ME)));
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
  if (ix.tool_name === "ask_user") return t("lib-inbox.ix_persona_asks");
  const name = ix.agent === "codex" ? "Codex" : ix.agent === "traex" ? "TRAEX" : ix.agent && ix.agent !== "claude" ? ix.agent : "";
  if (name && ix.kind === "question") return t("lib-inbox.ix_named_asks", { name });
  if (name && ix.kind === "permission") return t("lib-inbox.ix_named_permission", { name });
  if (ix.kind === "elicitation") return ix.elicitation?.mode === "url" ? t("lib-inbox.ix_elicitation_url") : t("lib-inbox.ix_elicitation_form");
  return { question: t("lib-inbox.ix_claude_asks"), plan: t("lib-inbox.ix_plan"), permission: t("lib-inbox.ix_permission"), screen: t("lib-inbox.ix_screen") }[ix.kind];
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
    case "elicitation":
      return ix.elicitation?.message?.trim() || ix.summary || "";
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

export const HANDOFF_PRESETS = [t("lib-inbox.handoff_summarize"), t("lib-inbox.handoff_research"), t("lib-inbox.handoff_handle")];

function escAttr(s: string) {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/** `<jarvis-context kind="…" title="…" url="…">` the way the app writes it (ContextRef.xml). */
export function contextXML(kind: string, title: string, url: string, text: string): string {
  return `<jarvis-context kind="${kind}" title="${escAttr(title)}" url="${escAttr(url)}">\n${text.replace(/<\/jarvis-context>/g, "")}\n</jarvis-context>`;
}

/** The item as context the chat shows as a chip, then what to do with it (InboxHandOffSheet.message). */
export function handOffMessage(it: InboxItem, ask: string): string {
  const title = `${it.connector ? it.account_name || it.source : isLark(it) ? t("lib-inbox.lark") : t("lib-inbox.kind_mail")} · ${it.title}`;
  let text = it.context?.trim() ? it.context : [it.sender ?? "", it.preview].filter(Boolean).join(t("lib-inbox.sender_separator"));
  if (it.lark) {
    const ol = it.lark.ol_account;
    const account = ol ? t("lib-inbox.handoff_lark_account", { account: ol }) : "";
    text += `\n\n${t("lib-inbox.handoff_lark", { feed: it.lark.feed_id, account })}`;
  }
  if (it.connector?.url) text += `\n\n${t("lib-inbox.handoff_connector", { name: it.account_name || it.source, url: it.connector.url })}`;
  const uid = it.mail?.uids[it.mail.uids.length - 1];
  if (it.mail && uid !== undefined) text += `\n\n${t("lib-inbox.handoff_mail", { account: it.mail.account_id, mailbox: it.mail.mailbox, uid })}`;
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
  /** lark-connector kept a recalled message's content (lark-display.ts): shown only with 防撤回 on, still marked 已撤回. */
  retained?: boolean;
  edited?: boolean;
  /** An edited message's earlier versions, oldest first (lark-display.ts, shown with 编辑历史 on). */
  edit_history?: { edited_at: string; text: string; parts?: LarkPart[]; olk?: unknown }[];
  reply_count?: number;
  thread_reply_count?: number;
  at_me?: boolean;
  /** Read state (others' messages) and Feishu's unread badge at this message: what the read report carries. */
  is_read?: boolean;
  badge_count?: number;
  parts?: LarkPart[];
  reply_to?: { id?: string; sender_name?: string; text?: string; recalled?: boolean; retained?: boolean };
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
  // the hub's copy of a message this page sent replaces its local one (same client_id), which may still be waiting
  const echoed = new Set(batch.flatMap((m) => (m.client_id && !m.id.startsWith("temp-") ? [m.client_id] : [])));
  const byId = new Map(have.filter((m) => !(m.id.startsWith("temp-") && m.client_id && echoed.has(m.client_id))).map((m) => [m.id, m]));
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

// ── 公司圈 ───────────────────────────────────────────────

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

// ── triage: the new items held back from the list (#1773) ───

/**
 * How triage stands (GET /api/v1/inbox `triage`, server/internal/hub/inbox_admit.go): new items the model could not
 * sort wait outside the list. An older hub sends nothing.
 */
export interface InboxTriage {
  /** ok | unconfigured | paused | error | timeout | behind */
  state: string;
  /** The state for people, the hub's own words; may be empty. */
  reason?: string;
  waiting: number;
  sources?: InboxTriageSource[];
}

export interface InboxTriageSource {
  source: string;
  name?: string;
  state: string;
  reason?: string;
  waiting: number;
}

/** What a state says when the hub gives no reason (the same words on iOS and Android). */
export const TRIAGE_REASONS: Record<string, string> = {
  unconfigured: t("lib-inbox.triage_unconfigured"),
  paused: t("lib-inbox.triage_paused"),
  error: t("lib-inbox.triage_error"),
  timeout: t("lib-inbox.triage_timeout"),
  behind: t("lib-inbox.triage_behind"),
};

/** The model settings 去配置 opens: 信息筛选, the purpose that sorts the inbox. */
export const TRIAGE_SETTINGS = "/settings?tab=model-config&purpose=inbox.triage";

/**
 * The banner over the list while new items wait for triage; null when none wait, triage is ok, or the hub is older.
 * One source waiting names it (「有 3 条飞书消息在等分拣：…」), several do not; the reason is the hub's, else the
 * state's words (an unknown state without a reason says only how many wait). `configure`: 去配置, only for a model
 * not set up.
 */
export function triageBanner(tr: InboxTriage | null | undefined): { text: string; configure: boolean } | null {
  if (!tr || typeof tr !== "object" || !tr.state || tr.state === "ok" || !(Number(tr.waiting) > 0)) return null;
  const sources = (Array.isArray(tr.sources) ? tr.sources : []).filter((x) => x && Number(x.waiting) > 0);
  const name = sources.length === 1 ? (sources[0].name ?? "").trim() : "";
  const reason = (tr.reason ?? "").trim() || TRIAGE_REASONS[tr.state] || "";
  const count = Number(tr.waiting);
  const waiting = name ? t("lib-inbox.triage_waiting_named", { count, name }) : t("lib-inbox.triage_waiting", { count });
  const text = reason ? t("lib-inbox.triage_waiting_reason", { waiting, reason }) : waiting;
  return { text, configure: tr.state === "unconfigured" };
}

// ── the list, shared by the page and the sidebar badge ───

interface InboxState {
  items: InboxItem[];
  /** The hub's triage state from the last list; null from an older hub. */
  triage: InboxTriage | null;
  loaded: boolean;
  loading: boolean;
  error: string;
  /** Items whose reply is being sent (the row shows a spinner, a second tap does nothing). */
  sending: string[];
  /** Status moves being written, by "<item>|<row>|<field>". */
  moving: string[];
}

let state: InboxState = { items: [], triage: null, loaded: false, loading: false, error: "", sending: [], moving: [] };
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
  onHubEvent("hello", (msg) => !isResumed(msg) && void reloadInbox()); // resumed: the missed inbox pushes are replayed
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
        const r = await api<{ items?: InboxItem[]; triage?: InboxTriage }>("/inbox?sources=all");
        set({ items: r.items ?? [], triage: r.triage ?? null, loaded: true, error: "" });
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
    const r = await api<{ items?: InboxItem[]; triage?: InboxTriage }>("/inbox/refresh?sources=all", { method: "POST" });
    set({ items: r.items ?? [], triage: r.triage ?? null, loaded: true, error: "" });
  } catch (e) {
    toast(t("lib-inbox.refresh_failed", { error: errorText(e) }), "error");
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
    toast(t(done ? "lib-inbox.done_failed" : "lib-inbox.undone_failed", { error: errorText(e) }), "error");
  }
}

/** Whether a mute covers an item: the same source, still open, and the same thing among what it can be muted by. */
export function muteCovers(source: string, o: InboxMuteOption, it: InboxItem): boolean {
  return it.source === source && !isDone(it) && (it.mutes ?? []).some((m) => m.tag === o.tag && m.value === o.value);
}

/**
 * 静音这个仓库 / 会话 / 发件人 (iOS InboxStore.mute): the item and the open ones of its source the mute covers go to 已处理
 * at once (「已静音：<名字>」), the hub keeps the mute and files them too; a failure puts them back.
 */
export async function muteItem(item: InboxItem, o: InboxMuteOption): Promise<void> {
  // Only the covered items are put back: a reload during the request (every hub inbox event reloads) keeps its items.
  const before = new Map(state.items.filter((it) => muteCovers(item.source, o, it)).map((it) => [it.id, it]));
  const now = new Date().toISOString();
  set({ items: state.items.map((it) => (before.has(it.id) ? { ...it, done_at: now, done_by: "read", reason: t("lib-inbox.muted_reason", { name: o.name }) } : it)) });
  try {
    await api("/inbox/mutes", { method: "POST", json: { item_id: item.id, tag: o.tag } });
    toast(t("lib-inbox.muted", { name: o.name }), "ok");
  } catch (e) {
    set({ items: state.items.map((it) => before.get(it.id) ?? it) });
    toast(t("lib-inbox.mute_failed", { error: errorText(e) }), "error");
  }
}

/** What the user muted, in every source (GET /api/v1/inbox/mutes), newest first. */
export async function listInboxMutes(): Promise<InboxMute[]> {
  const r = await api<{ mutes?: InboxMute[] }>("/inbox/mutes");
  return sortMutes(r.mutes ?? []);
}

/** 取消静音: only what comes in later comes back. */
export function unmuteInbox(id: string): Promise<unknown> {
  return api(`/inbox/mutes/${enc(id)}`, { method: "DELETE" });
}

/** What a bulk action does (POST /api/v1/inbox/bulk): 全部标为已处理, 全部忽略 (done_by=dismissed), and 移回 (their 撤销). */
export type BulkAction = "done" | "dismiss" | "reopen";

/** What a bulk request did: the items it moved as the hub has them now, the ones already so, and the ones it left
 * (`changed`: new messages since the list; `missing`: gone from the hub). */
export type BulkResult = { changed: InboxItem[]; unchanged: string[]; skipped: { id: string; reason: string }[] };

/** From this many items on, 全部已处理 / 全部忽略 asks first (「将处理 N 件」); fewer go at once, with 撤销 on the toast. */
export const BULK_CONFIRM_AT = 10;

/** The toast after a bulk request. */
export function bulkMessage(action: BulkAction | boolean, r: BulkResult): string {
  const a: BulkAction = action === true ? "done" : action === false ? "reopen" : action;
  const moved = r.changed.length;
  const fresh = r.skipped.filter((s) => s.reason === "changed").length;
  const head = !moved
    ? t(a === "reopen" ? "lib-inbox.bulk_undone_none" : "lib-inbox.bulk_done_none")
    : t(a === "reopen" ? "lib-inbox.bulk_undone" : a === "dismiss" ? "lib-inbox.bulk_dismissed" : "lib-inbox.bulk_done", { count: moved });
  return fresh ? t("lib-inbox.bulk_fresh", { head, count: fresh }) : head;
}

/** What 全部标为已处理 covers, for its label: the filters' names and the search, or 全部. */
export function bulkScope(filters: string[], query: string, count: number): string {
  return count ? t("lib-inbox.scope_count", { scope: bulkScopeName(filters, query), count }) : t("lib-inbox.scope_none");
}

/** The filters' names and the search, or 全部: what a bulk action covers, without the count. */
export function bulkScopeName(filters: string[], query: string): string {
  const parts = filters.filter(Boolean);
  if (query.trim()) parts.push(t("lib-inbox.scope_search", { query: query.trim() }));
  return parts.length ? parts.join(" · ") : t("lib-inbox.scope_all");
}

/** One bulk request. A hub from before /api/v1/inbox/bulk (404 / 405) gets 已处理 and 移回 the old way
 * (POST /api/v1/inbox/done); it has no 忽略. */
async function bulkRequest(action: BulkAction, targets: InboxItem[]): Promise<BulkResult> {
  const items = targets.map((it) => ({ id: it.id, version: it.version }));
  try {
    return await api<BulkResult>("/inbox/bulk", { method: "POST", json: { action, items } });
  } catch (e) {
    if (action === "dismiss" || !(e instanceof ApiError) || (e.status !== 404 && e.status !== 405)) throw e;
    return api<BulkResult>("/inbox/done", { method: "POST", json: { done: action === "done", items } });
  }
}

/** 全部标为已处理 / 全部忽略 (the view's open items), 全部看过 / 全部清除 (a section), and their 撤销: one request for
 * them all, each with the version it was listed at, so the hub leaves one that took new messages since open (it goes
 * back here too, and the list is pulled again). Shown at once; a failure puts them all back. Filing ends with 撤销 on
 * the toast. `action` true / false is done / reopen. */
export async function setAllDone(items: InboxItem[], action: BulkAction | boolean, undoable = true): Promise<void> {
  const a: BulkAction = action === true ? "done" : action === false ? "reopen" : action;
  const done = a !== "reopen";
  const seen = new Set<string>();
  const targets = items
    .map((it) => state.items.find((x) => x.id === it.id))
    .filter((it): it is InboxItem => !!it && isDone(it) !== done && !seen.has(it.id) && !!seen.add(it.id));
  if (!targets.length) return;
  const before = new Map(targets.map((it) => [it.id, it]));
  const now = new Date().toISOString();
  const by = a === "dismiss" ? "dismissed" : "user";
  set({ items: state.items.map((it) => (before.has(it.id) ? { ...it, done_at: done ? now : null, done_by: done ? by : null } : it)) });
  try {
    const r = await bulkRequest(a, targets);
    const fresh = new Map((r.changed ?? []).map((it) => [it.id, it]));
    const left = new Set((r.skipped ?? []).map((s) => s.id));
    set({ items: state.items.map((it) => fresh.get(it.id) ?? (left.has(it.id) ? before.get(it.id) ?? it : it)) });
    if ((r.skipped ?? []).some((s) => s.reason === "changed")) void reloadInbox();
    const moved = r.changed ?? [];
    toast(bulkMessage(a, r), "ok", undoable && done && moved.length ? { label: t("lib-inbox.undo"), run: () => void setAllDone(moved, "reopen", false) } : undefined);
  } catch (e) {
    set({ items: state.items.map((it) => before.get(it.id) ?? it) });
    toast(t(!done ? "lib-inbox.undone_failed" : a === "dismiss" ? "lib-inbox.dismiss_failed" : "lib-inbox.done_failed", { error: errorText(e) }), "error");
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
    toast(t("lib-inbox.sent_done"), "ok");
    return true;
  } catch (e) {
    if (before) patchItem(item.id, () => before);
    if (text) unsentReplies.set(item.id, text);
    toast(t("lib-inbox.reply_failed", { error: errorText(e) }), "error");
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
    toast(t("lib-inbox.status_changed", { name: status.name, value }), "ok");
  } catch (e) {
    if (before) patchItem(item.id, () => before);
    toast(t("lib-inbox.status_failed", { error: errorText(e) }), "error");
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
  const read = () => {
    const v = select(state);
    if (last.current && same(last.current.v, v)) return last.current.v;
    last.current = { v };
    return v;
  };
  // the same read on a server render (a test's renderToStaticMarkup): the list as it is
  return useSyncExternalStore(subscribe, read, read);
}

/** How many unhandled「需要你」items the inbox has (the sidebar's badge). */
export function useInboxBadge(): number {
  return useInbox((s) => badgeCount(s.items));
}

/** Tests only: back to an empty, unstarted list. */
export function resetInboxForTests() {
  state = { items: [], triage: null, loaded: false, loading: false, error: "", sending: [], moving: [] };
  started = false;
  inFlight = null;
  again = false;
  unsentReplies.clear();
}

/** Sends a message to a Feishu conversation via the native hub API, as account `ol` (undefined: the hub's own). */
export async function sendLarkMessage(feed: string, text: string, clientID?: string, ol?: number): Promise<{ message_id?: string }> {
  return api<{ message_id?: string }>(larkPath(`/lark/conversations/${enc(feed)}/messages`, ol), {
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
  ol?: number,
): Promise<{ message_id?: string }> {
  return api<{ message_id?: string }>(larkPath(`/lark/messages/${enc(messageID)}/replies`, ol), {
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
