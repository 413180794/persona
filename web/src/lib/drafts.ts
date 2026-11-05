// Changes to a composer's draft from outside it: the message menu's 引用 / 编辑, a queued message pulled back, an
// edited image put in for the next message (iOS: ChatView.bindMenu, AppModel.editQueued, ImageViewer's attach).
// The composer for that draft key listens; with none mounted, text goes to the stored draft (it is read on mount).
// 引用 waits as a bar above the input (not as `> ` lines in it) and goes out as the same `> ` block in front of the
// message, so what the agent reads is unchanged; a bubble draws a leading `> ` block folded (splitQuote).

export interface DraftImage {
  name: string;
  mime: string;
  data: string; // base64
}

/** What 引用 holds above the input: the quoted words, who said them, when; for a task's card, which task. */
export interface DraftQuote {
  text: string;
  from?: string;
  ts?: string;
  task?: QuotedTask;
}

/** The task a quoted card is about: its title and todo id, sent on the quote's first line so the assistant knows. */
export interface QuotedTask {
  title: string;
  id?: string;
}

/** `任务「<title>」（todo id: <id>）`: the first line of a task's quote. */
export function taskQuoteLine(t: QuotedTask): string {
  // i18n-ignore: written into the message and read back by TASK_LINE (and by the apps); not interface text
  return `任务「${t.title}」${t.id ? `（todo id: ${t.id}）` : ""}`;
}

// i18n-ignore: the shape taskQuoteLine writes
const TASK_LINE = /^任务「(.*)」(?:（todo id: ([^）\s]+)）)?$/;

/** A quote's words as sent (splitQuote's `quote`) back to what was quoted: a task's first line becomes its task. */
export function parseQuote(text: string): DraftQuote {
  const [first, ...rest] = text.split("\n");
  const m = TASK_LINE.exec(first.trim());
  if (!m) return { text };
  return { text: rest.join("\n").trim(), task: { title: m[1], ...(m[2] ? { id: m[2] } : {}) } };
}

export type DraftEvent =
  | ({ type: "quote" } & DraftQuote)
  | { type: "replace"; text: string }
  | { type: "append"; text: string }
  | { type: "image"; image: DraftImage };

/** "> " in front of every line, then a blank line, then what was already typed (ChatView.swift bindMenu). */
export function withQuote(draft: string, text: string): string {
  const quoted = text.split("\n").map((l) => `> ${l}`).join("\n");
  return `${quoted}\n\n${draft}`;
}

/** The message as sent with a quote: its block, a blank line, then what was typed (just the block when nothing was). */
export function withQuoteBlock(quote: DraftQuote | null, text: string): string {
  const words = quote?.text.trim() ?? "";
  const body = quote?.task ? [taskQuoteLine(quote.task), words].filter(Boolean).join("\n\n") : words;
  if (!body) return text;
  const block = withQuote("", body);
  return text.trim() ? block + text : block.trimEnd();
}

/** A message that starts with a `> ` block (sent with 引用, or typed): the quoted words and what follows. */
export function splitQuote(text: string): { quote: string; rest: string } | null {
  const lines = text.replace(/^\s*\n/, "").split("\n");
  let n = 0;
  while (n < lines.length && /^ {0,3}>/.test(lines[n])) n++;
  if (!n) return null;
  const quote = lines.slice(0, n).map((l) => l.replace(/^ {0,3}> ?/, "")).join("\n").trim();
  if (!quote) return null;
  return { quote, rest: lines.slice(n).join("\n").trim() };
}

const quoteKey = (key: string) => `quote:${key}`;

/** The quote kept for a composer's draft key (read on mount, like the draft). */
export function storedQuote(key: string): DraftQuote | null {
  try {
    const q = JSON.parse(localStorage.getItem(quoteKey(key)) ?? "null") as DraftQuote | null;
    return q && typeof q.text === "string" && (q.text || q.task?.title) ? q : null;
  } catch {
    return null;
  }
}

export function storeQuote(key: string, q: DraftQuote | null) {
  if (q) localStorage.setItem(quoteKey(key), JSON.stringify(q));
  else localStorage.removeItem(quoteKey(key));
}

/** A pulled-back queued message goes under what is typed, on its own line (AppModel.editQueued). */
export function withAppended(draft: string, text: string): string {
  return draft ? `${draft}\n${text}` : text;
}

/** The draft after a send failed: the composer emptied itself when the message went out, so the text comes back;
 * what was typed while it was on its way stays, under it on its own line. */
export function withUnsent(draft: string, unsent: string): string {
  if (!unsent) return draft;
  return draft ? `${unsent}\n${draft}` : unsent;
}

/** The draft after a text event (an image changes the attachments and a quote the bar above, not the text). */
export function applyDraft(draft: string, e: DraftEvent): string {
  switch (e.type) {
    case "replace":
      return e.text;
    case "append":
      return withAppended(draft, e.text);
    default:
      return draft;
  }
}

/** Replacing asks first only when something else is typed (编辑 must not silently drop a draft). */
export function replaceNeedsConfirm(draft: string, text: string): boolean {
  return draft.trim() !== "" && draft.trim() !== text.trim();
}

type Listener = (e: DraftEvent) => void;
const listeners = new Map<string, Set<Listener>>();

/** A composer listens for its draft key; returns the unsubscribe. */
export function onDraftEvent(key: string, l: Listener): () => void {
  if (!listeners.has(key)) listeners.set(key, new Set());
  listeners.get(key)!.add(l);
  return () => {
    listeners.get(key)?.delete(l);
  };
}

/** Sends an event to the composer of `key`. False when none is mounted and it could not be kept (an image). */
export function sendDraft(key: string, e: DraftEvent): boolean {
  const ls = listeners.get(key);
  if (ls?.size) {
    ls.forEach((l) => l(e));
    return true;
  }
  if (e.type === "image") return false;
  if (e.type === "quote") {
    storeQuote(key, { text: e.text, from: e.from, ts: e.ts, task: e.task });
    return true;
  }
  const stored = `draft:${key}`;
  const next = applyDraft(localStorage.getItem(stored) ?? "", e);
  if (next) localStorage.setItem(stored, next);
  else localStorage.removeItem(stored);
  return true;
}
