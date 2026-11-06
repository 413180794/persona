// Terminal rows the page draws itself (not xterm): pages of herdr's history above the screen, and the screen folded at
// the panel's width ("Wrap long lines") while watching. A row is runs of characters with one style each, from the ANSI text herdr
// reads or from xterm's grid; how a long row folds follows iOS TerminalWrap (hanging indent, what only pads a row).

/** A color: an index into the 256-color palette, or "#rrggbb". */
export type TermColor = number | string;

export interface CellStyle {
  fg?: TermColor;
  bg?: TermColor;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
  inverse?: boolean;
  strike?: boolean;
  invisible?: boolean;
}

export interface Run {
  text: string;
  /** Cells it takes (wide characters two). */
  cells: number;
  style: CellStyle;
}

export interface CellRow {
  runs: Run[];
}

// ── widths ──────────────────────────────────────────────

const ZERO: [number, number][] = [
  [0x0300, 0x036f], [0x0483, 0x0489], [0x0591, 0x05bd], [0x0610, 0x061a], [0x064b, 0x065f], [0x1ab0, 0x1aff],
  [0x1dc0, 0x1dff], [0x200b, 0x200f], [0x202a, 0x202e], [0x2060, 0x2064], [0x20d0, 0x20ff], [0xfe00, 0xfe0f],
  [0xfe20, 0xfe2f], [0xfeff, 0xfeff], [0xe0100, 0xe01ef],
];

const WIDE: [number, number][] = [
  [0x1100, 0x115f], [0x231a, 0x231b], [0x2329, 0x232a], [0x23e9, 0x23ec], [0x23f0, 0x23f0], [0x23f3, 0x23f3],
  [0x25fd, 0x25fe], [0x2614, 0x2615], [0x2648, 0x2653], [0x267f, 0x267f], [0x2693, 0x2693], [0x26a1, 0x26a1],
  [0x26aa, 0x26ab], [0x26bd, 0x26be], [0x26c4, 0x26c5], [0x26ce, 0x26ce], [0x26d4, 0x26d4], [0x26ea, 0x26ea],
  [0x26f2, 0x26f3], [0x26f5, 0x26f5], [0x26fa, 0x26fa], [0x26fd, 0x26fd], [0x2705, 0x2705], [0x270a, 0x270b],
  [0x2728, 0x2728], [0x274c, 0x274c], [0x274e, 0x274e], [0x2753, 0x2755], [0x2757, 0x2757], [0x2795, 0x2797],
  [0x27b0, 0x27b0], [0x27bf, 0x27bf], [0x2b1b, 0x2b1c], [0x2b50, 0x2b50], [0x2b55, 0x2b55], [0x2e80, 0x303e],
  [0x3041, 0x33ff], [0x3400, 0x4dbf], [0x4e00, 0x9fff], [0xa000, 0xa4cf], [0xa960, 0xa97f], [0xac00, 0xd7a3],
  [0xf900, 0xfaff], [0xfe10, 0xfe19], [0xfe30, 0xfe6f], [0xff00, 0xff60], [0xffe0, 0xffe6], [0x16fe0, 0x16fe4],
  [0x17000, 0x18cff], [0x1b000, 0x1b2ff], [0x1f004, 0x1f004], [0x1f0cf, 0x1f0cf], [0x1f18e, 0x1f18e],
  [0x1f191, 0x1f19a], [0x1f200, 0x1f251], [0x1f300, 0x1f320], [0x1f32d, 0x1f335], [0x1f337, 0x1f37c],
  [0x1f37e, 0x1f393], [0x1f3a0, 0x1f3ca], [0x1f3cf, 0x1f3d3], [0x1f3e0, 0x1f3f0], [0x1f3f4, 0x1f3f4],
  [0x1f3f8, 0x1f43e], [0x1f440, 0x1f440], [0x1f442, 0x1f4fc], [0x1f4ff, 0x1f53d], [0x1f54b, 0x1f54e],
  [0x1f550, 0x1f567], [0x1f57a, 0x1f57a], [0x1f595, 0x1f596], [0x1f5a4, 0x1f5a4], [0x1f5fb, 0x1f64f],
  [0x1f680, 0x1f6c5], [0x1f6cc, 0x1f6cc], [0x1f6d0, 0x1f6d2], [0x1f6d5, 0x1f6d7], [0x1f6eb, 0x1f6ec],
  [0x1f6f4, 0x1f6fc], [0x1f7e0, 0x1f7eb], [0x1f90c, 0x1f93a], [0x1f93c, 0x1f945], [0x1f947, 0x1f9ff],
  [0x1fa70, 0x1faff], [0x20000, 0x2fffd], [0x30000, 0x3fffd],
];

