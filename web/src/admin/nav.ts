// The admin sections, in order. A new section (hub updates #284, settings #287) is one more entry here plus its page.
// advanced: what most people never need (docs/plan-hub-center.md 0: one hub, the node service on every other
// computer); the navigation shows these under「高级」.
import type { AdminIconName } from "./icons";

export type Page = "overview" | "machines" | "peers" | "devices" | "extensions" | "network";

export const NAV: { page: Page; label: string; icon: AdminIconName; advanced?: boolean }[] = [
  { page: "overview", label: "概览", icon: "gauge" },
  { page: "machines", label: "电脑", icon: "machine" },
  { page: "devices", label: "设备", icon: "phone" },
  { page: "extensions", label: "扩展", icon: "puzzle" },
  { page: "network", label: "网络与访问", icon: "shield" },
  { page: "peers", label: "互联", icon: "mesh", advanced: true },
];
