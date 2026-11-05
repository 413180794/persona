import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { UserMessage } from "../../lib/turns";
import { Composer } from "./Composer";
import { QuoteBar, QuoteBlock, quoteSource } from "./Quote";
import { UserBubble } from "./SegmentView";

vi.mock("../../lib/links", async (orig) => ({ ...(await orig<typeof import("../../lib/links")>()), useContexts: () => [] }));

afterEach(() => vi.unstubAllGlobals());

const reply = "## 结论\n\n- **第一条**：改好了\n- 第二条 `code`\n\n> 里面的引用";

describe("引用 above the composer", () => {
  it("says who and when, the words without Markdown, and × to drop it", () => {
    expect(quoteSource({ text: "x", from: "Persona", ts: new Date(2026, 10, 5, 16, 45).toISOString() })).toMatch(/^引用 Persona · (\d+月\d+日 )?16:45$/);
    expect(quoteSource({ text: "x" })).toBe("引用");
    const html = renderToStaticMarkup(<QuoteBar quote={{ text: reply, from: "Persona" }} onRemove={() => {}} />);
    expect(html).toContain("结论 第一条：改好了 第二条 code 里面的引用");
    const summary = /<span class="line-clamp-2[^"]*">([^<]*)<\/span>/.exec(html)?.[1] ?? "";
    expect(summary).toBe("结论 第一条：改好了 第二条 code 里面的引用");
    expect(html).toContain("line-clamp-2");
    expect(html).toContain('aria-label="取消引用"');
  });

  it("keeps the input empty: the quote waits in the bar, not as > lines", () => {
    const store = new Map([["quote:t", JSON.stringify({ text: "第一行\n第二行", from: "Persona" })]]);
    vi.stubGlobal("localStorage", { getItem: (k: string) => store.get(k) ?? null });
    const html = renderToStaticMarkup(<Composer draftKey="t" placeholder="…" onSend={async () => true} />);
    expect(html).toContain('aria-label="引用的消息"');
    expect(html).toContain("第一行 第二行");
    expect(html).toMatch(/<textarea[^>]*><\/textarea>/);
    // a quote alone can be sent
    expect(html).toMatch(/<button[^>]*aria-label="发送"(?![^>]*disabled)/);
  });
});

describe("引用 in a sent bubble", () => {
  const msg = (text: string) => ({ id: "u1", text, images: [], contexts: [] }) as unknown as UserMessage;

  it("draws a leading > block as a folded quote card over the words", () => {
    const html = renderToStaticMarkup(<UserBubble msg={msg("> 第一行\n> \n> **第二行**\n\n我的看法")} mid="m" sid="s" />);
    expect(html).toContain("data-quote");
    expect(html).toContain("line-clamp-3");
    expect(html).toContain("第一行 第二行");
    expect(html).toContain("我的看法");
    expect(html).not.toContain("&gt;");
  });

  it("leaves a message without a leading quote as it was", () => {
    const html = renderToStaticMarkup(<UserBubble msg={msg("先说一句\n> 不是开头的引用")} mid="m" sid="s" />);
    expect(html).not.toContain("data-quote");
    expect(html).toContain("&gt; 不是开头的引用");
  });

  it("is a quote card alone when nothing was typed under it", () => {
    const html = renderToStaticMarkup(<QuoteBlock text="只引用了这句" />);
    expect(html).toContain("只引用了这句");
    expect(html).not.toContain("展开引用");
  });
});
