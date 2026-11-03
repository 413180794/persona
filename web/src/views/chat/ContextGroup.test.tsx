import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ContextGroup, contextSummary } from "./ContextGroup";

const cs = [
  { kind: "instructions", title: "给 Agent 的说明", text: "## 怎么做" },
  { kind: "context", title: "前面的聊天", text: "10-31 09:00 张三：构建挂了" },
  { kind: "doc", title: "文档 · 手册", url: "https://example.com/d", text: "第一步" },
];

describe("ContextGroup", () => {
  it("summarizes one context by its title and several by their count", () => {
    expect(contextSummary(cs)).toBe("上下文 · 3 项");
    expect(contextSummary([cs[1]])).toBe("上下文 · 前面的聊天");
    expect(contextSummary([{ kind: "edit", title: "段落", text: "x" }])).toBe("要改的内容 · 段落");
  });

  it("starts folded: one row, no titles or text", () => {
    const html = renderToStaticMarkup(<ContextGroup contexts={cs} />);
    expect(html).toContain("上下文 · 3 项");
    expect(html).toContain('aria-expanded="false"');
    expect(html).not.toContain("前面的聊天");
    expect(html).not.toContain("构建挂了");
  });
});
