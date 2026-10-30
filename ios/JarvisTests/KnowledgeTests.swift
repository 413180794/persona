import Foundation
import Testing
@testable import Jarvis

struct KnowledgeTests {
    @Test func timelineLinesSplitIntoWhenSourceAndText() {
        let l = KBLogLine("- 2026-10-30 23:16 · Agent · 迭代需求实现 · 一台电脑 · Round 1 is on the dev set.")
        #expect(l.when == "2026-10-30 23:16")
        #expect(l.source == "Agent · 迭代需求实现 · 一台电脑")
        #expect(l.text == "Round 1 is on the dev set.")
        #expect(l.shortWhen(now: ISO8601DateFormatter().date(from: "2026-11-01T00:00:00Z")!) == "10-30 23:16")
        #expect(l.shortWhen(now: ISO8601DateFormatter().date(from: "2027-02-01T00:00:00Z")!) == "2026-10-30 23:16")
        let plain = KBLogLine("- 只有一句话")
        #expect(plain.when.isEmpty && plain.source.isEmpty && plain.text == "只有一句话")
    }

    @Test func searchHitsKnowTheirEntryAndFile() {
        let a = KBHit(path: "chats/lark-1/summary.md", line: "- 10-29：修复了 MCP")
        #expect(a.entry?.kind == .chats && a.entry?.id == "lark-1")
        #expect(a.source == "档案" && a.text == "10-29：修复了 MCP")
        let b = KBHit(path: "docs/abc.json", line: "  \"title\": \"设计稿\",")
        #expect(b.entry?.kind == .docs && b.entry?.id == "abc")
        #expect(b.source == "资料" && b.text == "设计稿")
        let c = KBHit(path: "chats/lark-1/messages/2026-10-30.md", line: "10:01 张三：MCP 挂了")
        #expect(c.source == "聊天记录 · 2026-10-30")
        #expect(KBHit(path: "README.md", line: "x").entry == nil)
        // One row per entry, its 档案 lines before its 资料 lines, repeats dropped.
        let groups = KBSearchGroup.make([KBHit(path: "chats/lark-1/meta.json", line: "\"MCP\","), a, a, b])
        #expect(groups.map(\.key) == ["chats/lark-1", "docs/abc"])
        #expect(groups[0].hits.map(\.source) == ["档案", "资料"])
    }
}

extension KnowledgeTests {
    @Test func theProfileLeavesOutWhatTheHeaderShows() {
        let md = "# 张三\n\n一起做评测。\n\n重要级 P0 · 与我相关性 95 · 评测\n\n## 项目\n- A"
        #expect(KnowledgeDetailView.profile(md, oneLine: "一起做评测。") == "## 项目\n- A")
    }
}
