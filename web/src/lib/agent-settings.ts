// 设置 › Claude Code / Codex 设置 (the node's whitelist of ~/.claude/settings.json and ~/.codex/config.toml keys,
// iOS ClaudeSettingsView) and 设置 › 连接器 › 邮箱's add form (iOS MailAccountForm, docs/mail.md): the pure part.
import type { Machine } from "./types";

// ── agent settings ──

export type AgentKind = "claude" | "codex";

export type SettingValue = boolean | string | null;

/** One editable option, as GET /machines/{mid}/claude-settings (or codex-settings) lists it. value null = unset. */
export interface AgentSetting {
  key: string;
  title: string;
  detail: string;
  type: "bool" | "enum" | "string" | string;
  options?: string[];
  default?: SettingValue;
  value?: SettingValue;
}

export const AGENT_TEXT: Record<AgentKind, { name: string; path: string; file: string; footer: string }> = {
  claude: {
    name: "Claude Code",
    path: "claude-settings",
    file: "~/.claude/settings.json",
    footer: "改的是那台机器的 ~/.claude/settings.json，只动下面这几项；新开的 Claude 会话生效，正在跑的会话一般要重启才生效。",
  },
  codex: {
    name: "Codex",
    path: "codex-settings",
    file: "~/.codex/config.toml",
    footer: "改的是那台机器的 ~/.codex/config.toml，只动下面这几项，文件里别的配置（模型提供方、项目信任等）原样保留；新开的 Codex 会话生效。",
  },
};

/** The value in effect: what is set, else the default. */
export function effective(s: AgentSetting): SettingValue {
  return s.value ?? s.default ?? null;
}

export function isUnset(s: AgentSetting): boolean {
  return s.value === null || s.value === undefined;
}

/** The list with one key's value replaced (an optimistic change, or its rollback). */
export function withValue(list: AgentSetting[], key: string, value: SettingValue): AgentSetting[] {
  return list.map((s) => (s.key === key ? { ...s, value } : s));
}

/** What a text field sends: an empty field is "back to the default" (null), not an empty string written to the file. */
export function textValue(input: string): string | null {
  const t = input.trim();
  return t ? t : null;
}

/** The machine the page edits: the remembered one while it is online, else the first online one, else "". */
export function pickMachine(machines: Pick<Machine, "id" | "online">[], remembered: string): string {
  if (machines.some((m) => m.id === remembered && m.online)) return remembered;
  return machines.find((m) => m.online)?.id ?? "";
}

export interface ApplyResult {
  id: string;
  name: string;
  error?: string;
}

/** One line for a change sent to several machines: "已应用到 3 台机器", or which failed and why. */
export function applySummary(results: ApplyResult[]): { ok: boolean; text: string } {
  const failed = results.filter((r) => r.error);
  if (!failed.length) return { ok: true, text: results.length > 1 ? `已应用到 ${results.length} 台机器` : "已保存" };
  const done = results.length - failed.length;
  const why = failed.map((r) => `${r.name}：${r.error}`).join("；");
  return { ok: false, text: done ? `${done} 台已应用，${failed.length} 台失败（${why}）` : `没有改成（${why}）` };
}

/** How a value reads in a message: 开 / 关, the option, or 默认. */
export function valueText(v: SettingValue): string {
  if (v === null || v === undefined || v === "") return "默认";
  if (typeof v === "boolean") return v ? "开" : "关";
  return v;
}

// ── Claude Code › 屏蔽的配置目录 (GET /machines/{mid}/claude-dirs, docs/claude-integration.md「屏蔽配置目录」) ──

/** One config directory of a computer (CLAUDE_CONFIG_DIR), as the hub lists it. */
export interface ClaudeDir {
  path: string;
  default?: boolean;
  blocked?: boolean;
  blocked_by?: string;
  source: "default" | "config" | "found" | string;
  running: number;
  sessions?: number;
}

export interface ClaudeDirsState {
  dirs: ClaudeDir[];
  blocked: string[];
  home?: string;
  live: boolean;
}

/** The path as the user reads it: the computer's home as ~. */
export function shortPath(path: string, home?: string): string {
  if (home && (path === home || path.startsWith(home + "/"))) return "~" + path.slice(home.length);
  return path;
}

/** What a row says under the path: where it came from, how many sessions, how many claudes run from it. */
export function dirMeta(d: ClaudeDir, home?: string): string {
  const parts: string[] = [];
  if (d.default) parts.push("默认目录，不能屏蔽");
  else if (d.source === "config") parts.push("hub.json 里配的");
  else parts.push("自动发现");
  if (d.sessions) parts.push(`${d.sessions} 个会话`);
  if (d.running) parts.push(`${d.running} 个正在运行`);
  if (d.blocked && d.blocked_by && !samePath(d.blocked_by, d.path, home)) parts.push(`由规则 ${d.blocked_by} 屏蔽`);
  return parts.join(" · ");
}

/** The patterns after a row's 屏蔽 / 取消屏蔽: blocking adds the directory's own path; unblocking lifts the rule that
 *  blocks it (a glob lifts every directory it covers — the row says which rule). */
export function toggledPatterns(state: ClaudeDirsState, d: ClaudeDir): string[] {
  if (!d.blocked) return state.blocked.includes(d.path) ? state.blocked : [...state.blocked, shortPath(d.path, state.home)];
  const rule = d.blocked_by ?? d.path;
  return state.blocked.filter((p) => p !== rule);
}

