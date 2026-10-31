import Testing
@testable import BeautifulUI

@Suite struct RecordsSortTests {
    let records = [
        RecordItem(id: "b", name: "Blue Fig", tags: [], lastInteraction: "9 days ago", strength: .veryWeak),
        RecordItem(id: "a", name: "Alpine Churn", tags: [], lastInteraction: "4 days ago", strength: .strong),
        RecordItem(id: "c", name: "Cacao Norte", tags: [], lastInteraction: "No contact", strength: .none),
    ]

    @Test func sortsByNameByDefault() {
        #expect(RecordsSort().apply(records).map(\.id) == ["a", "b", "c"])
        #expect(RecordsSort(.name, ascending: false).apply(records).map(\.id) == ["c", "b", "a"])
    }

    @Test func sortsStrengthByRank() {
        #expect(RecordsSort(.strength).apply(records).map(\.id) == ["c", "b", "a"])
    }

    @Test func sortsInteractionTextLikeTheOriginal() {
        // The original compares the text, not the dates it describes.
        #expect(RecordsSort(.lastInteraction).apply(records).map(\.id) == ["a", "b", "c"])
    }
}
