import Testing
import UIKit
@testable import Jarvis

/// Select Text (#3189): a reply reads as the chat shows it, without the Markdown marks, so a part copied from it is clean;
/// your own message, formulas and code stay exactly as written.
@MainActor
struct SelectableMarkdownTests {
    private func shown(_ markdown: String) -> String { SelectableMarkdown.attributed(markdown).string }

    @Test func inlineMarksGoTheirLookStays() {
        let s = SelectableMarkdown.attributed("今天有 **3 件事**，跑 `swift test`，看 [PR](https://example.com/pr/42)。")
        #expect(s.string == "今天有 3 件事，跑 swift test，看 PR。")
        let bold = s.attribute(.font, at: 4, effectiveRange: nil) as? UIFont
        #expect(bold?.fontDescriptor.symbolicTraits.contains(.traitBold) == true)
        let link = (s.string as NSString).range(of: "PR")
        #expect(s.attribute(.link, at: link.location, effectiveRange: nil) as? URL == URL(string: "https://example.com/pr/42"))
    }

    /// Outside a fence `a*b*c` would be emphasis (`abc`); inside it is code, kept as written and monospaced.
    @Test func fencedCodeLosesItsFenceKeepsItsLines() {
        #expect(shown("a*b*c") == "abc")
        let s = SelectableMarkdown.attributed("新的写法：\n\n```swift\nlet x = a*b*c\n  __init__()\n```\n\n完了")
        #expect(s.string == "新的写法：\n\nlet x = a*b*c\n  __init__()\n\n完了")
        let at = (s.string as NSString).range(of: "a*b*c").location
        let font = s.attribute(.font, at: at, effectiveRange: nil) as? UIFont
        #expect(font?.fontDescriptor.symbolicTraits.contains(.traitMonoSpace) == true)
    }

    /// A shorter or other fence inside a fence is a line of it: ````md holding ```swift … ``` closes on ```` only.
    @Test func aFenceClosesOnlyOnItsOwnKind() {
        let md = "看：\n````md\n```swift\nlet a = 1\n```\n还在里面\n````\n外面"
        #expect(shown(md) == "看：\n```swift\nlet a = 1\n```\n还在里面\n外面")
        #expect(shown("~~~\n```\n~~~~\n后面") == "```\n后面")
        // An info string after a closing-looking fence: still inside.
        #expect(shown("```\na\n```js\nb\n```") == "a\n```js\nb\n")
    }

    @Test func formulasStayAsWritten() {
        #expect(shown("面积 $x*y*z$，还有 \\(a*b*c\\) 和 *重点*") == "面积 $x*y*z$，还有 \\(a*b*c\\) 和 重点")
        #expect(shown("$$\nE = m*c*c\n$$\n**完**") == "$$\nE = m*c*c\n$$\n完")
        #expect(shown("一行的 $$a*b*c$$ 公式") == "一行的 $$a*b*c$$ 公式")
        // `$` in a code span is code.
        #expect(shown("跑 `echo $HOME*` 看看") == "跑 echo $HOME* 看看")
    }

    @Test func indentedCodeStaysAsWritten() {
        #expect(shown("例子：\n\n    let x = a*b*c\n    __init__()\n\n完") == "例子：\n\n    let x = a*b*c\n    __init__()\n\n完")
        // In a list, four spaces in is the item's next level, not code.
        #expect(shown("- 一\n\n    - 二") == "• 一\n\n    • 二")
    }

    @Test func headingsBulletsAndRules() {
        #expect(shown("## 结论\n- 一\n  * 二\n\t+ 三\n1. **四**") == "结论\n• 一\n  • 二\n\t• 三\n1. 四")
        #expect(shown("上面\n\n* * *\n---\n___\n下面") == "上面\n\n* * *\n---\n___\n下面")
    }

    @Test func plainTextIsUnchanged() {
        let text = "PR #42 看完了：逻辑没问题。\n#不是标题\n2 * 3 = 6"
        #expect(shown(text) == text)
    }

    /// Your own message is shown as its bubble shows it: no Markdown at all.
    @Test func yourOwnMessageIsPlain() {
        let text = "Python 里 __init__.py 的 2*3*4 是不是写错了？\n# 不是标题\n- 也不是列表\n```\n照原样\n```"
        #expect(SelectableMarkdown.plain(text).string == text)
        #expect(shown(text) != text) // as a reply it would lose marks
    }

    /// The fonts follow the text size asked for.
    @Test func fontsFollowTheTextSize() {
        let small = UITraitCollection(preferredContentSizeCategory: .small)
        let large = UITraitCollection(preferredContentSizeCategory: .accessibilityExtraLarge)
        func size(_ s: NSAttributedString, _ at: Int) -> CGFloat { (s.attribute(.font, at: at, effectiveRange: nil) as? UIFont)?.pointSize ?? 0 }
        let md = "正文\n```\ncode\n```"
        let a = SelectableMarkdown.attributed(md, traits: small), b = SelectableMarkdown.attributed(md, traits: large)
        #expect(size(b, 0) > size(a, 0))
        #expect(size(b, b.length - 2) > size(a, a.length - 2))
        #expect(size(SelectableMarkdown.plain("字", traits: large), 0) > size(SelectableMarkdown.plain("字", traits: small), 0))
    }
}
