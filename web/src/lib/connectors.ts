// The hub's connectors (docs/connectors.md, docs/protocol.md「连接器 API」): GitHub, Telegram, an extension's, as
// GET /api/v1/connectors lists them, their sign-in a step at a time, their switches, and the threads their inbox items
// open as (GET /api/v1/connectors/{id}/thread). Mail and Feishu keep their own pages (settings/Connectors.tsx).
// iOS: ConnectorsViews.swift, ConnectorSignInViews.swift, GitHubAuthViews.swift, GitHubInboxSettings.swift,
// ConnectorThread/*.
//
// The pure parts (state words, rows, sign-in checks, footers, thread pages) are plain functions for the tests.

import { api, enc } from "./api";

// ── wire types ───────────────────────────────────────────

/** One of GET /api/v1/connectors (connector.Info). */
export interface HubConnector {
  id: string;
  name: string;
  status: string; // ok | not_configured | not_signed_in | no_permission | rate_limited | error | disabled
  account?: string;
  via?: string;
  error?: string;
  /** How to fix it, a step a sentence. */
  guide?: string[];
  last_sync?: string;
  last_event?: string;
  capabilities?: string[];
  hosts?: string[];
  /** Switched on (absent: a hub from before the switch, always on). */
  enabled?: boolean;
  /** 进知识库 / 进收件箱: only there when it has an archive, an inbox. */
  archive?: boolean;
  inbox?: boolean;
  /** Its own settings (GitHub's: GitHubInboxOptions). */
  options?: unknown;
}

export interface PromptField {
  name: string;
  label: string;
  secret?: boolean;
  placeholder?: string;
}

/** A QR code's modules as the hub draws them (connector.PromptQR): one string of "0" / "1" per row, no quiet zone. */
export interface PromptQR {
  size: number;
  rows: string[];
}

/** Where a sign-in stands (connector.Prompt): the step to fill in next, a device code, a QR code, or done. */
export interface ConnectorPrompt {
  step?: string;
  fields?: PromptField[];
  kind?: string; // "" | device_code | qr
  url?: string;
  /** url's QR code for a qr step, filled in by the hub on its way to the page. */
  qr?: PromptQR;
  user_code?: string;
  interval?: number;
  hint?: string;
  error?: string;
  done?: boolean;
  account?: string;
}

/** What deleting a connector's data removed (DELETE …/data, …/archive). */
export interface ConnectorDeleted {
  chats?: number;
  people?: number;
  inbox?: number;
}

export interface GitHubMissing {
  scope: string;
  affects: string[];
}

export interface GitHubMachine {
  id: string;
  name: string;
  state?: string; // signed_in | not_signed_in | no_gh | too_old | offline | error
  account?: string;
  scopes?: string[];
  missing?: GitHubMissing[];
  error?: string;
}

/** GET /api/v1/connectors/github/auth. */
export interface GitHubAuth {
  source: string; // machine | device
  machine?: string;
  machines: GitHubMachine[];
  scopes?: string[];
  scopes_known?: boolean;
  missing?: GitHubMissing[];
}

export interface GitHubInboxOptions {
  ci: string;
  muted_repos: string[];
  ignore_branches?: string[];
}

// ── one connector ────────────────────────────────────────

/** Its state in a word or two. A state this page does not know (a newer hub's) is not 出错了. */
export function connectorState(c: HubConnector): string {
  switch (c.status) {
    case "ok":
      return "已连接";
    case "not_configured":
      return "未配置";
    case "not_signed_in":
      return "未登录";
    case "no_permission":
      return "没有权限";
    case "rate_limited":
      return "请求太多，暂停中";
    case "disabled":
      return "已关闭";
    case "error":
      return "出错了";
    default:
      return "状态未知";
  }
}

export type ConnectorTone = "green" | "orange" | "red" | "idle";

