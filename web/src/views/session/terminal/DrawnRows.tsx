// Terminal rows the page draws itself: pages of herdr's history above the screen, and — 换行 while watching — the screen
// folded at the panel's width (iOS TerminalWrap: continuation lines hang under the row's text, what only pads a row to
// the pane's width ends at the panel's edge). Same font, size and colors as xterm, so Chinese takes its two cells here too.
import { memo, useMemo } from "react";
import type { TerminalPalette } from "../../../lib/terminal";
import { foldRow, rowKey, runCss, type CellRow, type Run } from "../../../lib/terminal-cells";

const PALETTE_ORDER: (keyof TerminalPalette)[] = [
  "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
  "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightMagenta", "brightCyan", "brightWhite",
];

/** The rows' font and colors: `--tfg` / `--tbg` and the 16 colors `--tc0`…`--tc15` the runs refer to. */
export function rowsStyle(palette: TerminalPalette | null, fontFamily: string, fontSize: number): React.CSSProperties {
  const vars: Record<string, string> = {};
  if (palette) {
    vars["--tfg"] = palette.foreground;
    vars["--tbg"] = palette.background;
    PALETTE_ORDER.forEach((k, i) => (vars[`--tc${i}`] = palette[k]));
  }
  return { ...vars, fontFamily: fontFamily || undefined, fontSize, lineHeight: 1.3, color: "var(--tfg)" } as React.CSSProperties;
}

function Runs({ runs }: { runs: Run[] }) {
  return (
    <>
      {runs.map((r, i) => (
        <span key={i} style={runCss(r.style)}>
          {r.text}
        </span>
      ))}
    </>
  );
}

type RowProps = { row: CellRow; sig: string; cursor?: number };
/** Rows are read again on every write: one draws again only when its text, styles or cursor changed. */
const same = (a: RowProps, b: RowProps) => a.sig === b.sig && a.cursor === b.cursor;

/** One row at the pane's width (no folding: the panel scrolls sideways). */
const PlainRow = memo(function PlainRow({ row }: RowProps) {
  return (
    <div className="min-h-[1.3em] whitespace-pre">
      <Runs runs={row.runs} />
    </div>
  );
}, same);

/** One row folded at the panel's width: its text wraps under its indent; its padding is cut at the edge. */
const FoldedRow = memo(function FoldedRow({ row, cursor }: RowProps) {
  const f = foldRow(row, cursor);
  return (
    <div
      className="min-h-[1.3em] overflow-hidden whitespace-pre-wrap [overflow-wrap:anywhere]"
      style={f.indent ? { paddingLeft: `${f.indent}ch`, textIndent: `-${f.indent}ch` } : undefined}
    >
      <Runs runs={f.body.runs} />
      {f.tail.runs.length > 0 && (
        <span className="whitespace-pre">
          <Runs runs={f.tail.runs} />
        </span>
      )}
    </div>
  );
}, same);

/**
 * Rows, folded or not; `cursor` puts the cursor in one of them. History grows at the top: its rows are keyed from the
 * end (`fromEnd`), so a page placed above draws only the new rows.
 */
export function DrawnRows({
  rows,
  fold,
  cursor,
  fromEnd,
}: {
  rows: CellRow[];
  fold: boolean;
  cursor?: { row: number; col: number } | null;
  fromEnd?: boolean;
}) {
  const sigs = useMemo(() => rows.map(rowKey), [rows]);
  return (
    <>
      {rows.map((row, i) => {
        const key = fromEnd ? rows.length - i : i;
        return fold ? (
          <FoldedRow key={key} row={row} sig={sigs[i]} cursor={cursor?.row === i ? cursor.col : undefined} />
        ) : (
          <PlainRow key={key} row={row} sig={sigs[i]} />
        );
      })}
    </>
  );
}
