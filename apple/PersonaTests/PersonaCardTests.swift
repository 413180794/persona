import Foundation
import Testing
@testable import Jarvis

/// The assistant's ```persona-card blocks (#886): what the app draws, and what stays a code block.
@MainActor
struct PersonaCardTests {
    @Test func stackedBarWithTotalAndRest() throws {
        let c = try #require(PersonaCard.parse("""
        {"type":"stacked_bar","title":"今天的热量分配","subtitle":"推荐全天摄入","total":{"value":2250,"unit":"kcal"},
         "segments":[{"label":"早餐","value":555,"note":"已吃"},{"label":"午餐","value":900},{"label":"晚餐","value":750}],
         "rest":{"label":"弹性空间","value":45}}
        """))
        #expect(c.type == .stackedBar)
        #expect(c.parts.map(\.label) == ["早餐", "午餐", "晚餐", "弹性空间"])
        #expect(c.sum == 2250)
        #expect(c.headline == .init(value: 2250, unit: "kcal"))
    }

    @Test func stackedBarWithoutTotalSumsItsParts() throws {
        let c = try #require(PersonaCard.parse(#"{"type":"stacked_bar","unit":"分钟","segments":[{"label":"深睡","value":90},{"label":"浅睡","value":240}]}"#))
        #expect(c.headline == .init(value: 330, unit: "分钟"))
    }

    @Test func statAndTrends() throws {
        let s = try #require(PersonaCard.parse(#"{"type":"stat","title":"今天步数","value":8432,"unit":"步","note":"比昨天 +12%"}"#))
        #expect(s.headline == .init(value: 8432, unit: "步"))
        let l = try #require(PersonaCard.parse(#"{"type":"line","unit":"小时","points":[{"label":"周一","value":6.5},{"label":"周二","value":7.2}]}"#))
        #expect(l.headline == nil)
        #expect(PersonaCard.parse(#"{"type":"bar","points":[{"label":"今天","value":3}]}"#) != nil)
        let twice = try #require(PersonaCard.parse(#"{"type":"bar","points":[{"label":"周一","value":3},{"label":"周一","value":5}]}"#))
        #expect(twice.points?.count == 2) // drawn by position, so a repeated label is still two bars
    }

    @Test func whatStaysACodeBlock() {
        let notCards = [
            "",
            "{\"type\":\"stacked_bar\",\"segments\":[{\"label\":\"早餐\"", // still streaming
            #"{"type":"pie","segments":[{"label":"a","value":1}]}"#, // a type the app does not know
            #"{"type":"stat","title":"no value"}"#,
            #"{"type":"stacked_bar","segments":[]}"#,
            #"{"type":"stacked_bar","segments":[{"label":"a","value":0}]}"#, // nothing to share out
            #"{"type":"stacked_bar","segments":[{"label":"a","value":5},{"label":"b","value":-1}]}"#,
            #"{"type":"line","points":[{"label":"周一","value":1}]}"#, // a line needs two points
            #"{"type":"stat","value":"2250"}"#, // numbers are numbers
        ]
        for text in notCards { #expect(PersonaCard.parse(text) == nil, "\(text)") }
    }

    @Test func numbersAreGroupedWithAtMostOneDecimal() {
        let grouping = Locale.current.groupingSeparator ?? ","
        #expect(PersonaCard.number(2250) == "2\(grouping)250")
        #expect(PersonaCard.number(7.25).count <= 3)
        #expect(PersonaCard.number(45) == "45")
    }
}
