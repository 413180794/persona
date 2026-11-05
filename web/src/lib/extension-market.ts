// 扩展市场 (docs/extensions.md「扩展市场」): markets the hub reads (an index persona-extensions.json on a site or a GitHub
// repository), their catalog, and installing, upgrading and removing from it. A full-rights device may: the package is
// pinned by the index's sha256 and nothing installs before the user has seen what it may do (the preview's
// permissions digest). The admin UI has the same routes under /api/v1/admin/extensions/-/ (its api.ts), the same words
// (here).
import { api, enc } from "./api";
import { extRoot, listOf } from "./extension-api";
import { changed, type HubExtensionDetail, type ExtensionPermission } from "./extensions";
import { previewConsent, type DependencyState, type PackMember } from "./extension-model";

/** One market the hub reads (its token, if any, is never sent back). */
export interface Market {
  id: string;
  spec: string;
  name: string;
  description?: string;
  homepage?: string;
  has_token: boolean;
  /** How many extensions its index has. */
  extensions: number;
  warnings?: string[];
  /** Why the last read failed; the extensions it had before stay listed. */
  error?: string;
  fetched_at?: string;
  added_at?: string;
  /** How its index is signed (a hub with market signatures, #1848). */
  signature?: MarketSignature;
}

/** A market index's signature as the hub found it: its state and the key it trusts. */
export interface MarketSignature {
  state: string;
  key_id?: string;
}

/** One extension of a market, as the hub sees it against what is installed. */
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
  permissions?: ExtensionPermission[];
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
  extensions: CatalogItem[];
  platform?: string;
}

/** What installing (or upgrading) would do, before it does: POST …/catalog/preview. */
export interface MarketPreview {
  preview_id: string;
  manifest: { id: string; name: string; version: string; description?: string; publisher?: string; homepage?: string; contributes?: Record<string, unknown[]>; extension_pack?: PackMember[] };
  depends?: DependencyState[];
  bundle?: MarketPreview[];
  unresolved?: string[];
  permissions: ExtensionPermission[];
  permissions_digest: string;
  warnings?: string[];
  expires_at: string;
  upgrade?: boolean;
  from?: string;
  more_permissions?: boolean;
  added_permissions?: ExtensionPermission[];
  market?: string;
}

// ── the hub ─────────────────────────────────────────────

/** The hub's market routes: /extensions/-, or /plugins/- on a hub from before #1844. */
const M = () => `${extRoot()}/-`;

/** A market as either kind of hub sends it: how many extensions under `extensions` (or `plugins`). */
export const marketView = (m: Market & { plugins?: number }): Market => {
  const { plugins, ...rest } = m;
  return { ...rest, extensions: m.extensions ?? plugins ?? 0 };
};

export const listMarkets = () => api<{ markets?: Market[] }>(`${M()}/markets`).then((r) => (r.markets ?? []).map(marketView));
export const addMarket = (spec: string, token?: string) => api<Market>(`${M()}/markets`, { method: "POST", json: token ? { spec, token } : { spec } }).then(marketView);
export const removeMarket = (id: string) => api<{ ok: boolean }>(`${M()}/markets/${enc(id)}`, { method: "DELETE" });
export const refreshMarket = (id: string) => api<Market>(`${M()}/markets/${enc(id)}/refresh`, { method: "POST" }).then(marketView);
/** A new token ("" takes it away); the hub reads the index again with it. */
export const setMarketToken = (id: string, token: string) => api<Market>(`${M()}/markets/${enc(id)}/token`, { method: "PUT", json: { token } }).then(marketView);

export function catalogQuery(q?: string, market?: string, refresh?: boolean): string {
  const p = new URLSearchParams();
  if (q?.trim()) p.set("q", q.trim());
  if (market) p.set("market", market);
  if (refresh) p.set("refresh", "1");
  const s = p.toString();
  return s ? `?${s}` : "";
}

export const getCatalog = (q?: string, market?: string, refresh?: boolean) =>
  api<Catalog & { plugins?: CatalogItem[] }>(`${M()}/catalog${catalogQuery(q, market, refresh)}`).then(catalogOf);
/** A catalog as either kind of hub sends it (the list under `extensions`, or `plugins`). */
export const catalogOf = (c: Catalog & { plugins?: CatalogItem[] }): Catalog => ({
  markets: (c.markets ?? []).map(marketView),
  extensions: listOf(c),
  ...(c.platform ? { platform: c.platform } : {}),
});
/** Both keys: a hub from before #1844 reads `plugin`, a newer one `extension` (or `plugin`). */
export const previewFromMarket = (market: string, extension: string) =>
  api<MarketPreview>(`${M()}/catalog/preview`, { method: "POST", json: { market, extension, plugin: extension } });