function within(cp: number, table: [number, number][]): boolean {
  let lo = 0, hi = table.length - 1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (cp < table[mid][0]) hi = mid - 1;
    else if (cp > table[mid][1]) lo = mid + 1;
    else return true;
  }
  return false;
}

/** Cells a character takes on the terminal: 0 for combining marks, 2 for East Asian wide ones and most emoji. */
export function charWidth(cp: number): number {
  if (cp < 0x300) return cp < 0x20 || (cp >= 0x7f && cp < 0xa0) ? 0 : 1;
  if (within(cp, ZERO)) return 0;
  return within(cp, WIDE) ? 2 : 1;
}

/** Cells a string takes. */
export function textCells(s: string): number {
  let n = 0;
  for (const ch of s) n += charWidth(ch.codePointAt(0)!);
  return n;
}

// ── ANSI text → rows ────────────────────────────────────

const sameStyle = (a: CellStyle, b: CellStyle) =>
  a.fg === b.fg && a.bg === b.bg && !a.bold === !b.bold && !a.dim === !b.dim && !a.italic === !b.italic &&
  !a.underline === !b.underline && !a.inverse === !b.inverse && !a.strike === !b.strike && !a.invisible === !b.invisible;

/** Adds text in a style to a row, joining the last run when the style is the same. */
export function pushText(row: CellRow, text: string, style: CellStyle, cells = textCells(text)) {
  if (!text) return;
  const last = row.runs[row.runs.length - 1];
  if (last && sameStyle(last.style, style)) {
    last.text += text;
    last.cells += cells;
  } else {
    row.runs.push({ text, cells, style: { ...style } });
  }
}

/** The extended color after 38 / 48 (`5;n` or `2;r;g;b`); how many parameters it used. */
function extendedColor(p: number[], i: number): [TermColor | undefined, number] {
  if (p[i + 1] === 5 && p[i + 2] !== undefined) return [Math.max(0, Math.min(255, p[i + 2])), 2];
  if (p[i + 1] === 2 && p[i + 4] !== undefined) {
    const hex = [p[i + 2], p[i + 3], p[i + 4]].map((v) => Math.max(0, Math.min(255, v)).toString(16).padStart(2, "0")).join("");
    return [`#${hex}`, 4];
  }
  return [undefined, 0];
}

