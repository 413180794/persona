import { describe, expect, it } from "vitest";
import { attentionReason, externalLink, sessionPreview } from "./format";
import type { Session } from "./types";

const session = (o: Partial<Session>): Session => ({ machine_id: "m", session_id: "s", cwd: "/x", status: "idle", ...o }) as Session;

// A botmux session's reason for waiting and its Feishu topic (attention_reason, external_url; docs/engines.md「botmux」).
describe("sessionPreview", () => {
  it("shows why it waits while it needs you", () => {
    expect(sessionPreview(session({ status: "blocked", state: "needs_you", last_text: "正在跑全量", attention_reason: "本轮执行失败，再发一条消息继续" }))).toBe("本轮执行失败，再发一条消息继续");
  });
  it("keeps the last words otherwise", () => {
    expect(sessionPreview(session({ status: "working", state: "working", last_text: "在补测试", attention_reason: "旧的原因" }))).toBe("在补测试");
    expect(sessionPreview(session({ status: "blocked", state: "needs_you", last_text: "在等你", attention_reason: "  " }))).toBe("在等你");
    expect(sessionPreview(session({}))).toBe("");
  });
});

describe("attentionReason", () => {
  it("is there only while it needs you", () => {
    expect(attentionReason(session({ status: "blocked", state: "needs_you", attention_reason: " 等你确认 " }))).toBe("等你确认");
    expect(attentionReason(session({ status: "offline", state: "offline", attention_reason: "等你确认" }))).toBeUndefined();
    expect(attentionReason(session({ status: "blocked", state: "needs_you" }))).toBeUndefined();
  });
});

describe("externalLink", () => {
  it("takes an https link", () => {
    expect(externalLink(session({ external_url: "https://applink.example.com/thread?x=1" }))).toBe("https://applink.example.com/thread?x=1");
  });
  it("drops anything else", () => {
    for (const url of ["http://applink.example.com/t", "javascript:alert(1)", "lark://open", "not a url", ""]) {
      expect(externalLink(session({ external_url: url })), url).toBeUndefined();
    }
    expect(externalLink(undefined)).toBeUndefined();
  });
});
