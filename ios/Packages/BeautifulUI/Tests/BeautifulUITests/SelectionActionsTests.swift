import Testing
@testable import BeautifulUI

@Suite struct SelectionActionsTests {
    @Test func streamsWordByWordAndRejoinsExactly() {
        let rewrite = "Churn pistachio first thing Saturday."
        let words = SelectionActions.words(rewrite)
        #expect(words.count == 5)
        #expect(words.joined(separator: " ") == rewrite)
    }
}
