// 设置 › 插件 › 渠道 (docs/channels.md): every way of talking to Persona — the apps, the Feishu bot, WeChat, /agent in
// Feishu, voice, and the chat app bots plugins contribute — as GET /api/v1/channels lists them; one channel's page
// (its switch, binding a step at a time, where its messages go, what it does, a test message), and the source mark on
// a message that came from one (its client_msg_id is `<channel>-<message id>`).
//
// The pure parts (state words, sections, the source of a message, the bind steps) are plain functions for the tests.

import { useEffect, useState } from "react";
import { api, enc } from "./api";
import type { ConnectorPrompt } from "./connectors";
import { useStore } from "./store";

// ── wire types (server/internal/channel) ─────────────────

export type ChannelState = "connected" | "not_connected" | "error" | "disabled" | "not_installed";

export interface ChannelCapabilities {
  images_in?: boolean;
  files_in?: boolean;
  voice_in?: boolean;
  format?: "markdown" | "plain";
  max_text?: number;
  asks?: "none" | "text" | "buttons";
  push?: boolean;
  typing?: boolean;
  progress?: boolean;
  threads?: boolean;
}

export interface ChannelTarget {
  kind: "main" | "new" | "conversation" | "agent";
  machine_id?: string;
  session_id?: string;
  title?: string;
}

export interface ChannelFeature {
  id: string;
  title: string;
  description?: string;
  on: boolean;
}

/** One of GET /api/v1/channels (channel.View). */
export interface Channel {
  id: string;
  name: string;
  kind: "app" | "im" | "mention" | "voice";
  source: "builtin" | "plugin" | "market";
  plugin?: string;
  market?: string;
  installed: boolean;
  enabled: boolean;
  state: ChannelState;
  error?: string;
  account?: { id?: string; name: string };
  description?: string;
  capabilities: ChannelCapabilities;
  target?: ChannelTarget;
  features?: ChannelFeature[];
  bind: "none" | "qr" | "code" | "fields" | "config";
  surfaces?: string[];
  last_event_at?: string;
  read_only?: boolean;
}

/** GET /api/v1/channels/{id} (channel.Detail). */
export interface ChannelDetail extends Channel {
  waiting: { answers: number; asks: number };
  conversations?: { machine_id: string; session_id: string; title?: string; at: string }[];
  /** The channel's own full page in the app ("settings/connectors/feishu-bot", "settings/plugins/<id>"). */
  settings_url?: string;
}

/** A bind step (connector.Prompt) with the two kinds a channel adds: a QR code to scan, a code to send to the bot. */
export interface ChannelPrompt extends ConnectorPrompt {
  qr?: { size: number; rows: string[] };
}

// ── the hub's interface ──────────────────────────────────

export async function listChannels(): Promise<Channel[]> {
  const r = await api<{ channels?: Channel[] }>("/channels");
  return r.channels ?? [];
}

export function channelDetail(id: string): Promise<ChannelDetail> {
  return api<ChannelDetail>(`/channels/${enc(id)}`);
}

export function updateChannel(id: string, body: { enabled?: boolean; target?: ChannelTarget; features?: Record<string, boolean> }): Promise<ChannelDetail> {
  return api<ChannelDetail>(`/channels/${enc(id)}`, { method: "PUT", json: body });
}

export function bindChannel(id: string, step: string, values: Record<string, string>): Promise<ChannelPrompt> {
  return api<ChannelPrompt>(`/channels/${enc(id)}/bind`, { method: "POST", json: { step, values } });
}

export function unbindChannel(id: string): Promise<ChannelDetail> {
  return api<ChannelDetail>(`/channels/${enc(id)}/binding`, { method: "DELETE" });
}

export function testChannel(id: string): Promise<unknown> {
  return api(`/channels/${enc(id)}/test`, { method: "POST" });
}

// ── words ────────────────────────────────────────────────

export type ChannelTone = "green" | "red" | "idle";

export function channelStateText(c: Channel): string {
  switch (c.state) {
    case "connected":
      return "已连接";
    case "error":
      return "出错了";
    case "disabled":
      return "已停用";
    case "not_installed":
      return "没安装";
  }
  return "未连接";
}

export function channelTone(c: Channel): ChannelTone {
  if (c.state === "connected") return "green";
  if (c.state === "error") return "red";
  return "idle";
}

/** The row's second line: the error, the account, else what it is. */
export function channelSubtitle(c: Channel): string {
  if (c.state === "error" && c.error) return c.error;
  if (c.kind === "app" && c.surfaces?.length) return c.surfaces.join(" · ");
  if (c.account?.name && c.state !== "not_installed") return c.account.name;
  return c.description ?? "";
}

