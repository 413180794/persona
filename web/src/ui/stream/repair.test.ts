import { describe, expect, it } from "vitest";
import { repairStreamingMarkdown as repair } from "./repair";

describe("repairStreamingMarkdown", () => {
  it("closes an open code fence with its own marker", () => {
    expect(repair("text\n```ts\nconst a")).toBe("text\n```ts\nconst a\n```");
    expect(repair("~~~~\nx\n")).toBe("~~~~\nx\n~~~~");
    expect(repair("```\na\n```\ndone")).toBe("```\na\n```\ndone");
  });

  it("closes ** and * the growing line leaves open", () => {
    expect(repair("这是 **重要")).toBe("这是 **重要**");
    expect(repair("an *emph")).toBe("an *emph*");
    expect(repair("**done** and *also*")).toBe("**done** and *also*");
  });

  it("leaves what it can't be sure of", () => {
    expect(repair("trailing **")).toBe("trailing **"); // nothing after the opener yet
    expect(repair("** spaced")).toBe("** spaced"); // not an opener
    expect(repair("**a\nb")).toBe("**a\nb"); // another line
    expect(repair("* item\n* other")).toBe("* item\n* other"); // list markers
    expect(repair("***both")).toBe("***both");
    expect(repair("`code **x")).toBe("`code **x"); // half a code span
    expect(repair("a \\*literal")).toBe("a \\*literal");
  });

  it("shows a half-written image or link as its text", () => {
    expect(repair("see ![chart](https://exa")).toBe("see chart");
    expect(repair("see ![cha")).toBe("see cha");
    expect(repair("read [the docs](https://exa")).toBe("read the docs");
    expect(repair("read [the docs](https://example.com) ok")).toBe("read [the docs](https://example.com) ok");
  });

  it("holds a table's half-written last row back until its line ends (#5602)", () => {
    const head = "| 文件 | 改动 |\n| --- | --- |\n| a.swift | 刷新 |\n";
    expect(repair(head + "| b.swi")).toBe(head);
    expect(repair(head + "| b.swift | 超时 |\n")).toBe(head + "| b.swift | 超时 |\n");
    // the header itself, before its divider, and a pipe in prose are left alone
    expect(repair("| 文件 | 改")).toBe("| 文件 | 改");
    expect(repair("Use a | b\n| not a table")).toBe("Use a | b\n| not a table");
  });
});
