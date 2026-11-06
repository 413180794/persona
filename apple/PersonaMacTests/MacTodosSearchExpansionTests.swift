import Testing
@testable import Persona

/// The collapsed done group expands while a search matches completed todos, and clearing the search never collapses
/// a group the user opened (#3474).
struct MacTodosSearchExpansionTests {
    @Test func emptyQueryKeepsTheCurrentExpandState() {
        #expect(MacTodosPage.SearchExpansion.doneExpanded(query: "", hasDoneMatches: false, current: false) == false)
        #expect(MacTodosPage.SearchExpansion.doneExpanded(query: "", hasDoneMatches: true, current: true) == true)
    }

    @Test func whitespaceOnlyQueryIsTreatedAsEmpty() {
        #expect(MacTodosPage.SearchExpansion.doneExpanded(query: " \t\n", hasDoneMatches: true, current: false) == false)
        #expect(MacTodosPage.SearchExpansion.doneExpanded(query: " \t\n", hasDoneMatches: true, current: true) == true)
    }

    @Test func nonEmptyQueryWithDoneMatchesExpandsTheGroup() {
        #expect(MacTodosPage.SearchExpansion.doneExpanded(query: "report", hasDoneMatches: true, current: false) == true)
        #expect(MacTodosPage.SearchExpansion.doneExpanded(query: "report", hasDoneMatches: true, current: true) == true)
    }

    @Test func nonEmptyQueryWithoutDoneMatchesKeepsTheCurrentExpandState() {
        #expect(MacTodosPage.SearchExpansion.doneExpanded(query: "report", hasDoneMatches: false, current: false) == false)
        #expect(MacTodosPage.SearchExpansion.doneExpanded(query: "report", hasDoneMatches: false, current: true) == true)
    }
}