/** The dot beside the state: red only for what is broken; off, not set up, unknown or loading are gray. */
export function connectorTone(c?: HubConnector | null): ConnectorTone {
  switch (c?.status) {
    case "ok":
      return "green";
    case "rate_limited":
      return "orange";
    case "error":
    case "no_permission":
      return "red";
    default:
      return "idle";
  }
}

/** The list row's right side: who it reads as when connected, else its state. */
export function connectorSummary(c: HubConnector): string {
  return c.status === "ok" ? c.account || connectorState(c) : connectorState(c);
}

/** The user signs in to it here, a step at a time (Telegram; GitHub's device flow). */
export function signsInHere(c: HubConnector): boolean {
  return !!c.capabilities?.includes("login");
}

/** A program the hub runs that has not said what it has yet (switched off since the hub started, or starting). */
export function undescribed(c: HubConnector): boolean {
  return Array.isArray(c.capabilities) && c.capabilities.length === 0;
}

export function isEnabled(c: HubConnector): boolean {
  return c.enabled ?? true;
}

/** It brings things into the knowledge base or the inbox (Telegram), so its switch syncs and it has data to delete; a
 * chat bot (WeChat: a status and a sign-in, docs/channels.md) only talks. One not described yet (no capabilities, or a
 * hub from before them) counts as bringing them, as before there were bots (the apps do the same). */
export function storesData(c: HubConnector): boolean {
  const caps = c.capabilities;
  if (!caps?.length) return true;
  return caps.includes("archive") || caps.includes("inbox");
}

/** The connector's switch: 同步 X for one that brings data in, 启用 X for one that does not. */
export function switchLabel(c: HubConnector): string {
  return `${storesData(c) ? "同步" : "启用"} ${c.name}`;
}

export function isSignedIn(c: HubConnector): boolean {
  return c.status === "ok" || c.status === "rate_limited";
}

/** Switched on and still starting: the page asks again every second for a while. */
export function isStarting(c?: HubConnector | null): boolean {
  return !!c && isEnabled(c) && undescribed(c);
}

/** The connectors that get rows of their own after 邮箱 and 飞书 (those two keep their own groups; 飞书's only shows on a
 * hub with feature `lark`, `larkGroup`, so without it a connector named lark is listed like any other). */
export function otherConnectors(list: HubConnector[], larkGroup = true): HubConnector[] {
  return list.filter((c) => c.id !== "mail" && !(larkGroup && c.id === "lark"));
}

// ── brand and wording, by id ─────────────────────────────

/** The marks that exist as files (ui/BrandMark.tsx); a connector with none of its own draws the generic icon. */
export type BrandId = "github" | "telegram" | "lark";

/** What this client knows about one connector by id. Everything is optional: a connector that is not in the table (any
 * plugin's) gets the generic icon and the generic wording, built from its own `name`. Whether an extension may bring its own
 * wording in its manifest is a protocol question, not settled here. */
interface ConnectorProfile {
  brand?: BrandId;
  /** The inbox button for an item answered in the app itself ("在 Telegram 里回复"). */
  openTitle?: string;
  /** What switching it on brings in, in the switch's footer; the default is 它的内容. */
  reads?: string;
  /** The sign-in step's footer: where the credentials live and what is shown of them. */
  signInFooter?: string;
  /** Signing out also ends the session at the service (Telegram's log-out call); false or absent: the hub only forgets its copy. */
  logoutEndsRemoteSession?: boolean;
}

// A Map, not an object: an extension's id may be `constructor` or `valueOf`.
const PROFILES = new Map<string, ConnectorProfile>([
  ["github", { brand: "github" }],
  ["lark", { brand: "lark" }],
  [
    "telegram",
    {
      brand: "telegram",
      openTitle: "在 Telegram 里回复",
      reads: "它的私聊和群",
      signInFooter: "API 凭据和登录状态加密存在 hub 上，只显示 api_id；退出登录时一起删掉。",
      logoutEndsRemoteSession: true,
    },
  ],
  // No brand yet: the mark and its terms of use are undecided, so 微信 draws the generic icon until they are.
  ["wechat-connector", { signInFooter: "应用密钥加密存在 hub 上，只显示账号名；退出登录时一起删掉。" }],
]);

