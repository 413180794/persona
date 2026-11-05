// Streaming Markdown in chunks (ported from cindy's streamingMarkdownChunks): the text is cut at top-level blank lines,
// so the chunks before the last are sealed and keep their parse and DOM (React.memo); only the growing last one is
// parsed again at each update.

export interface Chunk {
  /** Where it starts in the text: its React key and its fade state's key. */
  start: number;
  content: string;
}

export interface Fence {
  marker: string;
  length: number;
}

// A line that continues the block above it: indented, a list item (also one with no text yet, a "2." or "-" alone on
// its line, which is still the list's next item) or a quote.
const continues = (line: string) => /^[\t ]/.test(line) || /^(?:[-+*]|\d{1,9}[.)])(?:[\t ]|$)/.test(line) || line.startsWith(">");

// The growing last line may still become a list item (a "2" before its "."): no cut before it until it says (#1966).
const mayContinue = (line: string) => /^(?:[-+*]|\d{1,9}[.)]?)$/.test(line);

/** A code fence line (``` or ~~~, up to 3 spaces in): its character, length and what follows it. */
export function fenceRun(line: string): { marker: string; length: number; rest: string } | null {
  const m = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(line);
  return m ? { marker: m[1][0], length: m[1].length, rest: m[2] } : null;
}

/** Whether a fence line closes the open fence `f`. */
export const closes = (f: Fence, run: { marker: string; length: number; rest: string }) => run.marker === f.marker && run.length >= f.length && !run.rest.trim();

/**
 * Text whose parts depend on each other across blocks stays one chunk: reference definitions ([x]: url, used above
 * them), HTML blocks. (cindy also keeps several headings together for their ids; headings here have none.)
 */
function needsWholeDocument(md: string): boolean {
  return (
    /^ {0,3}\[(?:\\[^\r\n]|[^\]\\\r\n])+\]:/m.test(md) ||
    /^ {0,3}<(?:!--|\?|!\[CDATA\[|![A-Za-z]|\/?[A-Za-z][A-Za-z0-9-]*(?:[\t />]|$))/m.test(md)
  );
}

/**
 * The sealed top-level blocks and the growing tail. A cut is a blank line outside code fences and $$ blocks whose next
 * non-blank line starts a new top-level block (not an indented, list or quote continuation).
 */
export function splitChunks(md: string): Chunk[] {
  if ((!md.includes("\n\n") && !md.includes("\r\n\r\n")) || needsWholeDocument(md)) return [{ start: 0, content: md }];
  const cuts: number[] = [];
  let pending: number | null = null;
  let fence: Fence | null = null;
  let math = false;
  for (let start = 0; start <= md.length; ) {
    const nl = md.indexOf("\n", start);
    const end = nl < 0 ? md.length : nl;
    const line = md.slice(start, end).replace(/\r$/, "");
    if (pending !== null && line.trim()) {
      if (!continues(line) && !(nl < 0 && mayContinue(line))) cuts.push(pending);
      pending = null;
    }
    const run = fenceRun(line);
    if (run) fence = !fence ? run : closes(fence, run) ? null : fence;
    // (one still open is \$\$ here: normalizeMath escapes a $$ until its closing one is written)
    else if (!fence && /^ {0,3}(?:\$\$|\\\$\\\$)[\t ]*$/.test(line)) math = !math;
    // a \[ … \] block still being written: normalizeMath makes it $$ … $$ only once its \] is there
    else if (!fence && /^ {0,3}\\\[[\t ]*$/.test(line)) math = true;
    else if (!fence && /^ {0,3}\\\][\t ]*$/.test(line)) math = false;
    if (!fence && !math && !line.trim() && nl >= 0) pending = nl + 1;
    if (nl < 0) break;
    start = nl + 1;
  }
  if (!cuts.length) return [{ start: 0, content: md }];
  const out: Chunk[] = [];
  let at = 0;
  for (const c of cuts) {
    if (c <= at || c > md.length) continue;
    out.push({ start: at, content: md.slice(at, c) });
    at = c;
  }
  out.push({ start: at, content: md.slice(at) });
  return out;
}
