// 设置 › Claude Code / Codex 设置 (the node's whitelist of ~/.claude/settings.json and ~/.codex/config.toml keys,
// iOS ClaudeSettingsView) and 设置 › 连接器 › 邮箱's add form (iOS MailAccountForm, docs/mail.md): the pure part.
import { t } from "../i18n";
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
    footer: t("lib-agent-settings.claude_footer"),
  },
  codex: {
    name: "Codex",
    path: "codex-settings",
    file: "~/.codex/config.toml",
    footer: t("lib-agent-settings.codex_footer"),
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
  const v = input.trim();
  return v ? v : null;
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
  if (!failed.length) return { ok: true, text: results.length > 1 ? t("lib-agent-settings.applied_to", { count: results.length }) : t("lib-agent-settings.saved") };
  const done = results.length - failed.length;
  const why = failed.map((r) => t("lib-agent-settings.failure_item", { name: r.name, error: r.error ?? "" })).join(t("lib-agent-settings.failure_separator"));
  return { ok: false, text: done ? t("lib-agent-settings.partly_applied", { done, failed: failed.length, why }) : t("lib-agent-settings.not_applied", { why }) };
}

/** How a value reads in a message: 开 / 关, the option, or 默认. */
export function valueText(v: SettingValue): string {
  if (v === null || v === undefined || v === "") return t("lib-agent-settings.value_default");
  if (typeof v === "boolean") return v ? t("lib-agent-settings.value_on") : t("lib-agent-settings.value_off");
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
  if (d.default) parts.push(t("lib-agent-settings.dir_default"));
  else if (d.source === "config") parts.push(t("lib-agent-settings.dir_config"));
  else parts.push(t("lib-agent-settings.dir_found"));
  if (d.sessions) parts.push(t("lib-agent-settings.dir_sessions", { count: d.sessions }));
  if (d.running) parts.push(t("lib-agent-settings.dir_running", { count: d.running }));
  if (d.blocked && d.blocked_by && !samePath(d.blocked_by, d.path, home)) parts.push(t("lib-agent-settings.dir_blocked_by", { rule: d.blocked_by }));
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
  if (!p) return t("lib-agent-settings.pattern_empty");
  if (!p.startsWith("/") && !p.startsWith("~/")) return t("lib-agent-settings.pattern_prefix");
  if (p === "~" || p === "/") return t("lib-agent-settings.pattern_root");
  if (existing.includes(p)) return t("lib-agent-settings.pattern_exists");
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

export type MailKind = "gmail" | "outlook" | "feishu" | "imap";

/** The kinds, in the phone's order (iOS MailAccountForm.Kind). Gmail and Outlook sign in with Google / Microsoft. */
export const MAIL_KINDS: { k: MailKind; label: string }[] = [
  { k: "gmail", label: "Gmail" },
  { k: "outlook", label: "Outlook" },
  { k: "feishu", label: t("lib-agent-settings.mail_kind_feishu") },
  { k: "imap", label: t("lib-agent-settings.mail_kind_imap") },
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

/** Gmail and Outlook sign in on a provider's page that calls back into the phone app (a custom scheme the browser can't
 *  catch), so they are added on the phone; the web only sets Outlook's client ID up. */
export function signsInOnPhone(k: MailKind): k is "gmail" | "outlook" {
  return k === "gmail" || k === "outlook";
}

/** What is wrong with the form, field by field (empty = can be sent). Gmail and Outlook are never sent from here. */
export function mailErrors(f: MailForm): Partial<Record<keyof MailForm, string>> {
  const e: Partial<Record<keyof MailForm, string>> = {};
  if (f.kind === "gmail") return { kind: t("lib-agent-settings.mail_gmail_phone_only") };
  if (f.kind === "outlook") return { kind: t("lib-agent-settings.mail_outlook_phone_only") };
  if (!f.email.trim()) e.email = t("lib-agent-settings.mail_email_empty");
  else if (!EMAIL.test(f.email.trim())) e.email = t("lib-agent-settings.mail_email_invalid");
  if (!f.password) e.password = f.kind === "feishu" ? t("lib-agent-settings.mail_password_feishu") : t("lib-agent-settings.mail_password_empty");
  if (f.kind === "imap") {
    if (f.imapHost.trim() && !port(f.imapPort)) e.imapPort = t("lib-agent-settings.mail_port_invalid");
    if (f.smtpHost.trim() && !port(f.smtpPort)) e.smtpPort = t("lib-agent-settings.mail_port_invalid");
  }
  return e;
}

/** The request for a password account; null for Gmail and Outlook (Google / Microsoft sign-in, on the phone). Servers left empty are guessed
 * by the hub from the domain; 飞书's are fixed by the hub. SMTP on 587 is STARTTLS, anything else TLS. */
export function mailRequest(f: MailForm): AddMailAccount | null {
  if (signsInOnPhone(f.kind)) return null;
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
  if (a.status === "error") return a.error || t("lib-agent-settings.mail_unreachable");
  if (a.auth === "oauth2") return a.provider === "outlook" ? t("lib-agent-settings.mail_line_outlook") : t("lib-agent-settings.mail_line_gmail");
  if (a.provider === "feishu") return t("lib-agent-settings.mail_line_feishu");
  return a.imap_host ? `IMAP · ${a.imap_host}` : "IMAP";
}

/** The badge after an account's address: its provider's name for the ones signed in with a provider (iOS MailProviderMark). */
export function mailProviderBadge(a: MailAccount): string | null {
  if (a.provider === "gmail") return "Gmail";
  if (a.provider === "outlook") return "Outlook";
  if (a.provider === "feishu") return t("lib-agent-settings.mail_kind_feishu");
  return null;
}

/** GET / PUT /api/v1/mail/oauth/microsoft/config (docs/mail.md「Outlook 接入方式」): whether Outlook sign-in can start,
 *  with which client ID (not a secret) from where (app | config), and the redirect URI to register in Microsoft Entra. */
export interface MicrosoftConfig {
  configured: boolean;
  client_id?: string;
  source?: string;
  redirect_uri: string;
}

const CLIENT_ID = /^[0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$/;

/** What is wrong with a typed client ID ("" = fine), the same rule as the hub's. */
export function clientIDError(id: string): string {
  const v = id.trim();
  if (!v) return t("lib-agent-settings.mail_client_id_empty");
  return CLIENT_ID.test(v) ? "" : t("lib-agent-settings.mail_client_id_invalid");
}

/** 连接器's summary of the accounts: "2 个账户", "2 个 · 有错误", "未添加". */
export function mailSummary(accounts: MailAccount[] | null): string {
  if (!accounts) return "";
  if (!accounts.length) return t("lib-agent-settings.mail_none");
  if (accounts.some((a) => a.status === "error")) return t("lib-agent-settings.mail_with_error", { count: accounts.length });
  return t("lib-agent-settings.mail_accounts", { count: accounts.length });
}
