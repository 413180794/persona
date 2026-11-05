// Structural sharing for the chat's lines: chatLines (turns.ts) builds every line, segment and step afresh on each
// push, so the rows' memo never held and one new item re-rendered every finished exchange. A line that comes out the
// same as last time keeps last time's object; only the lines that really changed reach React as new props.

import type { ChatLine } from "./turns";

/** Whether two values hold the same thing: the same object, or arrays / plain objects / Maps / Sets whose parts are
 *  the same, all the way down (an Item pushed again as a new object with the same fields counts as the same).
 *  Compared after chatLines returns, so what it fills in place while building (a segment's `start`, a message's
 *  text joined from several blocks) is part of the comparison. */
export function sameValue(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (typeof a !== "object" || typeof b !== "object" || a === null || b === null) return false;
  if (Array.isArray(a)) {
    if (!Array.isArray(b) || a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) if (!sameValue(a[i], b[i])) return false;
    return true;
  }
  if (Array.isArray(b)) return false;
  if (a instanceof Map) {
    if (!(b instanceof Map) || a.size !== b.size) return false;
    for (const [k, v] of a) if (!b.has(k) || !sameValue(v, b.get(k))) return false;
    return true;
  }
  if (a instanceof Set) {
    if (!(b instanceof Set) || a.size !== b.size) return false;
    for (const v of a) if (!b.has(v)) return false;
    return true;
  }
  const pa = Object.getPrototypeOf(a);
  if ((pa !== Object.prototype && pa !== null) || pa !== Object.getPrototypeOf(b)) return false;
  const ka = Object.keys(a);
  if (ka.length !== Object.keys(b).length) return false;
  for (const k of ka) {
    if (!Object.prototype.hasOwnProperty.call(b, k)) return false;
    if (!sameValue((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k])) return false;
  }
  return true;
}

/** `next`, with each line that is the same as the line of that id in `prev` replaced by `prev`'s object; `prev`
 *  itself when nothing changed at all. */
export function stabilizeLines(prev: readonly ChatLine[] | undefined, next: ChatLine[]): ChatLine[] {
  if (!prev?.length) return next;
  const before = new Map(prev.map((l) => [l.id, l]));
  let all = prev.length === next.length;
  const out = next.map((l, i) => {
    const old = before.get(l.id);
    const kept = old && sameValue(old, l) ? old : l;
    if (kept !== prev[i]) all = false;
    return kept;
  });
  return all ? (prev as ChatLine[]) : out;
}

/** The previous value when the next one is the same (sameValue): for what the chat hands its rows through context. */
export function stableValue<T>(prev: T | undefined, next: T): T {
  return prev !== undefined && sameValue(prev, next) ? prev : next;
}
