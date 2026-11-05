import { describe, expect, it } from "vitest";
import {
  FIND_CLOSED,
  FIND_LIMIT,
  findInLines,
  findIndex,
  findLabel,
  findReducer,
  isFindShortcut,
  marksFor,
  queryFromSelection,
  stepFind,
  type FindState,
} from "./terminal-search";

describe("finding in lines", () => {
  const lines = ["$ npm test", "PASS src/a.test.ts", "", "pass again, Pass"];

  it("finds every match, top to bottom, ignoring case by default", () => {
    expect(findInLines(lines, "pass", false)).toEqual([
      { row: 1, start: 0, end: 4 },
      { row: 3, start: 0, end: 4 },
      { row: 3, start: 12, end: 16 },
    ]);
  });

  it("matches the case when asked", () => {
    expect(findInLines(lines, "pass", true)).toEqual([{ row: 3, start: 0, end: 4 }]);
    expect(findInLines(lines, "PASS", true)).toEqual([{ row: 1, start: 0, end: 4 }]);
  });

  it("takes the query as text, not a pattern", () => {
    expect(findInLines(["a.b", "axb", "(x)"], ".", false)).toEqual([{ row: 0, start: 1, end: 2 }]);
    expect(findInLines(["(x) [y] $z"], "(x)", false)).toEqual([{ row: 0, start: 0, end: 3 }]);
    expect(findInLines(["a*b"], "*", false)).toHaveLength(1);
  });

  it("finds overlapping text once per start, non-overlapping", () => {
    expect(findInLines(["aaaa"], "aa", false)).toHaveLength(2);
  });

  it("finds Chinese text", () => {
    expect(findInLines(["构建完成", "测试通过"], "通过", false)).toEqual([{ row: 1, start: 2, end: 4 }]);
  });

  it("finds nothing for an empty query or no match", () => {
    expect(findInLines(lines, "", false)).toEqual([]);
    expect(findInLines(lines, "zzz", false)).toEqual([]);
    expect(findInLines([], "a", false)).toEqual([]);
  });

  it("stops at the limit", () => {
    expect(findInLines(["a".repeat(FIND_LIMIT + 50)], "a", false)).toHaveLength(FIND_LIMIT);
  });
});

describe("marks", () => {
  it("lists the rows with a match and the row of the current one", () => {
    const m = findInLines(["a", "b", "a a"], "a", false);
    const marks = marksFor(m, 2);
    expect([...marks.rows]).toEqual([0, 2]);
    expect(marks.current).toBe(2);
    expect(marksFor(m, null).current).toBeNull();
    expect(marksFor(m, 9).current).toBeNull();
  });
});

describe("stepping through both lists", () => {
  it("starts at the first match going forward and the last going back", () => {
    expect(stepFind(null, 1, 2, 3)).toEqual({ seg: "rows", i: 0 });
    expect(stepFind(null, -1, 2, 3)).toEqual({ seg: "screen", i: 2 });
    expect(stepFind(null, 1, 0, 3)).toEqual({ seg: "screen", i: 0 });
    expect(stepFind(null, -1, 2, 0)).toEqual({ seg: "rows", i: 1 });
  });

  it("goes from the rows to the screen and back, then wraps", () => {
    expect(stepFind({ seg: "rows", i: 0 }, 1, 2, 3)).toEqual({ seg: "rows", i: 1 });
    expect(stepFind({ seg: "rows", i: 1 }, 1, 2, 3)).toEqual({ seg: "screen", i: 0 });
    expect(stepFind({ seg: "screen", i: 2 }, 1, 2, 3)).toEqual({ seg: "rows", i: 0 });
    expect(stepFind({ seg: "rows", i: 0 }, -1, 2, 3)).toEqual({ seg: "screen", i: 2 });
    expect(stepFind({ seg: "screen", i: 0 }, -1, 2, 3)).toEqual({ seg: "rows", i: 1 });
  });

  it("wraps within one list", () => {
    expect(stepFind({ seg: "screen", i: 2 }, 1, 0, 3)).toEqual({ seg: "screen", i: 0 });
    expect(stepFind({ seg: "rows", i: 0 }, -1, 3, 0)).toEqual({ seg: "rows", i: 2 });
    expect(stepFind({ seg: "rows", i: 0 }, 1, 1, 0)).toEqual({ seg: "rows", i: 0 });
  });

  it("has nowhere to go with no matches", () => {
    expect(stepFind(null, 1, 0, 0)).toBeNull();
    expect(stepFind({ seg: "rows", i: 0 }, 1, 0, 0)).toBeNull();
  });

  it("numbers the current match over both lists", () => {
    expect(findIndex({ seg: "rows", i: 1 }, 4)).toBe(1);
    expect(findIndex({ seg: "screen", i: 1 }, 4)).toBe(5);
  });
});

