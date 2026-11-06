import Testing
@testable import Persona

/// The long-press bar's emoji (`LarkQuickReactions`): the hub's quick list, each once, as many as the row shows.
struct LarkQuickReactionsTests {
    @Test func theRowHoldsSix() {
        let hub = ["THUMBSUP", "HEART", "SMILE", "LAUGH", "SURPRISED", "CRY", "OK", "DONE"]
        #expect(LarkQuickReactions.bar(hub) == Array(hub.prefix(6)))
        #expect(LarkQuickReactions.count == 6)
    }

    @Test func recentOnesComeFirstAndEachOnlyOnce() {
        // The hub puts recent ones before Feishu's defaults; a default the user used recently is listed twice.
        let hub = ["DONE", "heart", "THUMBSUP", "HEART", "SMILE", "DONE", "LAUGH", "CRY", "OK"]
        #expect(LarkQuickReactions.bar(hub) == ["DONE", "heart", "THUMBSUP", "SMILE", "LAUGH", "CRY"])
    }

    @Test func blanksAreSkippedAndAShortListStaysShort() {
        #expect(LarkQuickReactions.bar(["", " OK ", "  "]) == ["OK"])
        #expect(LarkQuickReactions.bar([]).isEmpty)
    }
}
