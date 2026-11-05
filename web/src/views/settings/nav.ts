// Where 设置 is (#1996): which category the URL opens, and the list of them a phone shows first. Wide, the
// categories are tabs in the bar and /settings is 通用; phone width, /settings is the list (as on iOS), a category is
// /settings?tab=… pushed over it, and Back goes back to the list.
import { t } from "../../i18n";
import { settingsTab } from "../../lib/channels";
import type { IconName } from "../../ui/icons";

export type Tab = "general" | "notifications" | "assistant" | "models" | "model-config" | "claude" | "codex" | "channels" | "connectors" | "extensions" | "translate" | "shortcuts" | "devices" | "hub";
export const TABS: Tab[] = ["general", "notifications", "assistant", "models", "model-config", "claude", "codex", "channels", "connectors", "extensions", "translate", "shortcuts", "devices", "hub"];
// The tab whose page opens one of its items, and the query parameter that names it.
const SUB: Partial<Record<Tab, string>> = { "model-config": "purpose", channels: "channel", extensions: "extension", connectors: "connector" };

export const TAB_INFO: Record<Tab, { label: string; icon: IconName }> = {
  general: { label: t("settings-nav.tab_general"), icon: "gear" },
  notifications: { label: t("settings-nav.tab_notifications"), icon: "bell" },
  assistant: { label: t("settings-nav.tab_assistant"), icon: "spark" },
  models: { label: t("settings-nav.tab_models"), icon: "layers" },
  "model-config": { label: t("settings-nav.tab_model_config"), icon: "gear" },
  claude: { label: "Claude Code", icon: "terminal" },
  codex: { label: "Codex", icon: "terminal" },
  channels: { label: t("settings-nav.tab_channels"), icon: "chat" },
  connectors: { label: t("settings-nav.tab_connectors"), icon: "link" },
  extensions: { label: t("settings-nav.tab_extensions"), icon: "extension" },
  translate: { label: t("settings-nav.tab_translate"), icon: "translate" },
  shortcuts: { label: t("settings-nav.tab_shortcuts"), icon: "keyboard" },
  devices: { label: t("settings-nav.tab_devices"), icon: "key" },
  hub: { label: "hub", icon: "hub" },
};

/** The phone's list: groups of categories, in the tab bar's order. */
export const LIST_GROUPS: { title: string; tabs: Tab[] }[] = [
  { title: t("settings-nav.group_general"), tabs: ["general", "notifications", "assistant", "translate", "shortcuts"] },
  { title: t("settings-nav.group_models"), tabs: ["models", "model-config", "claude", "codex"] },
  { title: t("settings-nav.group_connections"), tabs: ["channels", "connectors", "extensions", "devices", "hub"] },
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
