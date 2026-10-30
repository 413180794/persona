import Testing
@testable import BeautifulUI

@Suite struct ThinkingStateTests {
    @Test func staysClosedUntilThereIsSomethingToShow() {
        #expect(!ThinkingState.autoExpanded(isWorking: true, hasContent: false, lingering: false))
        #expect(ThinkingState.autoExpanded(isWorking: true, hasContent: true, lingering: false))
    }

    @Test func lingersOpenAfterWorkEndsThenFolds() {
        #expect(ThinkingState.autoExpanded(isWorking: false, hasContent: true, lingering: true))
        #expect(!ThinkingState.autoExpanded(isWorking: false, hasContent: true, lingering: false))
    }

    @Test func rowIdsDefaultToTitle() {
        let row = ThinkingRow("Read", detail: "flavors.ts", monospacedDetail: true)
        #expect(row.id == "Read")
        #expect(ThinkingRow(id: "r1", "Read").id == "r1")
    }
}
