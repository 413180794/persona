import { describe, expect, it } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import { normalizeMath } from "../Markdown";
import { splitChunks } from "./chunks";
import { repairStreamingMarkdown } from "./repair";

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

// Carried over from #1966 (by @tzh476): a long answer with every kind of block a cut could break, streamed a character
// at a time through Markdown's own steps (repair, normalizeMath) before it is cut.
describe("splitChunks while a reply grows", () => {
  const ANSWER = [
    "## 看完了",
    "改动没问题，有两处 **建议**，见 `src/a.ts:12` 和 https://example.com/x。",
    "- 第一项\n- 第二项",
    "1. 先做这个\n\n   接着第一项\n\n2. 再做那个\n3. 最后",
    "```ts\nconst a = 1;\n\nconst b = 2;\n```",
    "- 列表里的代码：\n\n  ```sh\n  echo 1\n\n  echo 2\n  ```",
    "前文：\n\n    indented code\n\n    more code",
    "| a | b |\n| --- | --- |\n| 1 | $x\\vert y$ |",
    "> 引用一段\n> 第二行",
    "> 另一段引用",
    "公式 $E = mc^2$，以及：\n\n$$\n\\int_0^1 x\\,dx\n\n= \\tfrac12\n$$",
    "\\[\na^2 + b^2\n\n= c^2\n\\]",
    "---",
    "```mermaid\ngraph TD; A-->B\n```",
    "标题二\n=====",
    "*强调* 开头，**粗体**",
    "> - 引用里的列表\n>\n> - 第二项",
    "- 外层\n\n  - 内层一\n\n  - 内层二",
    "1) 括号编号\n\n2) 第二",
    "2024\n\n2025 年",
    "要我留评论吗？",
  ].join("\n\n");
  const prepared = (n: number) => normalizeMath(repairStreamingMarkdown(ANSWER.slice(0, n)));

  it("does not cut before a list item still being written, or inside an open $$ or \\[ block", () => {
    const contents = (md: string) => splitChunks(md).map((x) => x.content);
    expect(contents("1. 一\n\n2.")).toEqual(["1. 一\n\n2."]); // an item with no text yet is still the list's
    expect(contents("- 一\n\n-")).toEqual(["- 一\n\n-"]);
    expect(contents("- 一\n\n2")).toEqual(["- 一\n\n2"]); // may become "2." with the next character
    expect(contents("- 一\n\n2025 年")).toEqual(["- 一\n\n", "2025 年"]);
    expect(contents(normalizeMath("前文\n\n$$\na\n\nb"))).toHaveLength(2); // open: no cut after the $$
    expect(contents("前文\n\n\\[\na\n\nb")).toHaveLength(2);
  });

  it("keeps what it sealed as the text grows", () => {
    let sealed: string[] = [];
    for (let n = 1; n <= ANSWER.length; n++) {
      const now = splitChunks(prepared(n)).slice(0, -1).map((x) => x.content);
      expect(now.slice(0, sealed.length), `at ${n}`).toEqual(sealed);
      sealed = now;
    }
    expect(sealed.length).toBeGreaterThan(8);
  });

  it("draws the same chunk by chunk as the whole text at every step", () => {
    // react-markdown puts a newline between blocks; between two chunks drawn separately there is none
    const draw = (md: string) => renderToStaticMarkup(createElement(ReactMarkdown, { remarkPlugins: [remarkGfm, remarkMath] }, md)).replace(/>\s+</g, "><");
    // A sealed chunk is the same text at every later step: drawn once, not once per step. Drawing them all again took
    // this test past its 5 s timeout under the full parallel suite (#4955).
    const drawn = new Map<string, string>();
    const html = (md: string) => drawn.get(md) ?? drawn.set(md, draw(md)).get(md)!;
    for (let n = 1; n <= ANSWER.length; n += 2) {
      const md = prepared(n);
      expect(splitChunks(md).map((x) => html(x.content)).join(""), `at ${n}: ${JSON.stringify(md.slice(-40))}`).toBe(html(md));
    }
  });
});
