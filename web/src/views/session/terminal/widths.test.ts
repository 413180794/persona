import { describe, expect, it } from "vitest";
import { Terminal } from "@xterm/xterm";
import { matchHerdrWidths } from "./widths";

/** The width xterm gives the character at the start of `text` once it is written to a fresh terminal. */
async function widthOf(term: Terminal, text: string): Promise<number> {
  await new Promise<void>((done) => term.write(`\x1b[2J\x1b[H${text}`, done));
  return term.buffer.active.getLine(0)!.getCell(0)!.getWidth();
}

describe("matchHerdrWidths", () => {
  it("gives emoji two cells like herdr, where xterm's own table gives one", async () => {
    const plain = new Terminal({ allowProposedApi: true, cols: 20, rows: 2 });
    expect(await widthOf(plain, "\u{1F600}")).toBe(1);
    expect(await widthOf(plain, "✅")).toBe(1);
    const term = new Terminal({ allowProposedApi: true, cols: 20, rows: 2 });
    matchHerdrWidths(term);
    expect(term.unicode.activeVersion).toBe("15-graphemes");
    for (const emoji of ["\u{1F600}", "\u{1F680}", "✅", "\u{1F468}‍\u{1F469}‍\u{1F467}", "\u{1F1E8}\u{1F1F3}", "❤️"]) {
      expect(await widthOf(term, emoji), emoji).toBe(2);
    }
  });

  it("keeps Chinese and full-width punctuation at two cells and Latin text and box drawing at one", async () => {
    const term = new Terminal({ allowProposedApi: true, cols: 20, rows: 2 });
    matchHerdrWidths(term);
    for (const wide of ["中", "「", "」", "（", "，", "：", "！", "あ", "한"]) expect(await widthOf(term, wide), wide).toBe(2);
    for (const narrow of ["a", "│", "─", "⌘", "✓", "★", "→"]) expect(await widthOf(term, narrow), narrow).toBe(1);
  });
});