/** Applies one SGR sequence's parameters to a style. */
export function applySGR(style: CellStyle, params: string): CellStyle {
  const s: CellStyle = { ...style };
  // 38:2::r:g:b (colon form, with an empty color space) reads the same as 38;2;r;g;b
  const p = (params === "" ? "0" : params)
    .split(";")
    .flatMap((part) => (part.includes(":") ? part.split(":").filter((x, j, all) => !(x === "" && j === 2 && all[1] === "2")) : [part]))
    .map((x) => (x === "" ? 0 : Number(x)));
  for (let i = 0; i < p.length; i++) {
    const n = p[i];
    if (n === 0) {
      for (const k of Object.keys(s)) delete (s as Record<string, unknown>)[k];
    } else if (n === 1) s.bold = true;
    else if (n === 2) s.dim = true;
    else if (n === 3) s.italic = true;
    else if (n === 4) s.underline = true;
    else if (n === 7) s.inverse = true;
    else if (n === 8) s.invisible = true;
    else if (n === 9) s.strike = true;
    else if (n === 21 || n === 22) {
      delete s.bold;
      delete s.dim;
    } else if (n === 23) delete s.italic;
    else if (n === 24) delete s.underline;
    else if (n === 27) delete s.inverse;
    else if (n === 28) delete s.invisible;
    else if (n === 29) delete s.strike;
    else if (n >= 30 && n <= 37) s.fg = n - 30;
    else if (n === 39) delete s.fg;
    else if (n >= 40 && n <= 47) s.bg = n - 40;
    else if (n === 49) delete s.bg;
    else if (n >= 90 && n <= 97) s.fg = n - 90 + 8;
    else if (n >= 100 && n <= 107) s.bg = n - 100 + 8;
    else if (n === 38 || n === 48) {
      const [c, used] = extendedColor(p, i);
      if (c !== undefined) {
        if (n === 38) s.fg = c;
        else s.bg = c;
      }
      i += used;
    }
  }
  return s;
}

/**
 * herdr's colored read (SGR escapes, rows separated by newlines) as rows. The style carries over from row to row, as
 * on a terminal; other escapes (cursor moves, OSC titles and links) are dropped, tabs go to the next multiple of 8.
 */
export function parseAnsiRows(text: string): CellRow[] {
  const rows: CellRow[] = [];
  let row: CellRow = { runs: [] };
  let cells = 0;
  let style: CellStyle = {};
  let buf = "";
  const flush = () => {
    if (buf) pushText(row, buf, style);
    buf = "";
  };
  const lines = text.replace(/\r\n/g, "\n").replace(/\r/g, "").split("\n");
  for (let li = 0; li < lines.length; li++) {
    const line = lines[li];
    for (let i = 0; i < line.length; ) {
      const c = line[i];
      if (c === "\x1b") {
        flush();
        const next = line[i + 1];
        if (next === "[") {
          let j = i + 2;
          while (j < line.length && !/[\x40-\x7e]/.test(line[j])) j++;
          if (line[j] === "m") style = applySGR(style, line.slice(i + 2, j));
          i = j + 1;
        } else if (next === "]") {
          // OSC: up to BEL or ST
          let j = i + 2;
          while (j < line.length && line[j] !== "\x07" && !(line[j] === "\x1b" && line[j + 1] === "\\")) j++;
          i = line[j] === "\x07" ? j + 1 : j + 2;
        } else {
          i += 2;
        }
        continue;
      }
      if (c === "\t") {
        const n = 8 - (cells % 8);
        buf += " ".repeat(n);
        cells += n;
        i++;
        continue;
      }
      const cp = line.codePointAt(i)!;
      const ch = String.fromCodePoint(cp);
      i += ch.length;
      if (cp < 0x20 || cp === 0x7f) continue;
      buf += ch;
      cells += charWidth(cp);
    }
    flush();
    rows.push(row);
    row = { runs: [] };
    cells = 0;
  }
  return rows;
}

/** A row's text, trailing spaces trimmed (what herdr's plain read has). */
export function plainRow(row: CellRow): string {
  return row.runs.map((r) => r.text).join("").replace(/\s+$/, "");
}

// ── xterm's grid → rows ─────────────────────────────────

/** The part of xterm's IBufferCell read here (so tests can fake it). */
export interface GridCell {
  getWidth(): number;
  getChars(): string;
  isFgDefault(): boolean;
  isFgRGB(): boolean;
  getFgColor(): number;
  isBgDefault(): boolean;
  isBgRGB(): boolean;
  getBgColor(): number;
  isBold(): number;
  isDim(): number;
  isItalic(): number;
  isUnderline(): number;
  isInverse(): number;
  isInvisible(): number;
  isStrikethrough(): number;
}

