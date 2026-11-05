import Foundation
import Testing
@testable import PersonaKit

/// One rule for where a link ends, everywhere links and paths are found (`Links`): at whitespace, at any non-ASCII
/// character (CJK, full-width punctuation), without trailing punctuation or an unpaired closing bracket. GFM's autolink
/// (MarkdownUI) ran on to the next space: a link in Chinese prose took the rest of the paragraph with it.
struct LinkTests {
    private func found(_ s: String) -> [String] { Links.find(in: s).map(\.absoluteString) }

    @Test func endsAtCJKAndFullWidthPunctuation() {
        #expect(found("对比页（http://192.0.2.10:23140/），出来我转给你拍板。拍板后整块开发") == ["http://192.0.2.10:23140/"])
        #expect(found("看这里http://example.com/a中文路径") == ["http://example.com/a"])
        for p in ["）", "。", "，", "、", "；", "：", "」", "』", "】", "》", "！", "？"] {
            #expect(found("链接 https://example.com/x\(p)后面") == ["https://example.com/x"], "\(p)")
        }
        #expect(found("a https://example.com/x b") == ["https://example.com/x"])
        #expect(found("https://example.com/x\nnext line") == ["https://example.com/x"])
    }

    @Test func trailingPunctuationAndBrackets() {
        #expect(found("See https://example.com/a.") == ["https://example.com/a"])
        #expect(found("Is it https://example.com/a?") == ["https://example.com/a"])
        #expect(found("one https://example.com/a, two https://example.com/b; three https://example.com/c!") ==
                ["https://example.com/a", "https://example.com/b", "https://example.com/c"])
        #expect(found("(see https://example.com/a)") == ["https://example.com/a"])
        #expect(found("https://en.wikipedia.org/wiki/Foo_(bar)") == ["https://en.wikipedia.org/wiki/Foo_(bar)"])
        #expect(found("(https://en.wikipedia.org/wiki/Foo_(bar))") == ["https://en.wikipedia.org/wiki/Foo_(bar)"])
        #expect(found("**https://example.com/a**") == ["https://example.com/a"])
        #expect(found("https://example.com/search?q=a&b=c#frag.") == ["https://example.com/search?q=a&b=c#frag"])
    }

    @Test func ipPortPathAndMixedText() {
        #expect(found("服务在 http://192.0.2.7:8080/api/v1/items?x=1 上") == ["http://192.0.2.7:8080/api/v1/items?x=1"])
        #expect(found("mixed English http://192.0.2.7:8080/ and 中文") == ["http://192.0.2.7:8080/"])
        #expect(found("www.example.com/docs，再看") == ["http://www.example.com/docs"])
        #expect(found("no link: http:// or ftp://example.com").isEmpty)
    }

    /// Markdown gets its bare links as <autolinks>, so cmark never extends them; code and existing links stay as they are.
    @Test func markdownWrapsBareLinksOnly() {
        #expect(Links.markdown("对比页（http://192.0.2.10:23140/），出来") == "对比页（<http://192.0.2.10:23140/>），出来")
        #expect(Links.markdown("[文档](https://example.com/a) 和 <https://example.com/b>") == "[文档](https://example.com/a) 和 <https://example.com/b>")
        #expect(Links.markdown("`curl https://example.com/a`，然后") == "`curl https://example.com/a`，然后")
        #expect(Links.markdown("```\ncurl https://example.com/a\n```\n看 https://example.com/b。") ==
                "```\ncurl https://example.com/a\n```\n看 <https://example.com/b>。")
        #expect(Links.markdown("| 1 | https://example.com/a |") == "| 1 | <https://example.com/a> |")
        #expect(Links.markdown("见 www.example.com。") == "见 [www.example.com](http://www.example.com)。")
        #expect(Links.markdown("没有链接") == "没有链接")
    }

}