/** The list's sections: the apps, the installed ones, those a market has. Empty sections are left out. */
export function channelSections(list: Channel[]): { title: string; footer?: string; channels: Channel[] }[] {
  const apps = list.filter((c) => c.kind === "app");
  const installed = list.filter((c) => c.kind !== "app" && c.source !== "market");
  const market = list.filter((c) => c.source === "market");
  return [
    { title: "Persona", footer: "在 Persona 的哪一端说都进同一段对话，提问和批准在卡片上原地作答。", channels: apps },
    { title: "已接入", footer: "这些地方发给机器人的消息会交给 Persona，回答和 Agent 的提问发回原处。只认绑定的那一个人。", channels: installed },
    { title: "可以安装", footer: "插件市场里提供渠道的插件：装好后在这里绑定。", channels: market },
  ].filter((s) => s.channels.length > 0);
}

/** 「3 个已连接」, with the ones that went wrong. */
export function channelsSummary(list: Channel[]): string {
  const on = list.filter((c) => c.kind !== "app" && c.state === "connected").length;
  const bad = list.filter((c) => c.state === "error").length;
  return bad > 0 ? `${on} 个已连接 · ${bad} 个出错` : `${on} 个已连接`;
}

export function targetText(t?: ChannelTarget): string {
  switch (t?.kind) {
    case "new":
      return "新对话（下一条消息开）";
    case "conversation":
      return t.title || "一段对话";
    case "agent":
      return t.title ? `Agent · ${t.title}` : "一个 Agent";
  }
  return "主对话";
}

/** What the channel can do, in a few words for its page. */
export function capabilityText(c: ChannelCapabilities): string[] {
  const out: string[] = [];
  const takes = ["文字", c.images_in && "图片", c.files_in && "文件", c.voice_in && "语音"].filter(Boolean);
  out.push(`收${takes.join("、")}`);
  out.push(c.format === "plain" ? "回答是纯文字" : "回答带格式（Markdown）");
  if (c.max_text) out.push(`一条最多 ${c.max_text} 字，长的拆开发`);
  if (c.asks === "text") out.push("提问和批准回复序号或文字");
  else if (c.asks === "buttons") out.push("提问和批准点按钮");
  if (c.typing) out.push("处理时显示「正在输入」");
  if (c.progress) out.push("处理时显示进度");
  return out;
}

export function bindTitle(c: Channel): string {
  switch (c.bind) {
    case "qr":
      return "扫码绑定";
    case "code":
      return "配对";
  }
  return "绑定";
}

export function isBound(c: Channel): boolean {
  return c.state === "connected" || (c.state === "error" && !!c.account);
}

/** Whether the page can bind it: it binds here, is on, and is not bound. */
export function canBind(c: Channel): boolean {
  return !c.read_only && c.source !== "market" && c.bind !== "none" && c.bind !== "config" && c.enabled && !isBound(c);
}

/** A step the app sends again by itself (a QR code to scan, a code to enter or send), not one with fields. */
export function isPolled(p?: ChannelPrompt | null): boolean {
  return !!p && !p.done && (p.kind === "qr" || p.kind === "pair_code" || p.kind === "device_code");
}

/** Where a channel's settings_url ("settings/connectors/feishu-bot") opens in the settings page: its tab and sub-page. */
export function settingsTarget(url: string): { tab: string; sub: string | null } | null {
  const m = /^settings\/([a-z-]+)(?:\/(.+))?$/.exec(url);
  return m ? { tab: m[1], sub: m[2] ?? null } : null;
}

// ── where a message came from ────────────────────────────

/** The channel a message came from, by its client_msg_id's prefix (`wechat-…`), when it is one there is. */
export function channelOf(clientMsgId: string | undefined, list: Pick<Channel, "id" | "name" | "kind">[]): Pick<Channel, "id" | "name" | "kind"> | undefined {
  if (!clientMsgId) return undefined;
  const dash = clientMsgId.indexOf("-");
  if (dash <= 0) return undefined;
  const head = clientMsgId.slice(0, dash);
  return list.find((c) => c.id === head && c.kind !== "app");
}

/** The name to show for a conversation's latest channel (Session.channels), "" when it has none known. */
export function conversationChannel(ids: string[] | undefined, list: Pick<Channel, "id" | "name">[]): Pick<Channel, "id" | "name"> | undefined {
  const id = ids?.[0];
  return id ? list.find((c) => c.id === id) ?? { id, name: id } : undefined;
}

// The list, fetched once per hub (it changes when a plugin comes or goes: the settings page reloads it), for the
// source marks in conversations.
let cached: { key: string; list: Channel[] } | null = null;
let inflight: Promise<Channel[]> | null = null;

/** The channels as the conversations need them (names for the source marks); [] on a hub without them. */
export function useChannels(): Channel[] {
  const has = useStore((s) => s.features.includes("channels"));
  const key = useStore((s) => s.features.join(","));
  const [list, setList] = useState<Channel[]>(() => (cached && cached.key === key ? cached.list : []));
  useEffect(() => {
    if (!has) return;
    if (cached?.key === key) {
      setList(cached.list);
      return;
    }
    let live = true;
    inflight ??= listChannels().finally(() => (inflight = null));
    inflight.then((l) => {
      cached = { key, list: l };
      if (live) setList(l);
    }, () => {});
    return () => void (live = false);
  }, [has, key]);
  return has ? list : [];
}

/** Forget the list (the settings page changed something). */
export function forgetChannels() {
  cached = null;
}
