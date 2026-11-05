import Testing
@testable import PersonaKit

/// A streaming reply cut into finished blocks (`StreamChunks`): what is cut stays the same as the text grows, a code
/// fence or an indented continuation is never cut, and the gaps follow the theme's margins.
struct StreamChunksTests {
    @Test func cutsAfterABlankLineOnceTheNextBlockStarts() {
        let parts = StreamChunks.split("## 标题\n\n第一段。\n\n第二段还在写")
        #expect(parts.done == ["## 标题", "第一段。"])
        #expect(parts.tail == "第二段还在写")
    }

    @Test func aBlankLineAtTheEndIsNotACutYet() {
        // The next block has not started: the blank line may still be inside a list or a fence that comes next.
        let parts = StreamChunks.split("第一段。\n\n")
        #expect(parts.done.isEmpty)
        #expect(parts.tail == "第一段。\n\n")
    }

    @Test func neverCutsInsideACodeFence() {
        let text = "看这段：\n\n```swift\nlet a = 1\n\nlet b = 2\n```\n\n后面"
        let parts = StreamChunks.split(text)
        #expect(parts.done == ["看这段：", "```swift\nlet a = 1\n\nlet b = 2\n```"])
        #expect(parts.tail == "后面")
    }

    @Test func keepsAnIndentedContinuationWithItsBlock() {
        let parts = StreamChunks.split("1. 第一项\n\n   接着第一项\n\n2. 第二项")
        #expect(parts.done == ["1. 第一项\n\n   接着第一项"])
        #expect(parts.tail == "2. 第二项")
    }

    @Test func whatIsCutStaysAsTheTextGrows() {
        let full = "## 看完了\n\n改动没问题。\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n要我留评论吗？"
        var last: [String] = []
        for n in stride(from: 1, through: full.count, by: 3) {
            let done = StreamChunks.split(String(full.prefix(n))).done
            #expect(Array(done.prefix(last.count)) == last, "earlier pieces changed at \(n)")
            last = done
        }
        let all = StreamChunks.split(full)
        #expect((all.done + [all.tail]).joined(separator: "\n\n") == full)
    }

    @Test func gapsAreTheLargerMargin() {
        #expect(StreamChunks.gap(after: "一段", before: "又一段") == 10)
        #expect(StreamChunks.gap(after: "一段", before: "## 标题") == 14)
        #expect(StreamChunks.gap(after: "## 标题", before: "一段") == 6)
        #expect(StreamChunks.gap(after: "| a |\n| 1 |", before: "一段") == 12)
        #expect(StreamChunks.gap(after: "```\ncode\n```", before: "一段") == 12)
    }
}