const rgb = (v: number) => `#${(v & 0xffffff).toString(16).padStart(6, "0")}`;

/** One row of xterm's grid, `cell(x)` for each column (wide characters take two, the second has width 0). */
export function gridRow(cols: number, cell: (x: number) => GridCell | undefined): CellRow {
  const row: CellRow = { runs: [] };
  for (let x = 0; x < cols; x++) {
    const c = cell(x);
    if (!c) break;
    const w = c.getWidth();
    if (w === 0) continue;
    const style: CellStyle = {};
    if (!c.isFgDefault()) style.fg = c.isFgRGB() ? rgb(c.getFgColor()) : c.getFgColor();
    if (!c.isBgDefault()) style.bg = c.isBgRGB() ? rgb(c.getBgColor()) : c.getBgColor();
    if (c.isBold()) style.bold = true;
    if (c.isDim()) style.dim = true;
    if (c.isItalic()) style.italic = true;
    if (c.isUnderline()) style.underline = true;
    if (c.isInverse()) style.inverse = true;
    if (c.isInvisible()) style.invisible = true;
    if (c.isStrikethrough()) style.strike = true;
    pushText(row, c.getChars() || " ", style, w);
  }
  return row;
}

// ── folding (iOS TerminalWrap) ──────────────────────────

const BORDERS = new Set(["│", "┃", "║", "▏", "▎"]);
const MARKERS = new Set(["⏺", "⎿", "❯", "›", ">", "•", "·", "-", "*", "+", "✻", "✶", "✳", "◦", "▸", "▪", "☐", "☒", "✔", "✘"]);
const RULES = new Set([0x2500, 0x2501, 0x2504, 0x2505, 0x2508, 0x2509, 0x254c, 0x254d, 0x2550]);

interface Ch {
  ch: string;
  cells: number;
  style: CellStyle;
}

function chars(row: CellRow): Ch[] {
  const out: Ch[] = [];
  for (const r of row.runs) for (const ch of r.text) out.push({ ch, cells: charWidth(ch.codePointAt(0)!), style: r.style });
  return out;
}

const isRule = (ch: string) => [...ch].length === 1 && RULES.has(ch.codePointAt(0)!);
/** Only pads a row: a space (whatever its background) or a rule. */
const isFiller = (c: Ch) => c.ch === " " || isRule(c.ch);
/** Shows nothing: a space with no background. */
const isBlank = (c: Ch) => c.ch === " " && c.style.bg === undefined && !c.style.inverse;

function rowOf(list: Ch[]): CellRow {
  const row: CellRow = { runs: [] };
  for (const c of list) pushText(row, c.ch, c.style, c.cells);
  return row;
}

export interface FoldedRow {
  /** What folds at the panel's width. */
  body: CellRow;
  /** What only pads the row to the pane's width (spaces with a background, a rule): drawn up to the panel's edge, never folded. */
  tail: CellRow;
  /** Cells the continuation lines hang under (after the row's indent and list marker). */
  indent: number;
}

/**
 * A row cut for folding: its text, then what only pads it to the pane's width (iOS: "ends at the phone's edge instead of
 * folding into lines of nothing"); blank cells at the end go, not past the cursor, which is drawn as an inverted cell.
 */
