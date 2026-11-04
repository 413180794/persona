import { describe, expect, it } from "vitest";
import { plainMarkdown } from "./plain-markdown";

describe("plainMarkdown (#1508)", () => {
  it("drops headings, emphasis and list markers", () => {
    expect(plainMarkdown("## 根因\nhub 和 node 的 **会话** 不同步")).toBe("根因 hub 和 node 的 会话 不同步");
    expect(plainMarkdown("**网页的改动**\n- **底部的「收起」**：好了\n1. 第二条\n- [x] 勾上")).toBe("网页的改动 底部的「收起」：好了 第二条 勾上");
    expect(plainMarkdown("> 引用 ~~删掉~~ *斜体* _也是_")).toBe("引用 删掉 斜体 也是");
  });

  it("keeps code's words without backticks, and its * and _", () => {
    expect(plainMarkdown("调 `POST /api/v1/sessions/{mid}/{sid}/interrupt` 停下")).toBe("调 POST /api/v1/sessions/{mid}/{sid}/interrupt 停下");
    expect(plainMarkdown("改 `a_b_c` 和 `**kw`")).toBe("改 a_b_c 和 **kw");
    expect(plainMarkdown("```go\nx := 1\n```")).toBe("x := 1");
    expect(plainMarkdown("snake_case_name 和 2 * 3")).toBe("snake_case_name 和 2 * 3");
  });

  it("shows a link as its text", () => {
    expect(plainMarkdown("看 [PR](https://github.com/o/r/pull/1) 和 <https://example.com>")).toBe("看 PR 和 https://example.com");
  });

  it("drops marks a clip left unpaired", () => {
    expect(plainMarkdown("剪在 **加粗中间…")).toBe("剪在 加粗中间…");
    expect(plainMarkdown("剪在 `代码中间…")).toBe("剪在 代码中间…");
  });

  it("flattens tables and rules", () => {
    expect(plainMarkdown("| a | b |\n| - | - |\n| 1 | 2 |\n\n---\n完")).toBe("a · b 1 · 2 完");
  });

  it("is empty for nothing", () => {
    expect(plainMarkdown(undefined)).toBe("");
    expect(plainMarkdown("  \n")).toBe("");
  });
});