/** Which mark a connector has of its own; others draw the generic one. */
export function connectorBrand(id: string): BrandId | undefined {
  return PROFILES.get(id)?.brand;
}

/** The inbox button for a connector's item that is answered in the app itself; undefined for a connector with no wording of its own. */
export function connectorOpenTitle(id: string): string | undefined {
  return PROFILES.get(id)?.openTitle;
}

// ── its page ─────────────────────────────────────────────

export function switchFooter(c: HubConnector): string {
  if (!storesData(c)) return isEnabled(c) ? `关掉后 ${c.name} 停下来；登录状态留着。` : "打开后才能登录。";
  if (isEnabled(c)) return `关掉后不再读 ${c.name}；登录状态和已经存下的数据都留着。`;
  return `打开后才能登录，登录后${PROFILES.get(c.id)?.reads ?? "它的内容"}进知识库和收件箱，助手也能查。`;
}

export function signInFooter(c: HubConnector): string {
  return PROFILES.get(c.id)?.signInFooter ?? "登录状态加密存在 hub 上，退出登录时删掉。";
}

/** The sign-out confirmation. Only a connector known to end the remote session says so; every other one (GitHub's device
 * token, an extension's key such as wechat-connector's) only has the hub forget its copy, so it says the authorization stays. */
export function logoutMessage(c: HubConnector): string {
  if (PROFILES.get(c.id)?.logoutEndsRemoteSession) return `hub 上存的 API 凭据和登录状态都会删掉，${c.name} 那边这个设备会话也会结束。再用要重新登录。`;
  return "hub 上存的凭据和登录状态会删掉，再用要重新登录。对方服务里的授权不会自动撤销，需要的话到那边撤销。";
}

export function feedsFooter(c: HubConnector): string {
  const lines: string[] = [];
  if (c.archive !== undefined) {
    lines.push(c.archive ? `进知识库：${c.name} 的内容存进知识库，助手和 AI 分身都能查。关掉后不再存新的，已存的留着。` : `不进知识库：不再存新的内容。收件箱和助手查 ${c.name} 照常。`);
  }
  if (c.inbox === false) lines.push("不进收件箱：不再收新的，已有的条目留着。");
  return lines.join("\n");
}

export function deletedLine(r: ConnectorDeleted, withInbox = true): string {
  const kb = `知识库里 ${r.chats ?? 0} 个会话、${r.people ?? 0} 个人`;
  return withInbox ? `已删除：${kb}，收件箱里 ${r.inbox ?? 0} 条。` : `已删除：${kb}。`;
}

/** Every field of the step on screen is filled in. */
export function canSendStep(p: ConnectorPrompt | null, values: Record<string, string>): boolean {
  if (!p?.fields?.length) return false;
  return p.fields.every((f) => (values[f.name] ?? "").trim().length > 0);
}

/** A code to enter on a web page (GitHub's device flow), asked again until the user has. */
export function isDeviceCode(p?: ConnectorPrompt | null): boolean {
  return p?.kind === "device_code" && !!p.user_code;
}

/** How the page draws a sign-in step: fields to fill in, a device code, a QR code (or, without its modules, the
 * link it encodes), or a step this page cannot draw (a kind it does not know, a QR code with nothing to show, a form
 * with no fields) — said so, with what to do instead, rather than an empty form whose 下一步 never lights up. */
export type StepView = "fields" | "device_code" | "qr" | "qr_link" | "unsupported";

export function stepView(p: ConnectorPrompt): StepView {
  switch (p.kind ?? "") {
    case "":
      return p.fields?.length ? "fields" : "unsupported";
    case "device_code":
      return p.user_code ? "device_code" : "unsupported";
    case "qr":
      if (p.qr?.rows?.length) return "qr";
      return /^https?:\/\//i.test(p.url ?? "") ? "qr_link" : "unsupported";
    default:
      return "unsupported";
  }
}

