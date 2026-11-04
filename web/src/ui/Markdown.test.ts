import { describe, expect, it } from "vitest";
import katex from "katex";
import { linkify, normalizeMath, splitCode } from "./Markdown";

describe("normalizeMath", () => {
  it("rewrites \\( \\) to inline dollars", () => {
    expect(normalizeMath("面积是 \\(\\pi r^2\\)。")).toBe("面积是 $\\pi r^2$。");
  });

  it("rewrites \\[ \\] to a display block", () => {
    expect(normalizeMath("公式：\\[ E = mc^2 \\]")).toBe("公式：\n$$\nE = mc^2\n$$");
    expect(normalizeMath("公式：\\[ E = mc^2 \\] 完")).toBe("公式：\n$$\nE = mc^2\n$$\n 完");
  });

  it("keeps display blocks inside list items and quotes", () => {
    expect(normalizeMath("- 公式：\\[ E=mc^2 \\]")).toBe("- 公式：\n  $$\n  E=mc^2\n  $$");
    expect(normalizeMath("> 引用里：\\[ a \\]")).toBe("> 引用里：\n> $$\n> a\n> $$");
    expect(normalizeMath("- $$x^2$$")).toBe("- $$\n  x^2\n  $$");
    expect(normalizeMath("> $$x^2$$")).toBe("> $$\n> x^2\n> $$");
  });

  it("leaves dollars alone", () => {
    expect(normalizeMath("$a_1 * b_2$ 和 $$x^2$$")).toBe("$a_1 * b_2$ 和 $$x^2$$");
  });

  it("turns a line holding only $$…$$ into a display block", () => {
    expect(normalizeMath("积分：\n$$\\int_0^1 x\\,dx$$\n完")).toBe("积分：\n$$\n\\int_0^1 x\\,dx\n$$\n完");
    expect(normalizeMath("$$a$$ 和 $$b$$")).toBe("$$a$$ 和 $$b$$");
  });

  it("keeps prices as text", () => {
    expect(normalizeMath("价格 $5 和 $10")).toBe("价格 \\$5 和 \\$10");
    expect(normalizeMath("共 $5，公式 $x^2$")).toBe("共 \\$5，公式 $x^2$");
    expect(normalizeMath("$x^2$ 和 $2x$")).toBe("$x^2$ 和 $2x$");
  });

  it("does not pair an unclosed $ with the next formula", () => {
    expect(normalizeMath("未闭合 $\\frac{a}{b 后面 $\\sqrt{2}$")).toBe("未闭合 \\$\\frac{a}{b 后面 $\\sqrt{2}$");
  });

  it("rewrites | inside math on table rows", () => {
    expect(normalizeMath("| 绝对值 | $|x|$ |\n| 范数 | $\\|v\\|$ |")).toBe(
      "| 绝对值 | $\\vert{}x\\vert{}$ |\n| 范数 | $\\Vert{}v\\Vert{}$ |",
    );
    expect(normalizeMath("不是表格 $|x|$")).toBe("不是表格 $|x|$");
  });

  it("keeps $ next to letters or digits as text", () => {
    expect(normalizeMath("$HOME/$USER/bin")).toBe("\\$HOME/\\$USER/bin");
    expect(normalizeMath("US$5 和 A$10")).toBe("US\\$5 和 A\\$10");
  });

  it("handles backslashes before and inside math", () => {
    expect(normalizeMath("换行 \\\\$x$")).toBe("换行 \\\\$x$");
    expect(normalizeMath("$\\text{售价 \\$5}$")).toBe('$\\text{售价 {\\char"24}5}$');
  });

  it("leaves code alone", () => {
    const md = "`\\(x\\)` 和\n```\n\\[y\\]\n```";
    expect(normalizeMath(md)).toBe(md);
    for (const code of [
      "~~~\n$5 和 \\(x\\)\n~~~",
      "前文\n\n    cost = $5 + $6\n    \\(x\\)\n\n后文",
      "``a `$5` b $6``",
      "```\n未闭合 $5 和 $6",
      "- 列表\n  ```\n  $5 和 $6\n  ```",
    ]) {
      expect(normalizeMath(code)).toBe(code);
    }
  });
});