/** Why a typed pattern cannot be added ("" = it can). */
export function patternError(input: string, existing: string[]): string {
  const p = input.trim().replace(/\/+$/, "");
  if (!p) return "填一个目录或通配，例如 ~/.automation/*";
  if (!p.startsWith("/") && !p.startsWith("~/")) return "要以 / 或 ~/ 开头（~ 是那台电脑的家目录）";
  if (p === "~" || p === "/") return "不能屏蔽整个家目录或根目录";
  if (existing.includes(p)) return "已经屏蔽了";
  return "";
}

/** The patterns with a typed one added (trimmed, no trailing /). */
export function withPattern(existing: string[], input: string): string[] {
  return [...existing, input.trim().replace(/\/+$/, "")];
}

/** Rules that block none of the listed directories (a glob nothing matches yet, a directory not seen): listed on
 *  their own so they can be deleted; the others are lifted from the rows they block. */
export function looseRules(state: ClaudeDirsState): string[] {
  return state.blocked.filter((p) => !state.dirs.some((d) => d.blocked && (d.blocked_by === p || samePath(p, d.path, state.home))));
}

/** A pattern written as this directory's path, with ~ or without. */
function samePath(pattern: string, path: string, home?: string): boolean {
  return pattern === path || pattern === shortPath(path, home);
}

// ── 连接器 › 邮箱 › 添加 ──

export type MailKind = "gmail" | "feishu" | "imap";

export const MAIL_KINDS: { k: MailKind; label: string }[] = [
  { k: "gmail", label: "Gmail" },
  { k: "feishu", label: "飞书" },
  { k: "imap", label: "其他 IMAP" },
];

export interface MailForm {
  kind: MailKind;
  email: string;
  password: string;
  imapHost: string;
  imapPort: string;
  smtpHost: string;
  smtpPort: string;
}

export const EMPTY_MAIL_FORM: MailForm = { kind: "imap", email: "", password: "", imapHost: "", imapPort: "993", smtpHost: "", smtpPort: "465" };

/** POST /api/v1/mail/accounts's body (docs/protocol.md「邮件 API」). */
export interface AddMailAccount {
  name: string;
  email: string;
  provider: "feishu" | "imap";
  auth: "password";
  password: string;
  imap_host?: string;
  imap_port?: number;
  smtp_host?: string;
  smtp_port?: number;
  smtp_security?: "tls" | "starttls";
}

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

const port = (p: string) => {
  const n = Number(p.trim());
  return Number.isInteger(n) && n > 0 && n < 65536 ? n : undefined;
};

/** What is wrong with the form, field by field (empty = can be sent). Gmail is never sent from here. */
export function mailErrors(f: MailForm): Partial<Record<keyof MailForm, string>> {
  const e: Partial<Record<keyof MailForm, string>> = {};
  if (f.kind === "gmail") return { kind: "Gmail 要用 Google 账号登录，只能在手机上添加" };
  if (!f.email.trim()) e.email = "填邮箱地址";
  else if (!EMAIL.test(f.email.trim())) e.email = "邮箱地址的格式不对";
  if (!f.password) e.password = f.kind === "feishu" ? "填飞书邮箱的第三方客户端登录密码" : "填密码或授权码";
  if (f.kind === "imap") {
    if (f.imapHost.trim() && !port(f.imapPort)) e.imapPort = "端口是 1–65535 的数字";
    if (f.smtpHost.trim() && !port(f.smtpPort)) e.smtpPort = "端口是 1–65535 的数字";
  }
  return e;
}

/** The request for a password account; null for Gmail (Google sign-in, on the phone). Servers left empty are guessed
 * by the hub from the domain; 飞书's are fixed by the hub. SMTP on 587 is STARTTLS, anything else TLS. */
export function mailRequest(f: MailForm): AddMailAccount | null {
  if (f.kind === "gmail") return null;
  const email = f.email.trim();
  const req: AddMailAccount = { name: email, email, provider: f.kind, auth: "password", password: f.password };
  if (f.kind === "imap") {
    if (f.imapHost.trim()) {
      req.imap_host = f.imapHost.trim();
      req.imap_port = port(f.imapPort);
    }
    if (f.smtpHost.trim()) {
      req.smtp_host = f.smtpHost.trim();
      req.smtp_port = port(f.smtpPort);
      req.smtp_security = f.smtpPort.trim() === "587" ? "starttls" : "tls";
    }
  }
  return req;
}

export interface MailAccount {
  id: string;
  name?: string;
  email: string;
  provider?: string;
  auth?: string;
  imap_host?: string;
  status?: string;
  error?: string;
}

/** The line under an account's address: its error, how it signs in, or its server. */
export function mailAccountLine(a: MailAccount): string {
  if (a.status === "error") return a.error || "连不上";
  if (a.auth === "oauth2") return "Gmail · Google 账号登录";
  if (a.provider === "feishu") return "飞书邮箱";
  return a.imap_host ? `IMAP · ${a.imap_host}` : "IMAP";
}

/** 连接器's summary of the accounts: "2 个账户", "2 个 · 有错误", "未添加". */
export function mailSummary(accounts: MailAccount[] | null): string {
  if (!accounts) return "";
  if (!accounts.length) return "未添加";
  if (accounts.some((a) => a.status === "error")) return `${accounts.length} 个 · 有错误`;
  return `${accounts.length} 个账户`;
}
