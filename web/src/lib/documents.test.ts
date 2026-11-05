import { describe, expect, it } from "vitest";
import { DARK_PAGE, documentKind, isDocument, lightness } from "./documents";

describe("documents (iOS DocumentCards)", () => {
  it("takes the files with a first page to show, whatever the case of the extension", () => {
    for (const p of ["/r/a.docx", "/r/b.DOC", "/r/c.xlsx", "/r/d.pptx", "/r/e.pages", "/r/f.numbers", "/r/g.key", "/r/h.rtf", "/r/i.PDF"]) expect(isDocument(p), p).toBe(true);
    for (const p of ["/r/a.png", "/r/b.mp4", "/r/c.ts", "/r/notes.md", "/r/.pdf", "/r/pdf", "/r/a.docx/"]) expect(isDocument(p), p).toBe(false);
  });

  it("says what each one is, as the Files app does", () => {
    expect(documentKind("/r/报告.docx")).toBe("Word 文档");
    expect(documentKind("/r/a.xls")).toBe("Excel 表格");
    expect(documentKind("/r/a.ppt")).toBe("PowerPoint 演示文稿");
    expect(documentKind("/r/a.key")).toBe("Keynote 演示文稿");
    expect(documentKind("/r/a.pdf")).toBe("PDF 文档");
    expect(documentKind("/r/a.bin")).toBe("文档");
  });

  it("tells a dark first page (Quick Look in dark mode) from a page, laying the pixels over white", () => {
    const px = (r: number, g: number, b: number, a = 255, n = 64) => Array.from({ length: n }, () => [r, g, b, a]).flat();
    expect(lightness(px(255, 255, 255))).toBe(1);
    expect(lightness(px(0, 0, 0))).toBe(0);
    expect(lightness(px(0, 0, 0, 0))).toBe(1); // transparent counts as the white under it
    expect(lightness(px(30, 30, 30))).toBeLessThan(DARK_PAGE);
    // a white page with black text on a quarter of it is still a page
    expect(lightness([...px(255, 255, 255, 255, 48), ...px(0, 0, 0, 255, 16)])).toBeGreaterThan(DARK_PAGE);
    expect(lightness([])).toBe(1);
  });
});
