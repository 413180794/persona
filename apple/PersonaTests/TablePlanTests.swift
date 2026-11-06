import Foundation
import Testing
@testable import Jarvis

/// Column widths of a reply's table (`TablePlan`): natural when it fits, long columns wrap before the table scrolls.
struct TablePlanTests {
    @Test func fitsAsIs() {
        let p = TablePlan.fit(natural: [20, 80, 100], minimum: [20, 30, 30], available: 300)
        #expect(p == TablePlan(widths: [20, 80, 100], scrolls: false))
    }

    /// 「# / 事项 / 裁定」 with a long 「裁定」: # and 「事项」 keep their width, 「裁定」 takes the rest and wraps.
    @Test func longColumnWraps() {
        let p = TablePlan.fit(natural: [16, 120, 900], minimum: [16, 30, 30], available: 300)
        #expect(!p.scrolls)
        #expect(p.widths[0] == 16 && p.widths[1] == 120)
        #expect(p.widths[2] == 164)
        #expect(p.widths.reduce(0, +) <= 300)
    }

    /// Two long columns share in proportion; none goes under what cannot wrap.
    @Test func longColumnsShare() {
        let p = TablePlan.fit(natural: [16, 400, 800], minimum: [16, 150, 30], available: 300)
        #expect(!p.scrolls && p.widths[0] == 16 && p.widths[1] >= 150 && p.widths.reduce(0, +) <= 300)
    }

    /// Too many columns for the screen: it scrolls, each column capped so long text still wraps.
    @Test func scrollsWhenNothingFits() {
        let p = TablePlan.fit(natural: Array(repeating: 60, count: 6) + [500], minimum: Array(repeating: 50, count: 7), available: 300)
        #expect(p.scrolls && p.widths.last == TablePlan.scrollCap)
    }

    @Test func rowsOfMarkdown() {
        let rows = MarkdownTableText.rows("| # | 事项 | 裁定 |\n| --- | :-: | ---: |\n| 1 | **小组件** | 见 [文档](https://x) 和 `a\\|b` |")
        #expect(rows == [["#", "事项", "裁定"], ["1", "小组件", "见 文档 和 a|b"]])
    }
}