export const installPreview = (pv: MarketPreview) =>
  api<HubExtensionDetail>(`${M()}/install`, { method: "POST", json: previewConsent(pv) }).then(changed);
export const upgradePreview = (id: string, pv: MarketPreview) =>
  api<HubExtensionDetail>(`${extRoot()}/${enc(id)}/-/upgrade`, { method: "POST", json: previewConsent(pv) }).then(changed);
/** Only one installed from a market (`origin` market:…); keepData leaves ~/.persona/extensions/data/<id>/. */
export const uninstallExtension = (id: string, keepData = false, cascade = false, members = false) => {
  const q = new URLSearchParams();
  if (keepData) q.set("keep_data", "1");
  if (cascade) q.set("cascade", "1");
  if (members) q.set("members", "1");
  return api<{ ok: boolean }>(`${extRoot()}/${enc(id)}/-/${q.size ? `?${q}` : ""}`, { method: "DELETE" }).then(changed);
};

// ── words and rules ─────────────────────────────────────

/** The market an extension was installed from: `market:<market id>/<extension id>`; "" otherwise. */
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
  "三种写法：https 地址（网站上的目录读其中的 persona-extensions.json，或索引文件本身）；github:owner/repo[@分支]（经 GitHub API 读，私有仓库填 token）；git:<地址>[#分支]（git@host:path.git、ssh://…、https://…，前面的 git: 对 git@ 的可以省略，用 hub 机器自己的 SSH 凭据，不收 token）。地址原样交给 hub，读不到时 hub 说原因。";

export type ItemState = { kind: "install" | "upgrade" | "installed" | "problem" | "elsewhere"; label: string; tone: "accent" | "green" | "orange" | "red" | "gray" };

/** Where a catalog extension stands on this hub, and what the button does. */
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

/** Uninstalling from here: only an extension this market installed. */
export const canUninstall = (it: CatalogItem) => Boolean(it.installed && it.from_this_market);

/** A market's line under its name: how many extensions, when it was read, whether it has a token. */
export function marketLine(m: Market, ago: (iso?: string) => string): string {
  return [`${m.extensions} 个扩展`, m.fetched_at ? `${ago(m.fetched_at)}读过` : "还没读过", m.has_token ? "有 token" : "", signatureText(m.signature)]
    .filter(Boolean)
    .join(" · ");
}

const SIGNATURES: Record<string, string> = {
  verified: "签名已验证",
  signed: "签名已验证",
  unsigned: "未签名",
  invalid: "签名对不上",
  missing: "签名不见了",
  key_changed: "签名的 key 换了",
};

/** How the market's index is signed (#1848); "" from a hub that does not say. */
export function signatureText(s?: MarketSignature): string {
  if (!s?.state) return "";
  return SIGNATURES[s.state] ?? `签名：${s.state}`;
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

// ── the settings route (?tab=extensions&extension=…) ──────────

/**
 * What the extensions tab shows, from its `extension` parameter: an extension's id (its page), `<id>/ui/<ui>` (a page it draws),
 * `-market` (the catalog), `-market/<market>/<extension>` (one extension of a market), `-markets` (the markets).
 * An extension's id never starts with "-" or has "/" (the hub keeps `-` for itself).
 */
export type ExtensionsRoute =
  | { kind: "list" }
  | { kind: "extension"; id: string }
  | { kind: "page"; id: string; ui: string }
  | { kind: "catalog" }
  | { kind: "item"; market: string; id: string }
  | { kind: "markets" };

export function extensionsRoute(sub: string | null | undefined): ExtensionsRoute {
  if (!sub) return { kind: "list" };
  if (sub === "-market") return { kind: "catalog" };
  if (sub === "-markets") return { kind: "markets" };
  const item = /^-market\/([^/]+)\/([^/]+)$/.exec(sub);
  if (item) return { kind: "item", market: item[1], id: item[2] };
  const page = /^([^/-][^/]*)\/ui\/([^/]+)$/.exec(sub);
  if (page) return { kind: "page", id: page[1], ui: page[2] };
  if (sub.startsWith("-") || sub.includes("/")) return { kind: "list" };
  return { kind: "extension", id: sub };
}

export function routeSub(r: ExtensionsRoute): string | null {
  switch (r.kind) {
    case "list":
      return null;
    case "extension":
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
