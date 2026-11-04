// 插件市场 (docs/plugins.md「插件市场」): markets the hub reads (an index persona-plugins.json on a site or a GitHub
// repository), their catalog, and installing, upgrading and removing from it. A full-rights device may: the package is
// pinned by the index's sha256 and nothing installs before the user has seen what it may do (the preview's
// permissions digest). The admin UI has the same routes under /api/v1/admin/plugins/-/ (its api.ts), the same words
// (here).
import { api, enc } from "./api";
import type { HubPluginDetail, PluginPermission } from "./plugins";

/** One market the hub reads (its token, if any, is never sent back). */
export interface Market {
  id: string;
  spec: string;
  name: string;
  description?: string;
  homepage?: string;
  has_token: boolean;
  /** How many plugins its index has. */
  plugins: number;
  warnings?: string[];
  /** Why the last read failed; the plugins it had before stay listed. */
  error?: string;
  fetched_at?: string;
  added_at?: string;
}

/** One plugin of a market, as the hub sees it against what is installed. */
export interface CatalogItem {
  market: string;
  market_name: string;
  id: string;
  name: string;
  version: string;
  description?: string;
  publisher?: string;
  homepage?: string;
  tags?: string[];
  contributes?: Record<string, string[]>;
  permissions?: PluginPermission[];
  /** Why this hub cannot install it (no package for its platform, needs a newer hub). */
  problem?: string;
  /** The installed version, if any. */
  installed?: string;
  installed_source?: string;
  from_this_market?: boolean;
  upgradable?: boolean;
}

export interface Catalog {
  markets: Market[];
  plugins: CatalogItem[];
  platform?: string;
}

/** What installing (or upgrading) would do, before it does: POST …/catalog/preview. */
export interface MarketPreview {
  preview_id: string;
  manifest: { id: string; name: string; version: string; description?: string; publisher?: string; homepage?: string; contributes?: Record<string, unknown[]> };
  permissions: PluginPermission[];
  permissions_digest: string;
  warnings?: string[];
  expires_at: string;
  upgrade?: boolean;
  from?: string;
  more_permissions?: boolean;
  added_permissions?: PluginPermission[];
  market?: string;
}

// ── the hub ─────────────────────────────────────────────

const M = "/plugins/-";

export const listMarkets = () => api<{ markets?: Market[] }>(`${M}/markets`).then((r) => r.markets ?? []);
export const addMarket = (spec: string, token?: string) => api<Market>(`${M}/markets`, { method: "POST", json: token ? { spec, token } : { spec } });
export const removeMarket = (id: string) => api<{ ok: boolean }>(`${M}/markets/${enc(id)}`, { method: "DELETE" });
export const refreshMarket = (id: string) => api<Market>(`${M}/markets/${enc(id)}/refresh`, { method: "POST" });
/** A new token ("" takes it away); the hub reads the index again with it. */
export const setMarketToken = (id: string, token: string) => api<Market>(`${M}/markets/${enc(id)}/token`, { method: "PUT", json: { token } });

export function catalogQuery(q?: string, market?: string, refresh?: boolean): string {
  const p = new URLSearchParams();
  if (q?.trim()) p.set("q", q.trim());
  if (market) p.set("market", market);
  if (refresh) p.set("refresh", "1");
  const s = p.toString();
  return s ? `?${s}` : "";
}

export const getCatalog = (q?: string, market?: string, refresh?: boolean) =>
  api<Catalog>(`${M}/catalog${catalogQuery(q, market, refresh)}`).then((c) => ({ ...c, markets: c.markets ?? [], plugins: c.plugins ?? [] }));
export const previewFromMarket = (market: string, plugin: string) => api<MarketPreview>(`${M}/catalog/preview`, { method: "POST", json: { market, plugin } });
export const installPreview = (pv: MarketPreview) =>
  api<HubPluginDetail>(`${M}/install`, { method: "POST", json: { preview_id: pv.preview_id, permissions_digest: pv.permissions_digest } });
export const upgradePreview = (id: string, pv: MarketPreview) =>
  api<HubPluginDetail>(`/plugins/${enc(id)}/-/upgrade`, { method: "POST", json: { preview_id: pv.preview_id, permissions_digest: pv.permissions_digest } });
/** Only one installed from a market (`origin` market:…); keepData leaves ~/.persona/plugins/data/<id>/. */
export const uninstallPlugin = (id: string, keepData = false) => api<{ ok: boolean }>(`/plugins/${enc(id)}/-/${keepData ? "?keep_data=1" : ""}`, { method: "DELETE" });

// ── words and rules ─────────────────────────────────────

/** The market a plugin was installed from: `market:<market id>/<plugin id>`; "" otherwise. */
export function marketOf(origin?: string): string {
  const m = /^market:([^/]+)\//.exec(origin ?? "");
  return m ? m[1] : "";
}

/**
 * A market read with git (`git:<url>[#branch]`, or a bare `git@host:path.git`): the hub's own SSH credentials, no
 * token. The address itself is never checked here: the hub reads it and says what is wrong (#1373).
 */
export const isGitMarket = (spec: string) => /^(git:|git@|ssh:\/\/)/.test(spec.trim());

