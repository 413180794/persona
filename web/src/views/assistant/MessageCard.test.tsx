import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { FeedItem } from "../../lib/feed";
import { FeedLineView } from "./Tasks";

// The card 「按任务进度起草的回复」 (#1519): drawn from what the hub records on it, so a reload keeps it sent.
describe("a message card", () => {
  const card = (body: Record<string, unknown> = {}): FeedItem => ({
    id: "m1", conversation: "c1", at: new Date().toISOString(), kind: "message", ref: "example:1",
    body: { source: "github", sender: "reviewer", chat: "example-org/example-repo#12", text: "clearBadges 删记录时不广播？", draft: "确实是 bug，谢谢指出", ...body },
  });

  it("offers the draft to send while it is not sent", () => {
    const html = renderToStaticMarkup(<FeedLineView item={card()} />);
    expect(html).toContain("按任务进度起草的回复");
    expect(html).toContain("<textarea");
    expect(html).toContain("发送");
  });

  it("shows what went out, and no 发送, once the hub says it was sent", () => {
    const html = renderToStaticMarkup(<FeedLineView item={card({ sent_at: "2026-11-04T10:23:00Z", sent: "确实是 bug，谢谢指出（改过）" })} />);
    expect(html).toContain("已以你的名义回复");
    expect(html).toContain("确实是 bug，谢谢指出（改过）");
    expect(html).not.toContain("<textarea");
    expect(html).not.toContain("发送");
    expect(html).not.toContain("稍后");
  });

  it("says a card sent before the hub recorded it is sent, with its draft", () => {
    const html = renderToStaticMarkup(<FeedLineView item={card({ sent_at: "2026-11-04T10:23:00Z" })} />);
    expect(html).toContain("已以你的名义回复");
    expect(html).toContain("确实是 bug，谢谢指出");
    expect(html).not.toContain("发送");
  });

  it("holds a draft with a blank to fill in", () => {
    const html = renderToStaticMarkup(<FeedLineView item={card({ draft: "确实是 bug，【预计今晚 / 几点前】修好" })} />);
    expect(html).toContain("草稿里还有没填的「【预计今晚 / 几点前】」");
    expect(html).toMatch(/<button[^>]*disabled[^>]*>(?:(?!<\/button>).)*发送/);
  });
});
