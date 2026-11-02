// Mail beyond the inbox's own reply (docs/mail.md): writing a new mail or answering one by hand (the app's ComposeView),
// and marking a mail read once it is opened (MailDetailView). The accounts are read once per page load and shared.

import { useEffect, useState } from "react";
import { api, enc } from "./api";
import type { MailAccount } from "./inbox";

export interface MailAddress {
  name?: string;
  email?: string;
}

export interface MailMessage {
  uid: number;
  message_id?: string;
  subject?: string;
  from?: MailAddress;
  to?: MailAddress[];
  cc?: MailAddress[];
  date?: string;
  text?: string;
  html?: string;
  seen?: boolean;
  references?: string[];
  attachments?: { id?: string; filename?: string; mime?: string; size?: number }[];
}

export function addressText(a?: MailAddress): string {
  return a ? (a.name && a.email ? `${a.name} <${a.email}>` : a.email || a.name || "") : "";
}

// ── writing ──────────────────────────────────────────────

export interface ComposeDraft {
  accountID: string;
  to: string;
  cc: string;
  subject: string;
  body: string;
  inReplyTo?: string;
  references?: string[];
}

export function newDraft(accountID: string): ComposeDraft {
  return { accountID, to: "", cc: "", subject: "", body: "" };
}

/** Addresses typed into one field: separated by commas (either width), semicolons or new lines; `Name <a@b>` keeps the name. */
export function parseAddresses(s: string): MailAddress[] {
  return s
    .split(/[,，;；\n]/)
    .map((x) => x.trim())
    .filter(Boolean)
    .map((x) => {
      const m = /^(.*?)\s*<([^<>\s]+)>$/.exec(x);
      if (m) return m[1].replace(/^"|"$/g, "").trim() ? { name: m[1].replace(/^"|"$/g, "").trim(), email: m[2] } : { email: m[2] };
      return { email: x };
    });
}

const EMAIL = /^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/;

/** The addresses in a field that do not look like one (the dialog names them before sending). */
export function badAddresses(s: string): string[] {
  return parseAddresses(s)
    .map((a) => a.email ?? "")
    .filter((e) => !EMAIL.test(e));
}

function pad(n: number) {
  return String(n).padStart(2, "0");
}

/** 2026-10-28 14:05, the line above a quoted mail. */
function quoteDate(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** A reply to `msg` from the account it came to: Re: subject, In-Reply-To / References, the first 40 lines quoted
 * (ComposeDraft.reply in the app). */
export function replyDraft(accountID: string, msg: MailMessage): ComposeDraft {
  const subject = msg.subject ?? "";
  const quoted = (msg.text ?? "").split("\n").slice(0, 40).map((l) => `> ${l}`).join("\n");
  const head = [quoteDate(msg.date), addressText(msg.from)].filter(Boolean).join("，");
  return {
    accountID,
    to: msg.from?.email ?? "",
    cc: "",
    subject: /^re:/i.test(subject) ? subject : `Re: ${subject}`,
    body: `\n\n${head} 写道：\n${quoted}`,
    inReplyTo: msg.message_id || undefined,
    references: [...(msg.references ?? []), ...(msg.message_id ? [msg.message_id] : [])],
  };
}

/** POST /api/v1/mail/send's body. The hub takes References as one string of ids (mail.SendRequest). */
export function sendBody(d: ComposeDraft) {
  const refs = (d.references ?? []).filter(Boolean);
  return {
    account_id: d.accountID,
    to: parseAddresses(d.to),
    cc: parseAddresses(d.cc),
    subject: d.subject,
    text: d.body,
    ...(d.inReplyTo ? { in_reply_to: d.inReplyTo } : {}),
    ...(refs.length ? { references: refs.join(" ") } : {}),
  };
}

export async function sendMail(d: ComposeDraft): Promise<void> {
  await api("/mail/send", { method: "POST", json: sendBody(d) });
}

/** Opened: the mail is read (best effort; the inbox's own state follows on its next push). */
export function markMailSeen(accountID: string, uid: number, mailbox: string): Promise<unknown> {
  return api(`/mail/messages/${enc(accountID)}/${uid}/flags`, { method: "POST", json: { mailbox, seen: true } }).catch(() => undefined);
}

// ── accounts ─────────────────────────────────────────────

let accounts: MailAccount[] = [];
let inFlight: Promise<MailAccount[]> | null = null;

/** The hub's mail accounts, read again on every call (calls while one is out share it); the last answer is kept. */
export function loadMailAccounts(): Promise<MailAccount[]> {
  inFlight ??= api<{ accounts?: MailAccount[] }>("/mail/accounts")
    .then((r) => (accounts = r.accounts ?? []))
    .finally(() => {
      inFlight = null;
    });
  return inFlight;
}

/** The accounts, the last known ones at once and the hub's answer when it comes. */
export function useMailAccounts(): MailAccount[] {
  const [list, setList] = useState(accounts);
  useEffect(() => {
    let live = true;
    loadMailAccounts()
      .then((a) => live && setList(a))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);
  return list;
}

/** Tests only. */
export function resetMailForTests() {
  accounts = [];
  inFlight = null;
}