/** A step the page asks again by itself until it is done (a code to enter, a QR code to scan). */
export function isPolledStep(p?: ConnectorPrompt | null): boolean {
  if (!p || p.done) return false;
  const v = stepView(p);
  return v === "device_code" || v === "qr" || v === "qr_link";
}

/** What a field's input should be: digits for codes, a phone keypad, a password. */
export function fieldInput(f: PromptField): { type: string; inputMode?: "numeric" | "tel"; autoComplete: string } {
  if (f.secret) return { type: "password", autoComplete: "current-password" };
  if (f.name === "phone") return { type: "tel", inputMode: "tel", autoComplete: "tel" };
  if (f.name === "code" || f.name === "api_id") return { type: "text", inputMode: "numeric", autoComplete: "one-time-code" };
  return { type: "text", autoComplete: "off" };
}

// ── GitHub ───────────────────────────────────────────────

export function scopeNames(missing: GitHubMissing[]): string {
  return missing.map((m) => m.scope).join("、");
}

/** A computer's gh login, in a line. */
export function githubMachineLine(m: GitHubMachine): string {
  switch (m.state) {
    case "signed_in": {
      const who = `gh 已登录${m.account ? ` · ${m.account}` : ""}`;
      return m.missing?.length ? `${who} · 少 ${scopeNames(m.missing)} 权限` : who;
    }
    case "not_signed_in":
      return "gh 没有登录";
    case "no_gh":
      return "没装 gh";
    case "too_old":
      return "personad 太旧，读不了 gh 的登录";
    case "offline":
      return "离线";
    default:
      return `读不了 gh 的登录${m.error ? `：${m.error}` : ""}`;
  }
}

export function githubMachineWarns(m: GitHubMachine): boolean {
  return m.state !== "signed_in" || !!m.missing?.length;
}

/** What to do about the scopes the token in use lacks. */
export function githubScopeAdvice(auth: GitHubAuth, missing: GitHubMissing[]): string {
  const names = scopeNames(missing);
  if (auth.source === "device") return `设备码登录少了 ${names} 权限：先换成别的登录方式，再用设备码重新登录一次。`;
  const machine = auth.machines.find((m) => m.id === auth.machine)?.name ?? "这台电脑";
  return `${machine} 的 gh 少了 ${names} 权限：在那台电脑上运行 gh auth refresh -s ${missing.map((m) => m.scope).join(",")}，或者改用设备码登录。`;
}

/** GitHub's inbox options as the hub gives them, or null when it gives none (or not that shape). */
export function githubOptions(raw: unknown): GitHubInboxOptions | null {
  if (!raw || typeof raw !== "object") return null;
  const o = raw as Record<string, unknown>;
  if (typeof o.ci !== "string") return null;
  const strings = (v: unknown) => (Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : undefined);
  return { ci: o.ci, muted_repos: strings(o.muted_repos) ?? [], ignore_branches: strings(o.ignore_branches) };
}

/** The CI choices, most quiet first. */
export const CI_MODES: { value: string; label: string }[] = [
  { value: "off", label: "关闭" },
  { value: "default_branch", label: "只看默认分支" },
  { value: "mine", label: "加上我的 PR" },
  { value: "all", label: "全部" },
];

export function githubInboxFooter(o: GitHubInboxOptions): string {
  const ci =
    o.ci === "off" ? "CI 失败不进收件箱。"
      : o.ci === "default_branch" ? "只有默认分支的 CI 失败进收件箱，算「需要你」。"
        : o.ci === "all" ? "所有分支的 CI 失败都算「需要你」。"
          : "默认分支和你自己开着的 PR 的 CI 失败算「需要你」，其他分支的算「知悉即可」。";
  const auto = (o.ignore_branches ?? ["train/*"]).join("、");
  return `${ci}同一个分支的失败合成一件，重跑也算同一件；后来跑成功、分支删了或 PR 合了就自动处理掉。自动分支（${auto}）不提醒。`
    + "真人的 @、请你 review、要求修改算「需要你」，机器人发的算「知悉即可」。静音的仓库什么都不进收件箱，已有的也会处理掉。";
}

