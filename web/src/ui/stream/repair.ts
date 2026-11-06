// Streaming Markdown made whole for the moment (ported from cindy's repairStreamingMarkdown): an unclosed code fence is
// closed, an unclosed ** / * at the end is closed, a half-written image or link shows as its text. Only what the
// growing end leaves open is touched, so half a delimiter doesn't flip the structure (and replay the fade) when its
// other half arrives. Only while streaming; the finished text is rendered as written.

import { closes, fenceRun, type Fence } from "./chunks";

// Whether the text ends inside a code fence (and with which), and the text outside fences.
function scanFences(md: string): { open: Fence | null; outside: string } {
  let open: Fence | null = null;
  const outside: string[] = [];
  for (const line of md.split(/\r?\n/)) {
    const run = fenceRun(line);
    if (run) open = !open ? run : closes(open, run) ? null : open;
    else if (!open) outside.push(line);
  }
  return { open, outside: outside.join("\n") };
}

function escaped(text: string, i: number): boolean {
  let n = 0;
  for (let j = i - 1; j >= 0 && text[j] === "\\"; j--) n++;
  return n % 2 === 1;
}

// A * that starts a list item: only spaces before it on its line, a space after it.
function bullet(text: string, i: number): boolean {
  if (text[i + 1] !== " " && text[i + 1] !== "\t") return false;
  for (let j = i - 1; j >= 0; j--) {
    if (text[j] === "\n") return true;
    if (text[j] !== " " && text[j] !== "\t") return false;
  }
  return true;
}

// Unescaped runs of exactly this delimiter: how many and where the last one is. A *** run pairs differently by
// context: then nothing is repaired.
function scan(text: string, delim: "**" | "*"): { count: number; last: number; mixed: boolean } {
  let count = 0;
  let last = -1;
  let mixed = false;
  for (let i = 0; i < text.length; ) {
    if (text[i] !== "*" || escaped(text, i)) {
      i++;
      continue;
    }
    let end = i;
    while (text[end] === "*") end++;
    const len = end - i;
    if (len >= 3) mixed = true;
    else if ((delim === "**" && len === 2) || (delim === "*" && len === 1 && !bullet(text, i))) {
      count++;
      last = i;
    }
    i = end;
  }
  return { count, last, mixed };
}

// Closes the last opener only when what follows it is text on the same line (not space, not a backtick).
function balance(md: string, delim: "**" | "*"): string {
  if (!md.includes(delim)) return md;
  const { count, last, mixed } = scan(md, delim);
  if (mixed || count % 2 === 0 || last < 0) return md;
  const rest = md.slice(last + delim.length);
  if (!rest.trim() || rest.includes("\n") || rest.includes("`") || /^\s/.test(rest)) return md;
  return md + delim;
}

const TABLE_DIVIDER = /^ {0,3}\|?[\t ]*:?-{3,}:?[\t ]*(?:\|[\t ]*:?-{3,}:?[\t ]*)*\|?[\t ]*$/;

/** A table's last row still being written (no line end yet, under the table's divider): left out until it is whole, so
 *  the table does not grow a cell at a time and reflow its columns with every word (#5602). */
function holdHalfRow(md: string): string {
  if (md.endsWith("\n")) return md;
  const nl = md.lastIndexOf("\n");
  if (nl < 0 || !md.slice(nl + 1).trimStart().startsWith("|")) return md;
  const lines = md.slice(0, nl).split(/\r?\n/);
  for (let i = lines.length - 1; i >= 0; i--) {
    const l = lines[i].trim();
    if (TABLE_DIVIDER.test(l) && l.includes("|")) return md.slice(0, nl + 1);
    if (!l.startsWith("|")) return md;
  }
  return md;
}

export function repairStreamingMarkdown(md: string): string {
  if (!md) return md;
  const { open, outside } = scanFences(md);
  if (open) {
    const marker = open.marker.repeat(open.length);
    return md.endsWith("\n") ? md + marker : `${md}\n${marker}`;
  }
  md = holdHalfRow(md);
  // half a code span: whatever * follows may be inside it
  let ticks = 0;
  for (let i = 0; i < outside.length; i++) if (outside[i] === "`" && !escaped(outside, i)) ticks++;
  if (ticks % 2) return md;
  let t = md;
  if (t.includes("![")) t = t.replace(/!\[([^\]\n]*)\]\([^)\n]*$/, "$1").replace(/!\[([^\]\n]*)$/, "$1");
  if (t.includes("](")) t = t.replace(/(^|[^!\\])\[([^\]\n]*)\]\([^)\n]*$/, "$1$2");
  return balance(balance(t, "**"), "*");
}
