// Messages other pages send for a conversation (问 Persona from 知识库, 交给 AI from 收件箱, a new Persona conversation):
// the page moves to the conversation at once and the message shows there as a sending bubble until the transcript has
// it, as the app's outbox does (AppModel.sendNow / askPersona). A failed one stays with the reason, 重试 and 删除.
// The conversation's own composer keeps its local sending bubbles (ChatView); both look the same.
import { api, ApiError, errorText, sessionPath } from "./api";
import { pickedLauncher, conversationKey } from "./assistant-chat";
import { uuid } from "./format";
import { navigate } from "./router";
import { getState, refreshAssistant, setOutbox } from "./store";
import { parseUserText, type UserMessage } from "./turns";
import type { Item, Session } from "./types";

export interface OutboxImage {
  mime: string;
  data: string;
}

export interface OutboxEntry {
  /** The client_msg_id: a retry sends the same one, so the hub takes it once. */
  id: string;
  text: string;
  images: OutboxImage[];
  at: number;
  /** sent: the hub has it (or it started the conversation); the bubble waits for the transcript. */
  state: "sending" | "sent" | "failed";
  error?: string;
  /** The computer's input box holds text (input_draft): what 清空并发送 clears, when it can. */
  clearable?: string;
  /** The hub could not tell whether it went in: a retry is a new message. */
  newId?: boolean;
  launcher?: string;
}

/** How long a sent message waits for the transcript before its bubble goes (the transcript may word it differently). */
export const SENT_GRACE_MS = 60_000;

export function outboxKey(mid: string, sid: string) {
  return conversationKey(mid, sid);
}

/** The bubble an entry shows as: contexts as quote cards, the images, like the transcript will. */
export function outboxMessage(e: OutboxEntry): UserMessage {
  return {
    id: `outbox:${e.id}`,
    ...parseUserText(e.text),
    images: e.images.map((image, i): Item => ({ id: `outbox:${e.id}#${i}`, seq: 0, role: "user", kind: "image", image })),
  };
}

/** The transcript has this message: one of its latest user messages says the same, with at least as many images. */
export function arrived(e: Pick<OutboxEntry, "text" | "images">, users: { text: string; images: unknown[] }[]): boolean {
  const text = parseUserText(e.text).text;
  return users.some((u) => u.text === text && u.images.length >= e.images.length);
}

/** What stays once the transcript moved: arrived ones go, and sent ones past the grace; sending and failed stay. */
export function settle(list: OutboxEntry[], users: { text: string; images: unknown[] }[], now: number): OutboxEntry[] {
  return list.filter((e) => e.state !== "sent" ? !(e.state === "sending" && arrived(e, users)) : !arrived(e, users) && now - e.at < SENT_GRACE_MS);
}

/** Why it didn't go, in words. */
export function failureText(e: unknown): string {
  if (e instanceof ApiError && e.code === "input_draft") return "电脑上的输入框里有没发出去的字";
  if (e instanceof ApiError && e.code === "dialog_open") return "电脑上开着一个对话框，先处理它再发";
  if (e instanceof ApiError && e.body.delivered === "unknown") return "不确定送到没有：先看一眼对话再决定要不要重发";
  return errorText(e);
}

// ── the store's side ────────────────────────────────────

function list(key: string): OutboxEntry[] {
  return getState().outbox[key] ?? [];
}

function update(key: string, id: string, patch: Partial<OutboxEntry>) {
  setOutbox(key, list(key).map((e) => (e.id === id ? { ...e, ...patch } : e)));
}

/** Drops what the transcript now has (ChatView calls it with the latest user messages). */
export function settleOutbox(mid: string, sid: string, users: { text: string; images: unknown[] }[]) {
  const key = outboxKey(mid, sid);
  const before = list(key);
  if (!before.length) return;
  const after = settle(before, users, Date.now());
  if (after.length !== before.length) setOutbox(key, after);
}

async function deliver(mid: string, sid: string, e: OutboxEntry, clearDraft?: string): Promise<boolean> {
  const key = outboxKey(mid, sid);
  try {
    await api(`${sessionPath(mid, sid)}/messages`, {
      method: "POST",
      json: { text: e.text, images: e.images.length ? e.images : undefined, client_msg_id: e.id, launcher: e.launcher || undefined, clear_draft: clearDraft },
    });
    update(key, e.id, { state: "sent", at: Date.now(), error: undefined, clearable: undefined });
    return true;
  } catch (err) {
    update(key, e.id, {
      state: "failed",
      error: failureText(err),
      clearable: err instanceof ApiError && err.code === "input_draft" && typeof err.body.clearable === "string" ? err.body.clearable : undefined,
      newId: err instanceof ApiError && err.body.delivered === "unknown",
    });
    return false;
  }
}

/** Shows the message in that conversation and sends it; false when it didn't go (the bubble says why). */
export function sendToSession(mid: string, sid: string, out: { text: string; images?: OutboxImage[] }, opts: { launcher?: string } = {}): Promise<boolean> {
  const key = outboxKey(mid, sid);
  const e: OutboxEntry = { id: uuid(), text: out.text, images: out.images ?? [], at: Date.now(), state: "sending", launcher: opts.launcher };
  setOutbox(key, [...list(key), e]);
  return deliver(mid, sid, e);
}

/** A message that already went in another way (the prompt that started a conversation): its bubble until the transcript has it. */
export function noteSent(mid: string, sid: string, out: { text: string; images?: OutboxImage[] }) {
  const key = outboxKey(mid, sid);
  setOutbox(key, [...list(key), { id: uuid(), text: out.text, images: out.images ?? [], at: Date.now(), state: "sent" }]);
}

export function retryOutbox(mid: string, sid: string, id: string, clearDraft?: string): Promise<boolean> {
  const key = outboxKey(mid, sid);
  const e = list(key).find((x) => x.id === id);
  if (!e || e.state === "sending") return Promise.resolve(false);
  // the hub said it may have gone in: the user looked and wants it sent again, as a new message
  const next: OutboxEntry = { ...e, id: e.newId ? uuid() : e.id, newId: false, state: "sending", error: undefined, at: Date.now() };
  setOutbox(key, list(key).map((x) => (x.id === id ? next : x)));
  return deliver(mid, sid, next, clearDraft);
}

export function discardOutbox(mid: string, sid: string, id: string) {
  const key = outboxKey(mid, sid);
  setOutbox(key, list(key).filter((e) => e.id !== id));
}

/**
 * 问 Persona: the text goes to Persona's main conversation (AppModel.askPersona), which opens at once with the message
 * on its way; with no main conversation yet, this one starts it (and becomes it). Throws only when starting fails.
 */
export async function askPersona(text: string): Promise<void> {
  let a = getState().assistant;
  if (!a?.main) {
    await refreshAssistant().catch(() => {});
    a = getState().assistant;
  }
  if (a?.main) {
    navigate({ name: "jarvis", sid: a.main });
    void sendToSession(a.machine_id, a.main, { text }, { launcher: pickedLauncher(outboxKey(a.machine_id, a.main)) });
    return;
  }
  const r = await api<{ session: Session }>("/assistant/conversations", { method: "POST", json: { prompt: text, main: true } });
  noteSent(r.session.machine_id, r.session.session_id, { text });
  void refreshAssistant().catch(() => {});
  navigate({ name: "jarvis", sid: r.session.session_id });
}

// Source compatibility for integrations written before the product rename.
export const askJarvis = askPersona;
