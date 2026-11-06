import { describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.localStorage = { getItem: (k: string) => (k === "persona.token" ? "t" : null), setItem() {}, removeItem() {} };
});

import { markThumbFailed, Slots, thumbFailed, thumbSrc } from "./file-thumbs";
import { thumbable } from "./files";

describe("which files get a thumbnail", () => {
  it("pictures, PDFs, videos and documents, as iOS asks", () => {
    for (const n of ["a.png", "B.JPG", "c.heic", "d.svg", "e.pdf", "f.mp4", "g.mov", "h.docx", "i.xlsx", "j.pptx", "k.pages", "l.key", "m.rtf", "n.numbers"]) expect(thumbable(n, 10), n).toBe(true);
  });
  it("not code, text, files without an extension or empty files", () => {
    for (const n of ["a.ts", "b.md", "c.json", "Makefile", "d.csv", "e.mp3"]) expect(thumbable(n, 10), n).toBe(false);
    expect(thumbable("a.png", 0)).toBe(false);
  });
});

describe("a thumbnail's address", () => {
  it("names the file's version so a changed file asks again", () => {
    const a = thumbSrc("m1", "/w/a.png", "/w", 64, 10, "2026-11-05T00:00:00Z");
    expect(a).toContain("/api/v1/machines/m1/file/thumb?path=%2Fw%2Fa.png&cwd=%2Fw&size=64");
    expect(a).toMatch(/&v=10-2026-11-05T00%3A00%3A00Z$/);
    expect(thumbSrc("m1", "/w/a.png", "/w", 64, 11, "2026-11-05T00:00:00Z")).not.toBe(a);
  });
  it("one that failed is remembered", () => {
    const s = thumbSrc("m1", "/w/x.pdf", "/w", 64, 1);
    expect(thumbFailed(s)).toBe(false);
    markThumbFailed(s);
    expect(thumbFailed(s)).toBe(true);
  });
});

describe("thumbnails load a few at a time", () => {
  it("waits for a turn and hands it on", () => {
    const slots = new Slots(2);
    const started: number[] = [];
    const give = [0, 1, 2, 3].map((i) => slots.take(() => started.push(i)));
    expect(started).toEqual([0, 1]);
    expect(slots.queued).toBe(2);
    give[0]();
    expect(started).toEqual([0, 1, 2]);
    give[0](); // twice is once
    expect(slots.busy).toBe(2);
    give[3](); // a row scrolled away leaves the queue
    give[1]();
    give[2]();
    expect(started).toEqual([0, 1, 2]);
    expect(slots.busy).toBe(0);
    expect(slots.queued).toBe(0);
  });
});
