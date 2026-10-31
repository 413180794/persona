import Testing
@testable import BeautifulUI

@Suite struct InsightCardsTests {
    @Test func smoothingKeepsEndpointsAndPassesThroughSamples() {
        let values = [1.0, 3.0, 2.0, 5.0]
        let out = InsightCards.smooth(values, perSegment: 4)
        #expect(out.count == 3 * 4 + 1)
        #expect(out.first == 1 && out.last == 5)
        #expect(out[4] == 3 && out[8] == 2)
    }

    @Test func shortSeriesAreLeftAlone() {
        #expect(InsightCards.smooth([1, 2]) == [1, 2])
    }

    @Test func percentsCarryTheirSign() {
        #expect(InsightCards.percent(1.15) == "+1.15%")
        #expect(InsightCards.percent(-4.41) == "-4.41%")
    }
}
