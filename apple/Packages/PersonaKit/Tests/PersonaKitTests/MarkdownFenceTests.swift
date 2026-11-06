import Testing
@testable import PersonaKit

/// A code fence ends only on a fence of the same character, at least as long, with nothing after it (CommonMark), in
/// every PersonaKit pass that skips code blocks (#3771).
struct MarkdownFenceTests {
    @Test func opensOnThreeOrMoreBackticksOrTildes() {
        #expect(MarkdownFence.opening("```swift") == MarkdownFence(character: "`", count: 3))
        #expect(MarkdownFence.opening("````md") == MarkdownFence(character: "`", count: 4))
        #expect(MarkdownFence.opening("~~~") == MarkdownFence(character: "~", count: 3))
        #expect(MarkdownFence.opening("``code``") == nil)
        #expect(MarkdownFence.opening("```code```") == nil)
        #expect(MarkdownFence.opening("~~~ a`b") == MarkdownFence(character: "~", count: 3))
        #expect(MarkdownFence.opening("text") == nil)
    }

    @Test func closesOnlyOnTheSameCharacterAtLeastAsLongWithNothingAfter() {
        let four = MarkdownFence.opening("````md")!
        #expect(!four.isClosed(by: "```"))
        #expect(!four.isClosed(by: "```html"))
        #expect(!four.isClosed(by: "~~~~"))
        #expect(four.isClosed(by: "````"))
        #expect(four.isClosed(by: "`````"))
        let three = MarkdownFence.opening("```")!
        #expect(!three.isClosed(by: "```js"))
        #expect(three.isClosed(by: "```"))
        #expect(three.isClosed(by: "```  "))
    }

    @Test func streamChunksNeverCutsInsideALongerFence() {
        let block = "````md\n```html\na\n\n<p>x</p>\n```\n````"
        let parts = StreamChunks.split("前\n\n" + block + "\n\n后")
        #expect(parts.done == ["前", block])
        #expect(parts.tail == "后")
        // A ```js line inside a ``` block does not end it either.
        let inner = "```\n```js\n\nlet a = 1\n```"
        #expect(StreamChunks.split("前\n\n" + inner + "\n\n后").done == ["前", inner])
    }

    @Test func rewritesLeaveTheWholeLongerFenceAlone() {
        // Old: the inner ``` ended the block, so the next line was rewritten and ```` opened a new block.
        let links = "````md\n```\ncurl https://example.com/a\n````"
        #expect(Links.markdown(links) == links)
        let math = "````md\n```\n$$\nnot math\n$$\n````"
        #expect(MathMarkdown.rewrite(math) == math)
        let html = "````md\n```\n<div align=\"center\">\n````"
        #expect(MarkdownHTML.simplify(html) == html)
        let paths = "````md\n```\n见 src/a.ts:40\n````"
        #expect(PathMarkdown(paths).paths.isEmpty)
    }
}
