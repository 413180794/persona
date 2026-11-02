import { describe, expect, it } from "vitest";
import {
  applySGR,
  charWidth,
  colorCss,
  foldRow,
  gridRow,
  parseAnsiRows,
  plainRow,
  rowKey,
  runCss,
  textCells,
  type CellRow,
  type GridCell,
} from "./terminal-cells";

const text = (row: CellRow) => row.runs.map((r) => r.text).join("");

describe("widths", () => {
  it("counts Chinese and most emoji as two cells, combining marks as none", () => {
    expect(charWidth("a".codePointAt(0)!)).toBe(1);
    expect(charWidth("中".codePointAt(0)!)).toBe(2);
    expect(charWidth("，".codePointAt(0)!)).toBe(2);
    expect(charWidth("😀".codePointAt(0)!)).toBe(2);
    expect(charWidth(0x0301)).toBe(0);
    expect(charWidth("─".codePointAt(0)!)).toBe(1);
    expect(textCells("ab中文")).toBe(6);
  });
});

describe("ANSI rows", () => {
  it("splits rows and keeps each run's style, carried over to the next row", () => {
    const rows = parseAnsiRows("\x1b[1;31mred\x1b[0m plain\n\x1b[32mgreen\nstill green\x1b[39m");
    expect(rows).toHaveLength(3);
    expect(rows[0].runs).toEqual([
      { text: "red", cells: 3, style: { bold: true, fg: 1 } },
      { text: " plain", cells: 6, style: {} },
    ]);
    expect(rows[1].runs[0].style).toEqual({ fg: 2 });
    expect(rows[2].runs[0]).toEqual({ text: "still green", cells: 11, style: { fg: 2 } });
  });

  it("reads 256 colors and true color, in both the semicolon and the colon form", () => {
    expect(applySGR({}, "38;5;208")).toEqual({ fg: 208 });
    expect(applySGR({}, "48;2;255;0;16")).toEqual({ bg: "#ff0010" });
    expect(applySGR({}, "38:2::1:2:3")).toEqual({ fg: "#010203" });
    expect(applySGR({}, "92;104")).toEqual({ fg: 10, bg: 12 });
    expect(applySGR({ fg: 1, bold: true, underline: true }, "22;24;39")).toEqual({});
    expect(applySGR({ fg: 1 }, "")).toEqual({});
  });

  it("drops other escapes and expands tabs", () => {
    const [row] = parseAnsiRows("\x1b]8;;https://example.com\x07link\x1b]8;;\x07\x1b[2Ka\tb");
    expect(text(row)).toBe("linka   b");
  });

  it("gives the plain text the node matches the anchor by", () => {
    const rows = parseAnsiRows("\x1b[7m❯ \x1b[0mgit status   \r\n中文  ");
    expect(rows.map(plainRow)).toEqual(["❯ git status", "中文"]);
  });
});

function cell(ch: string, opts: Partial<{ w: number; fg: number; rgb: boolean; bold: boolean; inverse: boolean }> = {}): GridCell {
  return {
    getWidth: () => opts.w ?? 1,
    getChars: () => ch,
    isFgDefault: () => opts.fg === undefined,
    isFgRGB: () => Boolean(opts.rgb),
    getFgColor: () => opts.fg ?? 0,
    isBgDefault: () => true,
    isBgRGB: () => false,
    getBgColor: () => 0,
    isBold: () => (opts.bold ? 1 : 0),
    isDim: () => 0,
    isItalic: () => 0,
    isUnderline: () => 0,
    isInverse: () => (opts.inverse ? 1 : 0),
    isInvisible: () => 0,
    isStrikethrough: () => 0,
  };
}

describe("xterm's grid", () => {
  it("joins cells of one style and skips the second half of a wide character", () => {
    const cells = [cell("中", { w: 2 }), cell("", { w: 0 }), cell("a"), cell("b", { fg: 0x336699, rgb: true }), cell("")];
    const row = gridRow(cells.length, (x) => cells[x]);
    expect(row.runs).toEqual([
      { text: "中a", cells: 3, style: {} },
      { text: "b", cells: 1, style: { fg: "#336699" } },
      { text: " ", cells: 1, style: {} },
    ]);
  });
});

describe("folding", () => {
  it("drops blank padding and keeps a rule across the pane apart from the text", () => {
    const [row] = parseAnsiRows("abc" + "─".repeat(10) + "   ");
    const f = foldRow(row);
    expect(text(f.body)).toBe("abc");
    expect(text(f.tail)).toBe("─".repeat(10));
    const [rule] = parseAnsiRows("─".repeat(20));
    expect(text(foldRow(rule).body)).toBe("");
    expect(text(foldRow(rule).tail)).toBe("─".repeat(20));
  });

  it("keeps a background that pads the row in the tail", () => {
    const [row] = parseAnsiRows("x\x1b[44m     \x1b[0m");
    const f = foldRow(row);
    expect(text(f.body)).toBe("x");
    expect(f.tail.runs).toEqual([{ text: "     ", cells: 5, style: { bg: 4 } }]);
  });

  it("hangs continuation lines under a list marker or a number", () => {
    expect(foldRow(parseAnsiRows("⏺ 好的，先跑一下测试")[0]).indent).toBe(2);
    expect(foldRow(parseAnsiRows("  ⎿  ok")[0]).indent).toBe(5);
    expect(foldRow(parseAnsiRows("  12. item")[0]).indent).toBe(6);
    expect(foldRow(parseAnsiRows("│ text")[0]).indent).toBe(2);
    expect(foldRow(parseAnsiRows("plain words")[0]).indent).toBe(0);
    expect(foldRow(parseAnsiRows("-x")[0]).indent).toBe(0);
  });

  it("draws the cursor as an inverted cell, past the text if it is there", () => {
    const [row] = parseAnsiRows("$ ls   ");
    const f = foldRow(row, 5);
    expect(text(f.body)).toBe("$ ls  ");
    expect(f.body.runs[f.body.runs.length - 1]).toEqual({ text: " ", cells: 1, style: { inverse: true } });
    const g = foldRow(parseAnsiRows("中文")[0], 2);
    expect(g.body.runs[1]).toEqual({ text: "文", cells: 2, style: { inverse: true } });
  });
});

describe("colors", () => {
  it("maps the 16 colors to the container's variables and the rest to the xterm table", () => {
    expect(colorCss(1)).toBe("var(--tc1)");
    expect(colorCss(16)).toBe("#000000");
    expect(colorCss(208)).toBe("#ff8700");
    expect(colorCss(244)).toBe("#808080");
    expect(colorCss("#123456")).toBe("#123456");
  });

  it("swaps colors when inverted and brightens bold ones", () => {
    expect(runCss({ inverse: true })).toEqual({ color: "var(--tbg)", background: "var(--tfg)" });
    expect(runCss({ fg: 1, bold: true })).toEqual({ color: "var(--tc9)", fontWeight: "700" });
    expect(runCss({ dim: true, underline: true, strike: true })).toEqual({ opacity: "0.55", textDecoration: "underline line-through" });
  });

  it("keys rows by their text and styles", () => {
    const [a, b] = parseAnsiRows("\x1b[31mx\x1b[0m\nx");
    expect(rowKey(a)).not.toBe(rowKey(b));
    expect(rowKey(parseAnsiRows("\x1b[31mx")[0])).toBe(rowKey(a));
  });
});
