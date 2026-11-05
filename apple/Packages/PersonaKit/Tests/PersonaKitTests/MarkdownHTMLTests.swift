import Testing
@testable import PersonaKit

/// README-style HTML turned into markdown MarkdownUI can render.
struct MarkdownHTMLTests {
    @Test func imageTableBecomesMarkdownTable() {
        let md = """
        # Title

        <table>
          <tr>
            <td><img src="docs/a.png" width="200" alt="A"></td>
            <td><img src="docs/b.png" width="200" alt="B"></td>
          </tr>
          <tr>
            <td align="center">first</td>
            <td align="center">second | more</td>
          </tr>
        </table>

        text
        """
        let out = MarkdownHTML.simplify(md)
        #expect(out.contains("| ![A](docs/a.png) | ![B](docs/b.png) |"))
        #expect(out.contains("| --- | --- |"))
        #expect(out.contains("| first | second \\| more |"))
        #expect(!out.contains("<td"))
        #expect(out.hasPrefix("# Title"))
        #expect(out.hasSuffix("text"))
    }

    @Test func blockTagsKeepTheirTextAndLinks() {
        let out = MarkdownHTML.simplify("<p align=\"center\"><a href=\"https://x.dev\">site</a><br/>hi &amp; bye</p>")
        #expect(out == "[site](https://x.dev) hi & bye")
    }

    @Test func codeFencesAndPlainMarkdownStayAsTheyAre() {
        let md = "a <b>inline</b>\n```html\n<table><tr><td>x</td></tr></table>\n```\n<https://auto.link>"
        #expect(MarkdownHTML.simplify(md) == md)
    }

    @Test func emptyTagLinesDrop() {
        #expect(MarkdownHTML.simplify("one\n<div align=\"center\">\ntwo\n</div>") == "one\ntwo")
    }
}
