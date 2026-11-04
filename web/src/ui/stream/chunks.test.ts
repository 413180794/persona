import { describe, expect, it } from "vitest";
import { splitChunks } from "./chunks";

describe("splitChunks", () => {
  it("cuts at top-level blank lines, each chunk starting where it is in the text", () => {
    const md = "first para\n\nsecond para\n\nthird";
    const c = splitChunks(md);
    expect(c.map((x) => x.content)).toEqual(["first para\n\n", "second para\n\n", "third"]);
    for (const x of c) expect(md.slice(x.start, x.start + x.content.length)).toBe(x.content);
  });

  it("keeps a code fence, a $$ block, lists and quotes whole", () => {
    expect(splitChunks("```\na\n\nb\n```\n\nafter").map((x) => x.content)).toEqual(["```\na\n\nb\n```\n\n", "after"]);
    expect(splitChunks("$$\na\n\nb\n$$\n\nafter")).toHaveLength(2);
    expect(splitChunks("- one\n\n- two\n\n  more")).toHaveLength(1);
    expect(splitChunks("> a\n\n> b")).toHaveLength(1);
  });

  it("an unclosed fence (still streaming) is not cut", () => {
    expect(splitChunks("intro\n\n```ts\nconst a = 1\n\nconst b")).toHaveLength(2);
    expect(splitChunks("intro\n\n```ts\nconst a = 1\n\nconst b")[1].content).toBe("```ts\nconst a = 1\n\nconst b");
  });

  it("stays one chunk when blocks depend on each other: references, HTML; headings don't", () => {
    expect(splitChunks("see [x]\n\n[x]: https://example.com")).toHaveLength(1);
    expect(splitChunks("# A\n\ntext\n\n# B")).toHaveLength(3);
    expect(splitChunks("<div>\n\nhi\n\n</div>")).toHaveLength(1);
    expect(splitChunks("# A\n\ntext\n\nmore")).toHaveLength(3);
  });

  it("a text without a blank line is one chunk", () => {
    expect(splitChunks("just one line")).toEqual([{ start: 0, content: "just one line" }]);
  });
});
