// Finding a message sent from here in the transcript. Claude Code keeps its own id for the user's message (not the
// client_msg_id), so the transcript's copy is found by its words: the same rules as iOS (PendingMessage.arrived / same)
// and Android (MessageEcho).

/** The computer's clock may be behind this one by this much. */
export const CLOCK_SLACK_MS = 120_000;

/** The words the hub adds where it cut a long text for the page (`…（已截断，共 N 字节）`). */
const CUT_MARK = "\n…（已截断";

/** Comparable form: the phone's image note, pasted wrappers and Claude's "[Image #21]" placeholders removed, no whitespace. */
export function normalizeWords(s: string): string {
  return s
    .replace(/<attached_images>[\s\S]*?<\/attached_images>/g, "")
    .replace(/<[\\/]*pasted_content[^>]*>/g, "")
    .replace(/\[Image #\d+\]/g, "")
    .replace(/\s+/g, "");
}

/** The transcript's copy of a message: equal once normalized, or one containing the other when both are long enough
 *  (a pending 好 must not be taken for any message with 好 in it). */
export function sameWords(a: string, b: string): boolean {
  const x = normalizeWords(a),
    y = normalizeWords(b);
  if (!x || !y) return false;
  return x === y || (Math.min([...x].length, [...y].length) >= 8 && (x.includes(y) || y.includes(x)));
}

export interface Sent {
  /** The words as the transcript shows them (contexts already taken out, parseUserText). */
  text: string;
  images: unknown[];
  /** When it was written here (ms). */
  at: number;
}

export interface Seen {
  id?: string;
  text: string;
  images: unknown[];
  ts?: string;
}

/** `user` is the copy of `sent`: written after it (with slack for the computer's clock), with at least its images, and
 *  its words — when the page cut a long one, its start. An older message with the same words (a second 继续) is not it. */
export function isEcho(sent: Sent, user: Seen): boolean {
  const at = user.ts ? Date.parse(user.ts) : NaN;
  if (!Number.isNaN(at) && at < sent.at - CLOCK_SLACK_MS) return false;
  if (user.images.length < sent.images.length) return false;
  const cut = user.text.lastIndexOf(CUT_MARK);
  if (cut >= 0) {
    const start = normalizeWords(user.text.slice(0, cut));
    return [...start].length >= 8 && normalizeWords(sent.text).startsWith(start);
  }
  // only pictures: the transcript's copy has no words either
  if (!normalizeWords(sent.text)) return sent.images.length > 0 && !normalizeWords(user.text);
  return sameWords(user.text, sent.text);
}

/** Which of `sent` the transcript's `users` hold: each user message answers one (the oldest it can), and one already
 *  taken (`claimed`, by id, kept across calls) answers none, so the same words sent twice need two copies. */
export function echoed<T>(sent: T[], users: Seen[], describe: (t: T) => Sent, claimed: Set<string>): Set<T> {
  const left = [...sent].sort((a, b) => describe(a).at - describe(b).at);
  const found = new Set<T>();
  for (const user of users) {
    if (user.id && claimed.has(user.id)) continue;
    const i = left.findIndex((t) => isEcho(describe(t), user));
    if (i < 0) continue;
    found.add(left[i]);
    left.splice(i, 1);
    if (user.id) claimed.add(user.id);
  }
  return found;
}
