// 设置 › 连接器 › 飞书机器人 (docs/bots.md, the app's FeishuBotModel): GET / PUT /api/v1/bots/feishu, pairing with a
// code sent to the bot from Feishu, and what the bot may do. Pure helpers here; the page is views/settings/FeishuBot.tsx.
import { api } from "./api";

export interface BotFeatures {
  main: boolean;
  cards: boolean;
  all_cards: boolean;
  pushes: boolean;
  sync: boolean;
  agents: boolean;
}

export interface FeishuBotStatus {
  enabled: boolean;
  app_id: string;
  /** The open platform's URL the hub resolved (feishu / lark / a custom one). */
  domain: string;
  has_secret: boolean;
  /** The credentials are hub.json's. */
  from_config: boolean;
  features: BotFeatures;
  connected: boolean;
  error?: string;
  last_event_at?: number;
  bot_name?: string;
  owner?: { name: string; paired_at: number } | null;
  pairing?: { code: string; expires_at: number } | null;
  /** Where the chat's messages go now. */
  target?: string;
  agents?: { title: string; machine_id: string; session_id: string }[] | null;
}

/** The edit sent to PUT: only what is set changes. */
export interface FeishuBotUpdate {
  enabled?: boolean;
  app_id?: string;
  app_secret?: string;
  domain?: string;
  features?: BotFeatures;
}

export const DEFAULT_FEATURES: BotFeatures = { main: true, cards: true, all_cards: false, pushes: false, sync: false, agents: false };

export const botStatus = () => api<FeishuBotStatus>("/bots/feishu");
export const updateBot = (u: FeishuBotUpdate) => api<FeishuBotStatus>("/bots/feishu", { method: "PUT", json: u });
export const pairBot = () => api<FeishuBotStatus>("/bots/feishu/pair", { method: "POST" });
export const unpairBot = () => api<FeishuBotStatus>("/bots/feishu/owner", { method: "DELETE" });
export const testBot = () => api<{ ok: boolean }>("/bots/feishu/test", { method: "POST" });

/** The platform picker's choice for what the hub resolved: feishu, lark, or the custom URL itself. */
export function domainChoice(resolved: string): string {
  if (resolved.includes("larksuite")) return "lark";
  if (!resolved || resolved.includes("open.feishu.cn")) return "feishu";
  return resolved;
}

export interface BotCredentials {
  appID: string;
  appSecret: string;
  domain: string;
}

/** The form filled from what the hub has (the secret is never read back). */
export function credentialsOf(st: FeishuBotStatus): BotCredentials {
  return { appID: st.app_id, appSecret: "", domain: domainChoice(st.domain) };
}

export function credentialsChanged(st: FeishuBotStatus | null, c: BotCredentials): boolean {
  if (!st) return c.appID.trim() !== "";
  return c.appID.trim() !== st.app_id || c.appSecret.trim() !== "" || c.domain !== domainChoice(st.domain);
}

/** The PUT for 验证并保存: an empty secret keeps the saved one. */
export function credentialsUpdate(c: BotCredentials): FeishuBotUpdate {
  const secret = c.appSecret.trim();
  return { app_id: c.appID.trim(), domain: c.domain, ...(secret ? { app_secret: secret } : {}) };
}

/** The switches after turning one: 所有 Agent 的提问 needs 提问和批准. */
export function withFeature(f: BotFeatures, key: keyof BotFeatures, on: boolean): BotFeatures {
  const next = { ...f, [key]: on };
  if (key === "cards" && !on) next.all_cards = false;
  if (key === "all_cards" && on) next.cards = true;
  return next;
}

/** What the status line says. */
export function botStateText(st: FeishuBotStatus | null, loading = false): string {
  if (!st) return loading ? "正在检查…" : "读不到状态";
  if (!st.enabled) return "未打开";
  if (!st.has_secret) return "还没填应用凭证";
  if (!st.connected) return st.error ? `连不上：${st.error}` : "正在连接…";
  return st.owner ? "已连接" : "已连接，等你配对";
}

/** The status dot's tone (none: off or unknown). */
export function botTone(st: FeishuBotStatus | null): "green" | "red" | "orange" | undefined {
  if (!st?.enabled) return undefined;
  if (!st.connected) return "red";
  return st.owner ? "green" : "orange";
}

/** The connectors list's one-word summary. */
export function botSummary(st: FeishuBotStatus | null): string {
  if (!st) return "";
  if (!st.enabled) return "未打开";
  if (!st.connected) return "连不上";
  return st.owner ? "已连接" : "待配对";
}

/** A pairing code still valid at `now` (ms). */
export function livePairing(st: FeishuBotStatus | null, now = Date.now()) {
  const p = st?.pairing;
  return p && p.expires_at * 1000 > now ? p : null;
}

/** 「9 分 3 秒内有效」. */
export function pairingLeft(expiresAt: number, now = Date.now()): string {
  const left = Math.max(0, Math.floor(expiresAt - now / 1000));
  return `${Math.floor(left / 60)} 分 ${left % 60} 秒内有效`;
}
