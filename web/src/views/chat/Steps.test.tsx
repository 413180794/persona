import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Item } from "../../lib/types";
import { CompactDivider } from "./Steps";

describe("CompactDivider", () => {
  it("is one separator with what is known, the folded notices on hover", () => {
    const item: Item = { id: "n1", seq: 1, role: "system", kind: "notice", text: "上下文已压缩", compact: { trigger: "manual", pre_tokens: 181_000, notes: ["压缩上下文", "上下文已压缩"] } };
    const html = renderToStaticMarkup(<CompactDivider item={item} />);
    expect(html).toContain('role="separator"');
    expect(html).toContain(">以上内容已压缩 · 手动 · 压缩前 18.1 万 token<");
    expect(html).toContain('title="压缩上下文\n上下文已压缩"');
  });
});
