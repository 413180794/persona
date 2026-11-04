// The admin API client (docs/admin.md「接口」): a session cookie, not the web client's bearer token. Writes carry the
// session's CSRF token, which lives in memory only (GET state hands it out again after a reload).
import { catalogQuery, type Catalog, type Market } from "../lib/plugin-market";
import type { AddedMachine, AdminPlugin, AdminPlugins, ConnectorRepair, LinkPair, MeshView, PluginPreview } from "./types";

export class AdminError extends Error {
  status: number;
  code: string;
  retryAfter: number;
  body: Record<string, unknown>;
  constructor(status: number, message: string, body: Record<string, unknown>, retryAfter = 0) {
    super(message);
    this.status = status;
    this.code = typeof body.code === "string" ? body.code : "";
    this.retryAfter = retryAfter;
    this.body = body;
  }
}

export type Transport = (url: string, init: RequestInit) => Promise<Response>;

let transport: Transport = (url, init) => fetch(url, init);
let csrf = "";
let onLoginRequired: () => void = () => {};

export function setTransport(t: Transport) {
  transport = t;
}

export function setCsrf(token: string | undefined) {
  csrf = token ?? "";
}

export function handleLoginRequired(fn: () => void) {
  onLoginRequired = fn;
}

type Method = "GET" | "POST" | "PUT" | "DELETE";

export async function adminApi<T>(path: string, method: Method = "GET", json?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  let body: string | undefined;
  if (method !== "GET") {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(json ?? {});
    if (csrf) headers["X-Jarvis-CSRF"] = csrf;
  }
  return answer<T>(await transport(`/api/v1/admin${path}`, { method, headers, body, credentials: "same-origin", cache: "no-store" }));
}

// adminUpload posts a file as it is (application/octet-stream): the one admin write whose body is not JSON (a plugin
// package, docs/admin.md「插件」). query carries what the body cannot.
export async function adminUpload<T>(path: string, file: Blob, query: Record<string, string>): Promise<T> {
  const headers: Record<string, string> = { "Content-Type": "application/octet-stream" };
  if (csrf) headers["X-Jarvis-CSRF"] = csrf;
  const q = new URLSearchParams(Object.entries(query).filter(([, v]) => v)).toString();
  return answer<T>(await transport(`/api/v1/admin${path}?${q}`, { method: "POST", headers, body: file, credentials: "same-origin", cache: "no-store" }));
}

async function answer<T>(res: Response): Promise<T> {
  const text = await res.text();
  let data: Record<string, unknown> = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    data = { error: text };
  }
  if (!res.ok) {
    const fromBody = Number(data.retry_after);
    const fromHeader = Number(res.headers.get("Retry-After"));
    const retry = Number.isFinite(fromBody) && fromBody > 0 ? fromBody : Number.isFinite(fromHeader) && fromHeader > 0 ? fromHeader : 0;
    const err = new AdminError(res.status, typeof data.error === "string" && data.error ? data.error : `HTTP ${res.status}`, data, retry);
    if (res.status === 401 && err.code === "login_required") {
      setCsrf("");
      onLoginRequired();
    }
    throw err;
  }
  return data as T;
}

const MESSAGES: Record<string, string> = {
  bad_password: "密码不对",
  weak_password: "密码至少 8 个字符",
  already_set: "已经有人设好了管理员密码，请直接登录",
  setup_local_only: "首次设置只能在 hub 所在的局域网里做",
  setup_code_required: "先输入设置码",
  bad_setup_code: "设置码不对或过期了，在 hub 机器上运行 jarvisd hub admin setup-code 取一个新的",
  login_required: "登录过期了，请重新登录",
  csrf: "页面过期了，刷新一下再试",
  tls_required: "需要经 TLS 端口访问",
  widen_cli_only: "放宽访问范围只能在 hub 机器上用命令行或部署脚本",
  from_env: "这一项由部署时的环境变量决定（JARVIS_ADMIN_ACCESS / JARVIS_PUBLIC_URL），这里不能改",
  would_lock_you_out: "改完之后你就进不来了",
  invalid_cidr: "网段格式不对，例如 192.0.2.0/24",
  invalid_id: "id 只能用小写字母、数字和 - _ .，以字母或数字开头",
  invalid_ssh_host: "ssh 主机别名不对",
  invalid_name: "名字最多 40 个字，不能换行",
  invalid_proxy: "代理要写成 socks5://主机:端口",
  invalid_kind: "这个版本的 hub 不支持这种接入方式，先升级 hub",
  ssh_unsupported: "不再添加 ssh 电脑：在那台电脑上装 node 服务，选「电脑来连」或「hub 来连」",
  not_service: "这台电脑不是「hub 来连」接入的",
  exists: "已经有这个 id 的电脑了",
  not_active: "重启 hub 之后才能对这台电脑操作",
  not_link: "这台电脑不是「电脑来连」接入的",
  not_in_group: "这台 hub 已经不在组里了，刷新看看",
  not_supervised: "这个 hub 不是由 systemd 或 Docker 管着的，请到 hub 机器上手动重启",
  not_implemented: "这个版本的 hub 还没有这个功能",
  invalid_address: "地址格式不对，例如 https://192.0.2.20:18662",
  unreachable: "连不上这个地址，检查地址、端口和对方的防火墙",
  invalid_code: "这不是一个有效的互联码",
  no_plugins: "这台 hub 没有启用插件",
  busy: "正在预览另一个插件，等它结束再试",
  too_large: "包太大了",
  confirm_required: "先看过它能做什么，确认后再装",
  no_markets: "这台 hub 读不了插件市场（没有数据库）",
};