/** The icon a market gets: its kind of source. */
export const marketKind = (spec: string): "github" | "git" | "web" => (spec.startsWith("github:") ? "github" : isGitMarket(spec) ? "git" : "web");

/** The words under the add form's address field, and its placeholder: the three ways to write one. */
export const MARKET_SPEC_PLACEHOLDER = "https://…、github:owner/repo 或 git:…";
export const MARKET_SPEC_HINT =
  "三种写法：https 地址（网站上的目录读其中的 persona-plugins.json，或索引文件本身）；github:owner/repo[@分支]（经 GitHub API 读，私有仓库填 token）；git:<地址>[#分支]（git@host:path.git、ssh://…、https://…，前面的 git: 对 git@ 的可以省略，用 hub 机器自己的 SSH 凭据，不收 token）。地址原样交给 hub，读不到时 hub 说原因。";

export type ItemState = { kind: "install" | "upgrade" | "installed" | "problem" | "elsewhere"; label: string; tone: "accent" | "green" | "orange" | "red" | "gray" };

/** Where a catalog plugin stands on this hub, and what the button does. */
export function itemState(it: CatalogItem): ItemState {
  if (it.installed && it.upgradable) {
    if (it.problem) return { kind: "problem", label: `已装 ${it.installed}`, tone: "gray" };
    return { kind: "upgrade", label: `可升级到 ${it.version}`, tone: "accent" };
  }
  if (it.installed) {
    if (it.installed_source && it.installed_source !== "installed") return { kind: "elsewhere", label: it.installed_source === "builtin" ? "hub 自带" : "写在 hub.json 里", tone: "gray" };
    return { kind: "installed", label: `已装 ${it.installed}`, tone: "green" };
  }
  if (it.problem) return { kind: "problem", label: "装不了", tone: "orange" };
  return { kind: "install", label: "未安装", tone: "gray" };
}

/** Uninstalling from here: only a plugin this market installed. */
export const canUninstall = (it: CatalogItem) => Boolean(it.installed && it.from_this_market);

/** A market's line under its name: how many plugins, when it was read, whether it has a token. */
export function marketLine(m: Market, ago: (iso?: string) => string): string {
  return [`${m.plugins} 个插件`, m.fetched_at ? `${ago(m.fetched_at)}读过` : "还没读过", m.has_token ? "有 token" : ""].filter(Boolean).join(" · ");
}

/** A market's address as people read it. */
export function specLabel(spec: string): string {
  if (spec.startsWith("github:")) return spec.slice("github:".length);
  if (spec.startsWith("git:")) return spec.slice("git:".length);
  try {
    const u = new URL(spec);
    return u.host + (u.pathname === "/" ? "" : u.pathname);
  } catch {
    return spec;
  }
}

/** The catalog item's title line: its version, publisher and market. */
export const itemLine = (it: CatalogItem) => [it.version, it.publisher, it.market_name].filter(Boolean).join(" · ");

/** A preview's version as it changes: "1.0.0 → 1.1.0" for an upgrade. */
export const previewVersion = (pv: MarketPreview) => (pv.upgrade && pv.from ? `${pv.from} → ${pv.manifest.version}` : pv.manifest.version);

/** The button that agrees: install, upgrade, or upgrade with more permissions. */
export const agreeLabel = (pv: MarketPreview) => (pv.upgrade ? (pv.more_permissions ? "同意新权限并升级" : "升级") : "同意并安装");

// ── the settings route (?tab=plugins&plugin=…) ──────────

/**
 * What the plugins tab shows, from its `plugin` parameter: a plugin's id (its page), `<id>/ui/<ui>` (a page it draws),
 * `-market` (the catalog), `-market/<market>/<plugin>` (one plugin of a market), `-markets` (the markets). A plugin's
 * id never starts with "-" or has "/" (the hub keeps `-` for itself).
 */
export type PluginsRoute =
  | { kind: "list" }
  | { kind: "plugin"; id: string }
  | { kind: "page"; id: string; ui: string }
  | { kind: "catalog" }
  | { kind: "item"; market: string; id: string }
  | { kind: "markets" };

export function pluginsRoute(sub: string | null | undefined): PluginsRoute {
  if (!sub) return { kind: "list" };
  if (sub === "-market") return { kind: "catalog" };
  if (sub === "-markets") return { kind: "markets" };
  const item = /^-market\/([^/]+)\/([^/]+)$/.exec(sub);
  if (item) return { kind: "item", market: item[1], id: item[2] };
  const page = /^([^/-][^/]*)\/ui\/([^/]+)$/.exec(sub);
  if (page) return { kind: "page", id: page[1], ui: page[2] };
  if (sub.startsWith("-") || sub.includes("/")) return { kind: "list" };
  return { kind: "plugin", id: sub };
}

export function routeSub(r: PluginsRoute): string | null {
  switch (r.kind) {
    case "list":
      return null;
    case "plugin":
      return r.id;
    case "page":
      return `${r.id}/ui/${r.ui}`;
    case "catalog":
      return "-market";
    case "item":
      return `-market/${r.market}/${r.id}`;
    case "markets":
      return "-markets";
  }
}
