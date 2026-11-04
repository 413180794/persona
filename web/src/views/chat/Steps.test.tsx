import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Segment, Step } from "../../lib/turns";
import type { Item } from "../../lib/types";
import { WorkRow, WorkTrace } from "./Steps";

const use = (n: number): Item => ({ id: `u${n}`, seq: n, role: "assistant", kind: "tool_use", tool_name: "Bash", tool_use_id: `c${n}`, tool_input: { command: `echo ${n}` } });
const seg = (tools: number): Segment => {
  const steps: Step[] = Array.from({ length: tools }, (_, i) => ({ kind: "tool", id: `u${i}`, use: use(i), images: [] }));
  return { id: "s", steps, tools, thoughts: 0, replies: 0, folds: [], start: "2026-11-04T10:00:00Z", end: "2026-11-04T10:01:58Z" };
};
const noop = () => {};

describe("WorkTrace (#1486)", () => {
  it("folds from its end however few steps it has: 收起 with an up arrow after the last step", () => {
    const html = renderToStaticMarkup(<WorkTrace seg={seg(4)} live={false} mid="m" sid="s" onFold={noop} />);
    const fold = html.lastIndexOf('aria-label="收起执行过程"');
    expect(fold).toBeGreaterThan(html.lastIndexOf("echo 3"));
    expect(html.slice(fold)).toMatch(/^aria-label="收起执行过程" aria-expanded="true"[^>]*>收起<svg[^>]*rotate\(180deg\)/);
    expect(renderToStaticMarkup(<WorkTrace seg={seg(1)} live mid="m" sid="s" onFold={noop} />)).toContain(">收起<svg");
  });

  it("makes the hairline a button too: a wider strip that folds from the end, out of the tab order", () => {
    const html = renderToStaticMarkup(<WorkTrace seg={seg(2)} live={false} mid="m" sid="s" onFold={noop} />);
    expect(html.match(/data-fold-from-end/g)).toHaveLength(2); // ChatView holds the row's end for both
    const rail = html.slice(0, html.indexOf("</button>"));
    expect(rail).toContain('tabindex="-1"');
    expect(rail).toContain('aria-label="收起执行过程"');
    expect(rail).toContain("cursor-pointer");
    expect(rail).toMatch(/w-\[11px\]/);
    expect(rail).toContain("group-hover/rail:bg-ink-3");
  });
});

describe("WorkRow", () => {
  it("starts folded: the header says it is closed and the trace is not drawn", () => {
    const html = renderToStaticMarkup(<WorkRow seg={seg(4)} live={false} now={0} mid="m" sid="s" expandFold={async () => {}} />);
    expect(html).toContain('aria-expanded="false"');
    expect(html).not.toContain("收起执行过程");
  });
});