describe("KaTeX", () => {
  it("renders what normalizeMath writes and mhchem", () => {
    for (const tex of ['\\text{售价 {\\char"24}5}', "\\ce{H2O}", "\\pu{123 kJ/mol}", "\\vert{}x\\vert{}"]) {
      expect(() => katex.renderToString(tex, { throwOnError: true })).not.toThrow();
    }
  });
});

describe("splitCode", () => {
  it("returns text and code alternately", () => {
    expect(splitCode("a `b` c")).toEqual(["a ", "`b`", " c"]);
    expect(splitCode("x\n~~~\ny\n~~~\nz")).toEqual(["x\n", "~~~\ny\n~~~\n", "z"]);
    expect(splitCode("x\n\n    code\ny")).toEqual(["x\n\n", "    code\n", "y"]);
  });

  it("does not treat indented list continuations as code", () => {
    expect(splitCode("- a\n\n    b $5")).toEqual(["- a\n\n    b $5"]);
  });
});

describe("splitCode containers", () => {
  it("advances past whitespace-only indented lines", () => {
    expect(splitCode("text\n\n    \nnext")).toEqual(["text\n\n    \nnext"]);
    expect(splitCode("    ")).toEqual(["    "]);
  });

  it("recognizes a fence on the list-marker line", () => {
    const md = "- ```sh\n  echo $HOME\n  ```\n\nFormula: \\(x\\)";
    expect(splitCode(md)).toEqual(["", "- ```sh\n  echo $HOME\n  ```\n", "\nFormula: \\(x\\)"]);
    expect(normalizeMath(md)).toBe("- ```sh\n  echo $HOME\n  ```\n\nFormula: $x$");
  });

  it("ends an unclosed fence where its quote ends", () => {
    expect(normalizeMath("> ```\n> code\n\nFormula: \\(x\\)")).toBe("> ```\n> code\n\nFormula: $x$");
  });
});

describe("normalizeMath review cases", () => {
  it("keeps $ inside URLs", () => {
    const md = "见 https://example.com/odata/$metadata 和 <https://a.com/$x> 及 $y$";
    expect(normalizeMath(md)).toBe(md);
    expect(linkify(normalizeMath(md), { math: true })).toBe("见 <https://example.com/odata/$metadata> 和 <https://a.com/$x> 及 $y$");
  });

  it("strips quote prefixes from captured display math", () => {
    expect(normalizeMath("> \\[\n> a+b\n> \\]")).toBe("> $$\n> a+b\n> $$");
  });

  it("keeps explicit \\( \\) math out of the $ heuristics", () => {
    expect(normalizeMath("The \\(n\\)th term")).toBe("The $n$th term");
    expect(normalizeMath("a \\(x +\ny\\) b")).toBe("a $x +\ny$ b");
    expect(normalizeMath("价 \\(\\$5\\)")).toBe('价 ${\\char"24}5$');
  });

  it("protects table math without outer pipes or after inline code", () => {
    expect(normalizeMath("Name | Value\n--- | ---\nnorm | $|x|$")).toBe("Name | Value\n--- | ---\nnorm | $\\vert{}x\\vert{}$");
    expect(normalizeMath("| `c` | $|x|$ |\n| --- | --- |")).toBe("| `c` | $\\vert{}x\\vert{}$ |\n| --- | --- |");
  });
});

describe("linkify", () => {
  it("does not touch URLs inside math", () => {
    const md = "$\\text{see https://a.com/x}$";
    expect(linkify(md, { math: true })).toBe(md);
  });

  it("still links bare URLs outside math", () => {
    expect(linkify("见 https://a.com/x 和 $x$", { math: true })).toBe("见 <https://a.com/x> 和 $x$");
  });

  it("treats $ as text without math", () => {
    expect(linkify("$5 见 https://example.com/path。说明 $10")).toBe("$5 见 <https://example.com/path>。说明 $10");
  });
});
