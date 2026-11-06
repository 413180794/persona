import { describe, expect, it } from "vitest";
import { codePathRef, findPathRefs, splitAtRefs, uniquePaths } from "./path-refs";

const paths = (t: string) => findPathRefs(t).map((r) => (r.line ? `${r.path}:${r.line}${r.col ? `:${r.col}` : ""}` : r.path));

describe("findPathRefs", () => {
  it("finds absolute, ~ and relative paths with a folder, where a message names them", () => {
    expect(paths("读 /tmp/persona-tasks/1664-lark-test.md 并完整执行这个任务")).toEqual(["/tmp/persona-tasks/1664-lark-test.md"]);
    expect(paths("看 ~/code/app/README.md 和 web/src/lib/api.ts")).toEqual(["~/code/app/README.md", "web/src/lib/api.ts"]);
    expect(paths("目录 /var/log/ 里")).toEqual(["/var/log/"]);
  });

  it("takes a line: path:12, path:12:5, path:12-20, path#L12", () => {
    expect(paths("见 server/internal/node/files.go:63 和 web/src/ui/Markdown.tsx:322:7")).toEqual(["server/internal/node/files.go:63", "web/src/ui/Markdown.tsx:322:7"]);
    expect(paths("改了 src/a.ts:12-20，还有 /x/y/b.go#L8")).toEqual(["src/a.ts:12", "/x/y/b.go:8"]);
    const [r] = findPathRefs("at src/a.ts:12.");
    expect(r).toMatchObject({ path: "src/a.ts", line: 12 });
    expect("at src/a.ts:12.".slice(r.start, r.end)).toBe("src/a.ts:12");
  });

  it("ends an ASCII path at Chinese text, full-width punctuation and a sentence's full stop", () => {
    expect(paths("写到 /tmp/a.md里了")).toEqual(["/tmp/a.md"]);
    expect(paths("已写入 /tmp/out/report.md。")).toEqual(["/tmp/out/report.md"]);
    expect(paths("See /tmp/out/report.md.")).toEqual(["/tmp/out/report.md"]);
    expect(paths("（/tmp/a/b.txt）")).toEqual(["/tmp/a/b.txt"]);
  });

  it("leaves URLs, words with a slash, versions and lone roots alone", () => {
    expect(paths("https://example.com/a/b.md and //cdn/x/y.js")).toEqual([]);
    expect(paths("model deepseek/v4.1-flash, and/or, /tmp alone, 1/2")).toEqual([]);
    expect(paths("a ~ b / c")).toEqual([]);
  });

  it("takes a path in backticks whole, non-ASCII names and spaces too", () => {
    expect(paths("生成了 `~/报告/第四季度.docx`")).toEqual(["~/报告/第四季度.docx"]);
    expect(paths("在 `~/My Notes/todo.md:3`")[0]).toBe("~/My Notes/todo.md:3");
    // both the whole span and the paths in it are candidates; what is there decides
    expect(paths("`/tmp/a.md 和 /tmp/b.md`")).toEqual(["/tmp/a.md 和 /tmp/b.md", "/tmp/a.md", "/tmp/b.md"]);
  });

  it("takes the line off a whole path in backticks", () => {
    // what fs.stat is asked about is the path alone; the line is where to open it
    expect(codePathRef("/tmp/x/a.md:12")).toMatchObject({ path: "/tmp/x/a.md", line: 12 });
    expect(codePathRef("~/报告/x.docx#L3")).toMatchObject({ path: "~/报告/x.docx", line: 3 });
    expect(findPathRefs("在 `~/My Notes/todo.md:3:5`")[0]).toMatchObject({ path: "~/My Notes/todo.md", line: 3, col: 5 });
    expect(uniquePaths(findPathRefs("见 `/tmp/x/a.md:12`"))).toEqual(["/tmp/x/a.md"]);
    expect(codePathRef("/tmp/x/a.md")).toMatchObject({ path: "/tmp/x/a.md", line: undefined });
  });

  it("asks about each path once", () => {
    expect(uniquePaths(findPathRefs("src/a.ts:1 src/a.ts:2 `src/a.ts`"))).toEqual(["src/a.ts"]);
  });
});

describe("codePathRef", () => {
  it("is a code span that is a whole path", () => {
    expect(codePathRef("src/a.ts:12")).toMatchObject({ path: "src/a.ts", line: 12 });
    expect(codePathRef(" /tmp/x/y.md ")).toMatchObject({ path: "/tmp/x/y.md", start: 1, end: 12 });
    expect(codePathRef("~/报告/第四季度.docx")).toMatchObject({ path: "~/报告/第四季度.docx" });
    expect(codePathRef("ls /tmp/x/y.md")).toBeUndefined();
    expect(codePathRef("/tmp")).toBeUndefined();
    expect(codePathRef("https://example.com/a/b")).toBeUndefined();
  });
});

describe("splitAtRefs", () => {
  it("cuts at the refs that are there, the longer at the same place first, none overlapping", () => {
    const t = "`/tmp/a.md 和 /tmp/b.md`";
    const there = new Set(["/tmp/a.md", "/tmp/b.md"]);
    const parts = splitAtRefs(t, findPathRefs(t), (p) => there.has(p));
    expect(parts.map((p) => (typeof p === "string" ? p : `[${p.path}]`))).toEqual(["`", "[/tmp/a.md]", " 和 ", "[/tmp/b.md]", "`"]);
    expect(splitAtRefs("x /tmp/a/b.md", findPathRefs("x /tmp/a/b.md"), () => false)).toEqual(["x /tmp/a/b.md"]);
  });
});