/** A repository to mute: owner/name, not one already muted. */
export function mutableRepo(input: string, muted: string[]): string | null {
  const repo = input.trim();
  return /^[^/\s]+\/[^/\s]+$/.test(repo) && !muted.includes(repo) ? repo : null;
}

// ── requests ─────────────────────────────────────────────

/** The hub's connectors; `all` takes Feishu and mail too. */
export async function listConnectors(all = false): Promise<HubConnector[]> {
  const r = await api<{ connectors?: HubConnector[] }>(`/connectors${all ? "?all=1" : ""}`);
  return r.connectors ?? [];
}

export function connectorLogin(id: string, step: string, values: Record<string, string>): Promise<ConnectorPrompt> {
  return api<ConnectorPrompt>(`/connectors/${enc(id)}/login`, { method: "POST", json: { step, values } });
}

export function connectorLogout(id: string): Promise<unknown> {
  return api(`/connectors/${enc(id)}/logout`, { method: "POST" });
}

/** The switch (`enabled`), 进知识库 (`archive`), 进收件箱 (`inbox`) or its own settings (`options`). */
export function setConnector(id: string, body: { enabled?: boolean; archive?: boolean; inbox?: boolean; options?: unknown }): Promise<HubConnector> {
  return api<HubConnector>(`/connectors/${enc(id)}/settings`, { method: "PUT", json: body });
}

export function deleteConnectorData(id: string): Promise<ConnectorDeleted> {
  return api<ConnectorDeleted>(`/connectors/${enc(id)}/data`, { method: "DELETE" });
}

export function deleteConnectorArchive(id: string): Promise<ConnectorDeleted> {
  return api<ConnectorDeleted>(`/connectors/${enc(id)}/archive`, { method: "DELETE" });
}

export function githubAuth(): Promise<GitHubAuth> {
  return api<GitHubAuth>("/connectors/github/auth");
}

/** Where GitHub's token comes from: a machine's gh login, or the device flow (machine null). */
export function setGitHubAuth(machine: string | null): Promise<GitHubAuth> {
  return api<GitHubAuth>("/connectors/github/auth", { method: "PUT", json: machine ? { source: "machine", machine } : { source: "device" } });
}

// ── threads (an inbox item opened) ───────────────────────

export interface ThreadPerson {
  id?: string;
  name: string;
  avatar?: string;
}

export interface ThreadMedia {
  kind: string; // image | sticker | video | voice | file
  ref?: string;
  thumb?: string;
  mime?: string;
  name?: string;
  size?: number;
  duration?: number;
  width?: number;
  height?: number;
  emoji?: string;
}

export interface ThreadMessage {
  id: string;
  at: string;
  sender: ThreadPerson;
  mine?: boolean;
  event?: boolean;
  text?: string;
  format?: string; // "" | markdown
  media?: ThreadMedia[];
  reply_to?: { id: string; from?: string; text?: string };
  edited?: boolean;
  url?: string;
  kind?: string; // "" | review | code | check
  review?: string; // approved | changes_requested | commented | dismissed
  code?: { path: string; line?: number; hunk?: string };
  check?: { name: string; state: string; url?: string; log?: string };
}

export interface ThreadChat {
  id: string;
  name: string;
  kind: string; // dm | group | channel | pr | issue
  url?: string;
  avatar?: string;
  members?: number;
  /** The action that answers here; absent: the user cannot answer in this thread. */
  reply?: string;
  state?: string;
  author?: ThreadPerson;
  labels?: { name: string; color?: string }[];
  branch?: { head: string; base: string };
  body?: string;
  body_format?: string;
}

export interface ThreadPage {
  chat: ThreadChat;
  messages: ThreadMessage[];
  before?: string;
}

export function isChatThread(c?: ThreadChat): boolean {
  return c?.kind === "dm" || c?.kind === "group" || c?.kind === "channel";
}

