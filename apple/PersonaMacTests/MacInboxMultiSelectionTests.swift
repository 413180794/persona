import Testing
@testable import Persona

/// A multi-row inbox selection can mix items with agent requests: only items can be filed as Done, so a selection
/// made only of agent requests must neither offer mark-done nor swallow E (#3479).
@MainActor
struct MacInboxMultiSelectionTests {
    @Test func itemsOnlyCanBeMarkedDone() {
        let selection = MacInboxMultiSelection.decide(itemCount: 3, agentCount: 0)
        #expect(selection == .itemsOnly(itemCount: 3))
        #expect(selection.itemCount == 3)
        #expect(selection.canMarkDone)
    }

    @Test func mixedOnlyFilesTheItemRows() {
        let selection = MacInboxMultiSelection.decide(itemCount: 2, agentCount: 1)
        #expect(selection == .mixed(itemCount: 2, agentCount: 1))
        #expect(selection.itemCount == 2)
        #expect(selection.canMarkDone)
    }

    @Test func agentsOnlyCannotBeMarkedDone() {
        let selection = MacInboxMultiSelection.decide(itemCount: 0, agentCount: 2)
        #expect(selection == .agentsOnly(agentCount: 2))
        #expect(selection.itemCount == 0)
        #expect(!selection.canMarkDone)
    }

    @Test func emptySelectionCannotBeMarkedDone() {
        let selection = MacInboxMultiSelection.decide(itemCount: 0, agentCount: 0)
        #expect(selection == .agentsOnly(agentCount: 0))
        #expect(!selection.canMarkDone)
    }

    /// toggleDone silently ignores agent rows: an agents-only selection files nothing and keeps the selection as it
    /// was (the E handler relies on `canMarkDone`, this pins the state-side guard).
    @Test func agentsOnlyToggleDoneChangesNothing() {
        let state = MacInboxState()
        let before = InboxStore.shared.items
        state.selection = [.agent("a"), .agent("b")]
        state.toggleDone(state.selection, model: AppModel())
        #expect(state.selection == [.agent("a"), .agent("b")])
        #expect(InboxStore.shared.items == before)
    }
}
