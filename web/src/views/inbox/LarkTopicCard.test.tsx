import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { LarkMessage } from "../../lib/inbox";
import { LarkTopicReplies } from "../../lib/lark-topic-replies";
import { LarkTopicCard, replyLine } from "./LarkTopicCard";

const queue = () => new LarkTopicReplies(async () => []);

describe("LarkTopicCard", () => {
  it("says a reply in one line: text, else what its parts are", () => {
    expect(replyLine({ id: "r", position: "1", text: "  好的\n明天  " })).toBe("好的 明天");
    expect(replyLine({ id: "r", position: "1", parts: [{ kind: "image" }] })).toBe("[图片]");
    expect(replyLine({ id: "r", position: "1", parts: [{ kind: "file", text: "a.pdf" }] })).toBe("[文件] a.pdf");
    expect(replyLine({ id: "r", position: "1", recalled: true, text: "x" })).toBe("消息已撤回");
  });

  it("draws the poster, the topic and placeholders for replies not read yet", () => {
    const topic: LarkMessage = { id: "t1", position: "9", sender_name: "示例 A", sender_type: "bot", reply_count: 5, created_at: "2026-01-02T03:04:05Z" };
    const html = renderToStaticMarkup(<LarkTopicCard topic={topic} replies={queue()} onOpen={() => {}}><p>正文</p></LarkTopicCard>);
    expect(html).toContain("示例 A");
    expect(html).toContain("机器人");
    expect(html).toContain("正文");
    expect(html).toContain("查看更早 3 条回复");
    expect(html).toContain('data-mid="t1"');
  });

  it("has no reply preview without replies, nor on a recalled topic", () => {
    const none = renderToStaticMarkup(<LarkTopicCard topic={{ id: "t", position: "1", sender_name: "B" }} replies={queue()}><p /></LarkTopicCard>);
    expect(none).not.toContain("lark-topic-replies");
    const recalled = renderToStaticMarkup(<LarkTopicCard topic={{ id: "t", position: "1", sender_name: "B", recalled: true, reply_count: 2 }} replies={queue()}><p /></LarkTopicCard>);
    expect(recalled).not.toContain("lark-topic-replies");
  });
});