export function isGitHubThread(c?: ThreadChat): boolean {
  return c?.kind === "pr" || c?.kind === "issue";
}

export const THREAD_PAGE = 30;

export function loadThread(id: string, thread: string, before = ""): Promise<ThreadPage> {
  const q = new URLSearchParams({ thread, limit: String(THREAD_PAGE) });
  if (before) q.set("before", before);
  return api<ThreadPage>(`/connectors/${enc(id)}/thread?${q}`);
}

export function replyInThread(id: string, body: { thread: string; action: string; text: string; reply_to?: string }): Promise<unknown> {
  return api(`/connectors/${enc(id)}/thread/reply`, { method: "POST", json: body });
}

/** A media ref's URL under /api/ (an <img> adds the token with withToken). */
export function mediaURL(id: string, ref: string): string {
  return `/api/v1/connectors/${enc(id)}/media?ref=${encodeURIComponent(ref)}`;
}

/** An older page put before what is on screen, one copy per id, the page's chat the newest. */
export function mergeOlder(have: ThreadPage, older: ThreadPage): ThreadPage {
  const ids = new Set(have.messages.map((m) => m.id));
  return { chat: have.chat, messages: [...older.messages.filter((m) => !ids.has(m.id)), ...have.messages], before: older.before };
}

/** The newest page again (after a reply): its messages replace the newest ones, the older ones stay. */
export function mergeNewest(have: ThreadPage | null, newest: ThreadPage): ThreadPage {
  if (!have || !newest.messages.length) return newest;
  const first = newest.messages[0].id;
  const cut = have.messages.findIndex((m) => m.id === first);
  if (cut < 0) return newest;
  return { chat: newest.chat, messages: [...have.messages.slice(0, cut), ...newest.messages], before: have.before };
}

/** Whether a message starts a new group (another sender, or more than five minutes after the last). */
export function startsGroup(m: ThreadMessage, prev?: ThreadMessage): boolean {
  if (!prev || prev.event || m.event || m.reply_to) return true;
  if ((prev.sender.id || prev.sender.name) !== (m.sender.id || m.sender.name) || !!prev.mine !== !!m.mine) return true;
  return Date.parse(m.at) - Date.parse(prev.at) > 5 * 60_000;
}

/** A file / voice message described: 示例.pdf · 278 KB, 语音 0:12. */
export function mediaLine(m: ThreadMedia): string {
  const dur = m.duration ? `${Math.floor(m.duration / 60)}:${String(m.duration % 60).padStart(2, "0")}` : "";
  if (m.kind === "voice") return ["语音", dur].filter(Boolean).join(" ");
  if (m.kind === "video") return ["视频", dur].filter(Boolean).join(" ");
  const size = m.size ? (m.size >= 1 << 20 ? `${(m.size / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(m.size / 1024))} KB`) : "";
  return [m.name || "文件", size].filter(Boolean).join(" · ");
}

/** GitHub's state of a pull request / issue in a word. */
export function githubStateLabel(state?: string, kind?: string): string {
  switch (state) {
    case "open":
      return "开着";
    case "merged":
      return "已合并";
    case "closed":
      return kind === "issue" ? "已关闭" : "已关闭（未合并）";
    case "draft":
      return "草稿";
    default:
      return state ?? "";
  }
}

export function reviewLabel(review?: string): string {
  switch (review) {
    case "approved":
      return "批准了";
    case "changes_requested":
      return "要求修改";
    case "dismissed":
      return "review 被撤回";
    default:
      return "review 了";
  }
}

export function checkLabel(state: string): string {
  switch (state) {
    case "success":
      return "通过";
    case "failure":
      return "失败";
    case "pending":
      return "进行中";
    case "skipped":
      return "跳过";
    case "cancelled":
      return "已取消";
    default:
      return state;
  }
}

/** A label's own color (six hex digits) or none. */
export function labelColor(color?: string): string | undefined {
  return color && /^[0-9a-f]{6}$/i.test(color) ? `#${color}` : undefined;
}
