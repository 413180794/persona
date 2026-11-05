import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import remarkMath from "remark-math";
import remarkGfm from "remark-gfm";
import remarkCjkFriendly from "remark-cjk-friendly";
import remarkCjkFriendlyParse from "remark-cjk-friendly/parseOnly";
import type { PluggableList } from "unified";
import remarkGfmParse from "./remark-gfm-parse";

// The parse-only plugins must render exactly what the full remark-gfm / remark-cjk-friendly did (#2687).
const SAMPLES: Record<string, string> = {
  table: "| 名称 | 数量 |\n| :-- | --: |\n| 苹果 | 3 |\n| `a|b` | 4 |",
  strikethrough: "这个 ~~删掉~~ 了，~单个~ 也算",
  tasks: "- [ ] 待办\n- [x] 完成\n  - [ ] 子项",
  autolink: "看 https://example.com/a?b=1 和 www.example.com，邮件 me@example.com",
  footnote: "正文[^1]，再一个[^note]。\n\n[^1]: 第一个脚注\n[^note]: 第二个 **脚注**",
  cjk: "这是**「强调」**的文字，*（斜体）*也是，__下划线__（括号）",
  mixed: "# 标题\n\n> 引用 ~~删~~ https://example.com\n\n| a |\n| - |\n| 是**「粗体」**吗 |\n\n$x^2$ 和 [链接](https://example.com)",
};

function html(source: string, plugins: PluggableList): string {
  return renderToStaticMarkup(<ReactMarkdown remarkPlugins={plugins}>{source}</ReactMarkdown>);
}

describe("remark-gfm-parse", () => {
  for (const [name, source] of Object.entries(SAMPLES)) {
    it(`renders ${name} like remark-gfm`, () => {
      expect(html(source, [remarkGfmParse])).toBe(html(source, [remarkGfm]));
    });

    it(`renders ${name} like remark-gfm + remark-cjk-friendly with math`, () => {
      const parsed = html(source, [remarkGfmParse, remarkMath, remarkCjkFriendlyParse]);
      expect(parsed).toBe(html(source, [remarkGfm, remarkMath, remarkCjkFriendly]));
    });
  }

  it("actually parses GFM and CJK emphasis", () => {
    expect(html(SAMPLES.table, [remarkGfmParse])).toContain("<table>");
    expect(html(SAMPLES.strikethrough, [remarkGfmParse])).toContain("<del>删掉</del>");
    expect(html(SAMPLES.tasks, [remarkGfmParse])).toContain('type="checkbox"');
    expect(html(SAMPLES.autolink, [remarkGfmParse])).toContain('href="https://example.com/a?b=1"');
    expect(html(SAMPLES.footnote, [remarkGfmParse])).toContain('data-footnotes="true"');
    expect(html(SAMPLES.cjk, [remarkGfmParse, remarkCjkFriendlyParse])).toContain("<strong>「强调」</strong>");
    // Control: without the CJK plugin CommonMark's flanking rules leave it as literal asterisks.
    expect(html(SAMPLES.cjk, [remarkGfmParse])).not.toContain("<strong>「强调」</strong>");
  });
});
