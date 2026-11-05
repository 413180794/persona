// Where picked words sit in the transcript (#2437): a selection becomes a TextAnchor (the message it is in and the
// offsets in that message's text), and an anchor becomes a Range again after the transcript re-rendered, so the spot
// can be lit and numbered while its comment waits in the composer. A message's text is its text nodes joined, which is
// stable across renders as long as the words are the same; when they moved the words are looked for near where they were.
import type { TextAnchor } from "./comments";

const ITEM = "[data-items]";
/** How many characters before the words are kept to tell the same words apart when the text moved. */
const BEFORE = 16;

function textNodes(root: Node): Text[] {
  const out: Text[] = [];
  const walk = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let n = walk.nextNode(); n; n = walk.nextNode()) out.push(n as Text);
  return out;
}

/** The text of an element as the anchors count it. */
export function textOf(root: Node): string {
  return textNodes(root)
    .map((n) => n.data)
    .join("");
}

/** Where (node, offset) is in the root's text; -1 when it is outside the root. */
export function offsetIn(root: Node, node: Node, offset: number): number {
  if (!root.contains(node)) return -1;
  const pre = document.createRange();
  pre.selectNodeContents(root);
  pre.setEnd(node, offset);
  return textOf(pre.cloneContents()).length;
}

/** The anchor of a selection inside one message of the transcript; null for words across messages or outside any. */
export function anchorOf(range: Range): TextAnchor | null {
  const from = range.startContainer instanceof Element ? range.startContainer : range.startContainer.parentElement;
  const root = from?.closest<HTMLElement>(ITEM);
  if (!root || !root.contains(range.endContainer)) return null;
  const start = offsetIn(root, range.startContainer, range.startOffset);
  const end = offsetIn(root, range.endContainer, range.endOffset);
  if (start < 0 || end <= start) return null;
  const item = (root.dataset.items ?? "").split(/\s+/)[0];
  if (!item) return null;
  const text = textOf(root);
  return { item, start, end, exact: text.slice(start, end), before: text.slice(Math.max(0, start - BEFORE), start) };
}

/** The offset of `exact` in `text` for an anchor that was at `near` with `before` in front of it: the matches that still
 *  have the same words in front come first, then the one nearest to where it was; -1 when it is not there. */
export function nearest(text: string, exact: string, near: number, before = ""): number {
  if (!exact) return -1;
  let best = -1;
  let bestSame = false;
  for (let at = text.indexOf(exact); at >= 0; at = text.indexOf(exact, at + 1)) {
    const same = Boolean(before) && text.slice(Math.max(0, at - before.length), at) === before;
    if (best < 0 || (same && !bestSame) || (same === bestSame && Math.abs(at - near) < Math.abs(best - near))) {
      best = at;
      bestSame = same;
    }
  }
  return best;
}

/** The anchor's words as a Range in the message under `scope`; null when the message or the words are not there. */
export function rangeFor(scope: ParentNode, a: TextAnchor): Range | null {
  const root = [...scope.querySelectorAll<HTMLElement>(ITEM)].find((el) => (el.dataset.items ?? "").split(/\s+/).includes(a.item));
  if (!root) return null;
  const nodes = textNodes(root);
  const text = nodes.map((n) => n.data).join("");
  const start = text.slice(a.start, a.end) === a.exact ? a.start : nearest(text, a.exact, a.start, a.before);
  if (start < 0) return null;
  const end = start + a.exact.length;
  const range = document.createRange();
  let seen = 0;
  let began = false;
  for (const n of nodes) {
    const next = seen + n.data.length;
    if (!began && start < next) {
      range.setStart(n, start - seen);
      began = true;
    }
    if (began && end <= next) {
      range.setEnd(n, end - seen);
      return range;
    }
    seen = next;
  }
  return null;
}

// ── going back to a comment's spot ───────────────────────

type Jumper = (id: string) => boolean;
const jumpers = new Set<Jumper>();

/** The transcript says it can scroll to a comment's words (while it is mounted). Returns the way to take it back. */
export function onJumpToComment(fn: Jumper): () => void {
  jumpers.add(fn);
  return () => {
    jumpers.delete(fn);
  };
}

/** Scrolls the transcript to the words of the comment with this id; false when no transcript shows them. */
export function jumpToComment(id: string): boolean {
  for (const j of jumpers) if (j(id)) return true;
  return false;
}