describe("the label", () => {
  it("is empty before a query, 无结果 without matches, else n/total", () => {
    expect(findLabel("", null)).toBe("");
    expect(findLabel("a", null)).toBe("无结果");
    expect(findLabel("a", { index: -1, count: 0 })).toBe("无结果");
    expect(findLabel("a", { index: 0, count: 12 })).toBe("1/12");
    expect(findLabel("a", { index: 11, count: 12 })).toBe("12/12");
  });

  it("shows only the count when no match is current, and 999+ past the limit", () => {
    expect(findLabel("a", { index: -1, count: 5 })).toBe("5");
    expect(findLabel("a", { index: 3, count: FIND_LIMIT + 1 })).toBe("4/999+");
  });
});

describe("the find bar's state", () => {
  const open: FindState = { open: true, query: "err", caseSensitive: true };

  it("opens, keeping the last query unless the selection brings one", () => {
    expect(findReducer(FIND_CLOSED, { type: "open" })).toEqual({ open: true, query: "", caseSensitive: false });
    expect(findReducer({ ...open, open: false }, { type: "open" }).query).toBe("err");
    expect(findReducer(open, { type: "open", query: "warn" }).query).toBe("warn");
  });

  it("closes and forgets the query, keeping the case switch", () => {
    expect(findReducer(open, { type: "close" })).toEqual({ open: false, query: "", caseSensitive: true });
    expect(findReducer(FIND_CLOSED, { type: "close" })).toBe(FIND_CLOSED);
  });

  it("follows the field and toggles the case", () => {
    expect(findReducer(open, { type: "query", query: "errors" }).query).toBe("errors");
    expect(findReducer(open, { type: "query", query: "err" })).toBe(open);
    expect(findReducer(open, { type: "toggleCase" }).caseSensitive).toBe(false);
    expect(findReducer(FIND_CLOSED, { type: "toggleCase" }).caseSensitive).toBe(true);
  });

  it("brings a one-line selection to the field, trimmed and capped", () => {
    expect(queryFromSelection("  error  ")).toBe("error");
    expect(queryFromSelection("")).toBeNull();
    expect(queryFromSelection("   ")).toBeNull();
    expect(queryFromSelection("two\nlines")).toBeNull();
    expect(queryFromSelection("x".repeat(500))).toHaveLength(200);
  });
});

describe("the shortcut", () => {
  const e = (over: Partial<Parameters<typeof isFindShortcut>[0]>) => ({ type: "keydown", key: "f", ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...over });

  it("is ⌘F on a Mac", () => {
    expect(isFindShortcut(e({ metaKey: true }), { mac: true, driving: true })).toBe(true);
    expect(isFindShortcut(e({ metaKey: true, key: "F" }), { mac: true, driving: false })).toBe(true);
    expect(isFindShortcut(e({ ctrlKey: true }), { mac: true, driving: false })).toBe(false); // ^F is the program's
    expect(isFindShortcut(e({ metaKey: true, shiftKey: true }), { mac: true, driving: false })).toBe(false);
  });

  it("is Ctrl+Shift+F elsewhere, and Ctrl+F too while not driving (^F is the program's then)", () => {
    expect(isFindShortcut(e({ ctrlKey: true, shiftKey: true, key: "F" }), { mac: false, driving: true })).toBe(true);
    expect(isFindShortcut(e({ ctrlKey: true }), { mac: false, driving: true })).toBe(false);
    expect(isFindShortcut(e({ ctrlKey: true }), { mac: false, driving: false })).toBe(true);
    expect(isFindShortcut(e({ metaKey: true }), { mac: false, driving: false })).toBe(false);
  });

  it("is not another key, a release or a chord with Alt", () => {
    expect(isFindShortcut(e({ metaKey: true, key: "g" }), { mac: true, driving: false })).toBe(false);
    expect(isFindShortcut(e({ metaKey: true, type: "keyup" }), { mac: true, driving: false })).toBe(false);
    expect(isFindShortcut(e({ metaKey: true, altKey: true }), { mac: true, driving: false })).toBe(false);
    expect(isFindShortcut(e({}), { mac: true, driving: false })).toBe(false);
  });
});