export function foldRow(row: CellRow, cursorCol?: number): FoldedRow {
  const list = chars(row);
  let end = list.length;
  // the cursor's column is in cells; find the character index it falls on
  let cursorIndex: number | undefined;
  if (cursorCol !== undefined) {
    let at = 0;
    cursorIndex = list.length;
    for (let i = 0; i < list.length; i++) {
      if (at >= cursorCol) {
        cursorIndex = i;
        break;
      }
      at += list[i].cells;
    }
    if (cursorIndex === list.length && at < cursorCol) cursorIndex = list.length + (cursorCol - at);
  }
  while (end > 0 && isBlank(list[end - 1])) end--;
  let bodyEnd = end;
  while (bodyEnd > 0 && isFiller(list[bodyEnd - 1])) bodyEnd--;
  if (cursorIndex !== undefined) bodyEnd = Math.max(bodyEnd, Math.min(end, cursorIndex + 1));
  const body = list.slice(0, bodyEnd);
  if (cursorIndex !== undefined) {
    for (let i = body.length; i <= cursorIndex; i++) body.push({ ch: " ", cells: 1, style: {} });
    const c = body[cursorIndex];
    body[cursorIndex] = { ...c, style: { ...c.style, inverse: !c.style.inverse } }; // the block cursor
  }
  const tail = list.slice(bodyEnd, Math.max(end, bodyEnd));
  return { body: rowOf(body), tail: rowOf(tail), indent: hangingIndent(body) };
}

/** Where a folded row's continuation lines start, in cells: after its leading spaces and borders, and a list marker with the spaces after it. */
function hangingIndent(list: Ch[]): number {
  const end = list.length;
  let i = 0;
  const spaces = () => {
    while (i < end && (list[i].ch === " " || BORDERS.has(list[i].ch))) i++;
  };
  spaces();
  const start = i;
  if (i < end && MARKERS.has(list[i].ch)) {
    i++;
  } else {
    while (i < end && /^[0-9]$/.test(list[i].ch)) i++;
    if (i > start && i < end && (list[i].ch === "." || list[i].ch === ")")) i++;
    else i = start;
  }
  if (i > start) {
    if (i < end && list[i].ch === " ") spaces();
    else i = start;
  }
  if (i >= end) return 0;
  let cells = 0;
  for (let k = 0; k < i; k++) cells += list[k].cells;
  return cells;
}

// ── colors ──────────────────────────────────────────────

const CUBE = [0, 95, 135, 175, 215, 255];

/** A color as CSS: the 16 terminal colors are the container's `--tc0`…`--tc15`, the rest the xterm 256-color table. */
export function colorCss(c: TermColor): string {
  if (typeof c === "string") return c;
  if (c < 16) return `var(--tc${c})`;
  if (c < 232) {
    const n = c - 16;
    const hex = [CUBE[Math.floor(n / 36)], CUBE[Math.floor(n / 6) % 6], CUBE[n % 6]].map((v) => v.toString(16).padStart(2, "0")).join("");
    return `#${hex}`;
  }
  const g = (8 + (c - 232) * 10).toString(16).padStart(2, "0");
  return `#${g}${g}${g}`;
}

/** A run's inline style: colors (inverse swaps them; bold makes the first eight bright, as xterm), weight, decorations. */
export function runCss(s: CellStyle): Record<string, string> {
  const out: Record<string, string> = {};
  let fg = s.fg;
  if (s.bold && typeof fg === "number" && fg < 8) fg += 8;
  const fgCss = fg === undefined ? "var(--tfg)" : colorCss(fg);
  const bgCss = s.bg === undefined ? undefined : colorCss(s.bg);
  if (s.inverse) {
    out.color = bgCss ?? "var(--tbg)";
    out.background = fgCss;
  } else {
    if (fg !== undefined) out.color = fgCss;
    if (bgCss) out.background = bgCss;
  }
  if (s.bold) out.fontWeight = "700";
  if (s.italic) out.fontStyle = "italic";
  if (s.dim) out.opacity = "0.55";
  const lines = [s.underline && "underline", s.strike && "line-through"].filter(Boolean);
  if (lines.length) out.textDecoration = lines.join(" ");
  if (s.invisible) out.visibility = "hidden";
  return out;
}

/** A stable key for a row (memoizing what draws it). */
export function rowKey(row: CellRow): string {
  return row.runs.map((r) => `${JSON.stringify(r.style)}${r.text}`).join("\u0000");
}
