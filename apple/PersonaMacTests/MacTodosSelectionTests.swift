import Foundation
import Testing
@testable import Persona

/// The Mac todos page moves its selection when the selected todo is deleted (#3471): list context menu,
/// the detail page's own delete button, and a push delete from another device all only change `store.todos`.
struct MacTodosSelectionTests {
    private func todo(_ id: String, done: Bool = false) -> TodoItem {
        TodoItem(id: id, title: id, doneAt: done ? Date() : nil)
    }

    @Test func keepsSelectionWhileTodoStillExists() {
        let open = [todo("a"), todo("b"), todo("c")]
        // Not the first row: the existing selection wins even though a fallback exists.
        #expect(MacTodosSelection.resolve(selectedID: "b", existing: open, open: open, done: []) == "b")
    }

    @Test func movesToFirstOpenTodoWhenSelectedIsDeleted() {
        let remaining = [todo("a"), todo("c")]
        #expect(MacTodosSelection.resolve(selectedID: "b", existing: remaining, open: remaining, done: []) == "a")
    }

    @Test func fallsBackToFirstDoneTodoWhenNoOpenTodoRemains() {
        let done = [todo("d", done: true)]
        #expect(MacTodosSelection.resolve(selectedID: "b", existing: done, open: [], done: done) == "d")
    }

    @Test func clearsSelectionWhenEveryTodoIsGone() {
        #expect(MacTodosSelection.resolve(selectedID: "b", existing: [], open: [], done: []) == nil)
    }

    @Test func keepsSelectionHiddenByTheSearchQuery() {
        // Existence is checked against the full store list, not the query-filtered lists: typing a search
        // that hides the selected todo must not jump the detail column.
        let all = [todo("a"), todo("b")]
        #expect(MacTodosSelection.resolve(selectedID: "b", existing: all, open: [todo("a")], done: []) == "b")
    }
}
