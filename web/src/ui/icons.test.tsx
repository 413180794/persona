import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Icon } from "./icons";

describe("device icons", () => {
  it("renders distinct, nonempty phone, watch, computer and browser glyphs", () => {
    const glyphs = ["phone", "watch", "machine", "monitor", "web"].map((name) => {
      const html = renderToStaticMarkup(<Icon name={name} />);
      const glyph = html.slice(html.indexOf(">") + 1, html.lastIndexOf("</svg>"));
      expect(glyph).toMatch(/<(path|rect|circle|line|polyline|polygon)\b/);
      return glyph;
    });
    expect(new Set(glyphs).size).toBe(glyphs.length);
  });

  it.each(["phone", "watch"])("keeps %s in the shared icon design system", (name) => {
    const html = renderToStaticMarkup(<Icon name={name} className="text-ink-3" />);
    expect(html).toContain('viewBox="0 0 24 24"');
    expect(html).toContain('stroke="currentColor"');
    expect(html).toContain('stroke-linecap="round"');
    expect(html).toContain('stroke-linejoin="round"');
    expect(html).toContain('aria-hidden="true"');
    expect(html).toContain('class="text-ink-3"');
  });
});
