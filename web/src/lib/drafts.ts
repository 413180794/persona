// Changes to a composer's draft from outside it: the message menu's 引用 / 编辑, a queued message pulled back, an
// edited image put in for the next message (iOS: ChatView.bindMenu, AppModel.editQueued, ImageViewer's attach).
// The composer for that draft key listens; with none mounted, text goes to the stored draft (it is read on mount).

export interface DraftImage {
  name: string;
  mime: string;
  data: string; // base64
}

export type DraftEvent =
  | { type: "quote"; text: string }
  | { type: "replace"; text: string }
  | { type: "append"; text: string }
  | { type: "image"; image: DraftImage };

/** "> " in front of every line, then a blank line, then what was already typed (ChatView.swift bindMenu). */
export function withQuote(draft: string, text: string): string {
  const quoted = text.split("\n").map((l) => `> ${l}`).join("\n");
  return `${quoted}\n\n${draft}`;
}

/** A pulled-back queued message goes under what is typed, on its own line (AppModel.editQueued). */
export function withAppended(draft: string, text: string): string {
  return draft ? `${draft}\n${text}` : text;
}

/** The draft after a text event (an image changes the attachments, not the text). */
export function applyDraft(draft: string, e: DraftEvent): string {
  switch (e.type) {
    case "quote":
      return withQuote(draft, e.text);
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
  const stored = `draft:${key}`;
  const next = applyDraft(localStorage.getItem(stored) ?? "", e);
  if (next) localStorage.setItem(stored, next);
  else localStorage.removeItem(stored);
  return true;
}
