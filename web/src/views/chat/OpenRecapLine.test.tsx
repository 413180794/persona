import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Item } from "../../lib/types";
import { OpenRecapLine } from "./Lines";

const recap = (n: number, text: string): Item => ({ id: `r${n}#0`, seq: n, role: "system", kind: "recap", text });

describe("OpenRecapLine", () => {
  it("is a plain block in the chat's flow: the newest recap in full, nothing pinned over the messages", () => {
    const html = renderToStaticMarkup(<OpenRecapLine open={{ line: "r1#0", earlier: [], newest: recap(1, "两条建议都改完了") }} onClose={() => {}} />);
    expect(html).toContain("离开期间回顾");
    expect(html).toContain("两条建议都改完了");
    expect(html).toContain('aria-label="关闭回顾"');
    expect(html).not.toMatch(/\b(fixed|sticky|absolute)\b/);
    expect(html).not.toContain("点击查看"); // a single recap: no chip next to it
  });

  it("keeps the older recaps folded with it as their chip above it", () => {
    const html = renderToStaticMarkup(
      <OpenRecapLine open={{ line: "rcr1#0", earlier: [recap(1, "一"), recap(2, "二")], newest: recap(3, "三") }} onClose={() => {}} />,
    );
    expect(html).toContain("2 次离开期间回顾，点击查看");
    expect(html.indexOf("点击查看")).toBeLessThan(html.indexOf("三"));
  });
});
