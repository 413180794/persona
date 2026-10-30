import Testing
@testable import BeautifulUI

@Suite struct SearchListTests {
    let items = ["Forecast summer demand", "Find waffle cone suppliers", "Compare seasonal flavors", "Draft flavor launch plan", "Check cold-chain status", "Audit sugar costs"]

    @Test func emptyQueryShowsTheFirstFive() {
        #expect(SearchList.results(items, query: "").count == 5)
    }

    @Test func filtersCaseInsensitively() {
        #expect(SearchList.results(items, query: "FLAVOR") == ["Compare seasonal flavors", "Draft flavor launch plan"])
    }

    @Test func emptyStateWaitsForThreeCharacters() {
        #expect(!SearchList.isEmptyState(items, query: "zz"))
        #expect(SearchList.isEmptyState(items, query: "zzz"))
        #expect(!SearchList.isEmptyState(items, query: "sug"))
    }
}
