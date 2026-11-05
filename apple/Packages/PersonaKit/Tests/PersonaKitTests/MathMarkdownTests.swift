import Foundation
import Testing
@testable import PersonaKit

struct MathMarkdownTests {
    @Test func displayBlocksBecomeMathFences() {
        #expect(MathMarkdown.rewrite("前\n$$\na^2 + b^2 = c^2\n$$\n后") == "前\n```math\na^2 + b^2 = c^2\n```\n后")
        #expect(MathMarkdown.rewrite("\\[\n\\int_0^1 x\\,dx\n\\]") == "```math\n\\int_0^1 x\\,dx\n```")
        #expect(MathMarkdown.rewrite("$$E = mc^2$$") == "```math\nE = mc^2\n```")
        #expect(MathMarkdown.rewrite("  $x^2$  ") == "```math\nx^2\n```") // a line that is one formula
    }

    @Test func inlineFormulasBecomeImages() {
        let out = MathMarkdown.rewrite("面积是 $\\pi r^2$，周长 \\(2\\pi r\\)。")
        #expect(out == "面积是 ![\\pi r^2](\(MathMarkdown.url("\\pi r^2")))，周长 ![2\\pi r](\(MathMarkdown.url("2\\pi r")))。")
        let url = URL(string: MathMarkdown.url("\\frac{a}{b} [x]"))!
        #expect(MathMarkdown.tex(url) == "\\frac{a}{b} [x]")
        #expect(MathMarkdown.rewrite("集合 $[0, 1]$ 上").contains("![(0, 1)]("))
    }

    @Test func dollarsThatAreMoney() {
        for text in ["花了 $5 和 $10", "$5-$10 之间", "价格 $ 5 $", "转义 \\$x\\$ 不算", "a $ b", "大约 $10 一个月：$ 左右"] {
            #expect(MathMarkdown.rewrite(text) == text, "\(text)")
        }
    }

    @Test func moneyThenAFormula() {
        let out = MathMarkdown.rewrite("大约 $10 一个月，公式是 $x^2$。")
        #expect(out == "大约 $10 一个月，公式是 ![x^2](\(MathMarkdown.url("x^2")))。")
        #expect(MathMarkdown.rewrite("单位 $\\text{米}$ 可以").contains("persona-math:"))
    }

    @Test func codeIsLeftAlone() {
        let md = "用 `$x$` 写公式\n```\n$$\nnot math\n$$\n```\n    $y$ 缩进的代码"
        #expect(MathMarkdown.rewrite(md) == md)
        #expect(MathMarkdown.rewrite("没有公式的文字") == "没有公式的文字")
    }

    @Test func unclosedBlockStaysAsItWas() {
        #expect(MathMarkdown.rewrite("$$\na + b\n还没写完") == "$$\na + b\n还没写完")
    }

}
