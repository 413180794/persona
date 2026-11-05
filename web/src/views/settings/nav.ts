// Where 设置 is (#1996): which category the URL opens, and the list of them a phone shows first. Wide, the
// categories are tabs in the bar and /settings is 通用; phone width, /settings is the list (as on iOS), a category is
// /settings?tab=… pushed over it, and Back goes back to the list.
import { settingsTab } from "../../lib/channels";
import type { IconName } from "../../ui/icons";

export type Tab = "general" | "assistant" | "models" | "model-config" | "claude" | "codex" | "channels" | "connectors" | "extensions" | "translate" | "devices" | "hub";
export const TABS: Tab[] = ["general", "assistant", "models", "model-config", "claude", "codex", "channels", "connectors", "extensions", "translate", "devices", "hub"];
// The tab whose page opens one of its items, and the query parameter that names it.
const SUB: Partial<Record<Tab, string>> = { "model-config": "purpose", channels: "channel", extensions: "extension", connectors: "connector" };

export const TAB_INFO: Record<Tab, { label: string; icon: IconName }> = {
  general: { label: "通用", icon: "gear" },
  assistant: { label: "助手", icon: "spark" },
  models: { label: "模型供应商", icon: "layers" },
  "model-config": { label: "模型配置", icon: "gear" },
  claude: { label: "Claude Code", icon: "terminal" },
  codex: { label: "Codex", icon: "terminal" },
  channels: { label: "渠道", icon: "chat" },
  connectors: { label: "连接器", icon: "link" },
  extensions: { label: "扩展", icon: "extension" },
  translate: { label: "翻译", icon: "translate" },
  devices: { label: "设备", icon: "key" },
  hub: { label: "hub", icon: "hub" },
};

/** The phone's list: groups of categories, in the tab bar's order. */
export const LIST_GROUPS: { title: string; tabs: Tab[] }[] = [
  { title: "通用", tabs: ["general", "assistant", "translate"] },
  { title: "模型和 Agent", tabs: ["models", "model-config", "claude", "codex"] },
  { title: "连接", tabs: ["channels", "connectors", "extensions", "devices", "hub"] },
];

/** The category and item a URL's query names; `tab` is null when it names none (the phone's list). */
export function settingsAt(search: string): { tab: Tab | null; sub: string | null } {
  const q = new URLSearchParams(search);
  const t = settingsTab(q.get("tab")) as Tab;
  if (!TABS.includes(t)) return { tab: null, sub: null };
  const param = SUB[t];
  // ?tab=plugins&plugin=<id>: a link from before #1845
  const sub = param ? (q.get(param) ?? (t === "extensions" ? q.get("plugin") : null)) : null;
  if (t === "extensions" && sub === "-channels") return { tab: "channels", sub: null };
  if (t === "extensions" && sub?.startsWith("-channels/")) return { tab: "channels", sub: sub.slice("-channels/".length) };
  return { tab: t, sub };
}

/** The URL of a category (and its open item). 通用 is plain /settings on a wide screen, where that opens it; on a
 *  phone /settings is the list, so it is named. */
export function settingsHref(tab: Tab, id: string | null, phone: boolean): string {
  if (tab === "general" && !phone) return "/settings";
  const param = SUB[tab];
  return `/settings?tab=${tab}${id && param ? `&${param}=${encodeURIComponent(id)}` : ""}`;
}

/** The history state of a category opened from the phone's list: Back from it returns to the list. */
export const FROM_LIST = "personaSettingsList";

export function openedFromList(state: unknown): boolean {
  return Boolean(state && typeof state === "object" && (state as Record<string, unknown>)[FROM_LIST]);
}