// errorText is what a person reads for a failed call. Codes not listed here (pair_failed, member_hub,
// center_unreachable…) carry a message the hub wrote for people: shown as it is.
export function errorText(e: unknown): string {
  if (e instanceof AdminError) {
    if (e.code === "rate_limited") return e.retryAfter ? `试错太多次了，${e.retryAfter} 秒后再试` : "试错太多次了，稍后再试";
    return MESSAGES[e.code] ?? e.message;
  }
  if (e instanceof TypeError) return "连不上 hub";
  return e instanceof Error ? e.message : String(e);
}

// ── the center and connectors (docs/plan-hub-center.md 6) ──

const enc = encodeURIComponent;

export const getMesh = () => adminApi<MeshView>("/mesh");

// setCenter makes hubId (this hub's own id too) the group's center. confirm: go ahead although the center now in place
// cannot be reached (409 confirm_required without it; that hub becomes a member when it comes back).
export const setCenter = (hubId: string, confirm = false) => adminApi<MeshView>(`/peers/${enc(hubId)}/center`, "POST", { confirm });

// addLinkMachine: the computer dials the hub (its jarvis-link, paired with linkPair next). Live at once.
export const addLinkMachine = (m: { id: string; name: string; icon: string }) => adminApi<AddedMachine>("/machines", "POST", { kind: "link", ...m });

// addServiceMachine: the hub dials the computer's connector. The hub pairs with the pasted link first and only then
// writes hub.json; 502 pair_failed says why in words.
export const addServiceMachine = (m: { id: string; name: string; icon: string; pair_link: string; address?: string; proxy?: string }) =>
  adminApi<AddedMachine>("/machines", "POST", { kind: "service", ...m });

export const linkPair = (id: string) => adminApi<LinkPair>(`/machines/${enc(id)}/link-pair`, "POST");

export const repairConnector = (id: string, pairLink: string, address?: string) =>
  adminApi<ConnectorRepair>(`/machines/${enc(id)}/connector`, "POST", { pair_link: pairLink, address: address || undefined });

// ── plugins (docs/admin.md「插件」): install, upgrade, roll back, uninstall, switch ──

export const getPlugins = () => adminApi<AdminPlugins>("/plugins");

// previewPlugin stages source (dir: / file: / https://…#sha256= / remote:) on the hub: what it may do, before anything
// installs. upgrade: the installed plugin's id when this is its new version.
export const previewPlugin = (source: string, upgrade?: string) => adminApi<PluginPreview>("/plugins/preview", "POST", { source, upgrade: upgrade || undefined });

export const uploadPlugin = (file: File, upgrade?: string) => adminUpload<PluginPreview>("/plugins/upload", file, { name: file.name, upgrade: upgrade ?? "" });

export const installPlugin = (pv: PluginPreview) => adminApi<AdminPlugin>("/plugins/install", "POST", { preview_id: pv.preview_id, permissions_digest: pv.permissions_digest });

export const upgradePlugin = (id: string, pv: PluginPreview) =>
  adminApi<AdminPlugin>(`/plugins/${enc(id)}/upgrade`, "POST", { preview_id: pv.preview_id, permissions_digest: pv.permissions_digest });

export const rollbackPlugin = (id: string) => adminApi<AdminPlugin>(`/plugins/${enc(id)}/rollback`, "POST");

export const setPluginEnabled = (id: string, enabled: boolean) => adminApi<AdminPlugin>(`/plugins/${enc(id)}/enabled`, "PUT", { enabled });

export const uninstallPlugin = (id: string, keepData: boolean) => adminApi<{ ok: boolean }>(`/plugins/${enc(id)}`, "DELETE", { keep_data: keepData });

// ── plugin markets (docs/plugins.md「插件市场」): the apps' routes under /plugins/-/, behind the admin session. Installing
// and upgrading from a market's preview go through installPlugin / upgradePlugin above (the preview's id and digest).

export const getMarkets = () => adminApi<{ markets?: Market[] }>("/plugins/-/markets").then((r) => r.markets ?? []);
export const addMarket = (spec: string, token?: string) => adminApi<Market>("/plugins/-/markets", "POST", token ? { spec, token } : { spec });
export const removeMarket = (id: string) => adminApi<{ ok: boolean }>(`/plugins/-/markets/${enc(id)}`, "DELETE");
export const refreshMarket = (id: string) => adminApi<Market>(`/plugins/-/markets/${enc(id)}/refresh`, "POST");
export const setMarketToken = (id: string, token: string) => adminApi<Market>(`/plugins/-/markets/${enc(id)}/token`, "PUT", { token });
export const getCatalog = (q?: string, market?: string, refresh?: boolean) =>
  adminApi<Catalog>(`/plugins/-/catalog${catalogQuery(q, market, refresh)}`).then((c) => ({ ...c, markets: c.markets ?? [], plugins: c.plugins ?? [] }));
export const previewFromMarket = (market: string, plugin: string) => adminApi<PluginPreview>("/plugins/-/catalog/preview", "POST", { market, plugin });
