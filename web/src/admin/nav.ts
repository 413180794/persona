// The admin sections, in order. A new section (hub updates #284, settings #287) is one more entry here plus its page.
// advanced: what most people never need (docs/archive/operations/plan-hub-center.md 0: one hub, the node service on every other
// computer); the navigation shows these under "Advanced".
import { t } from "../i18n";
import type { AdminIconName } from "./icons";

export type Page = "overview" | "machines" | "peers" | "devices" | "extensions" | "assistant" | "usage" | "network";

export const NAV: { page: Page; label: string; icon: AdminIconName; advanced?: boolean }[] = [
  { page: "overview", label: t("admin-nav.overview"), icon: "gauge" },
  { page: "machines", label: t("admin-nav.machines"), icon: "machine" },
  { page: "devices", label: t("admin-nav.devices"), icon: "phone" },
  { page: "extensions", label: t("admin-nav.extensions"), icon: "puzzle" },
  { page: "assistant", label: t("admin-nav.assistant"), icon: "spark" },
  { page: "usage", label: t("admin-nav.usage"), icon: "pulse" },
  { page: "network", label: t("admin-nav.network"), icon: "shield" },
  { page: "peers", label: t("admin-nav.peers"), icon: "mesh", advanced: true },
];
