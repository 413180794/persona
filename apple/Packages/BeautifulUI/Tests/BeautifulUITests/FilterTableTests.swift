import Testing
@testable import BeautifulUI

@Suite struct FilterTableTests {
    let rows = [
        FilterTableRow(task: "a", date: "", statusID: "todo", owner: ""),
        FilterTableRow(task: "b", date: "", statusID: "progress", owner: ""),
        FilterTableRow(task: "c", date: "", statusID: "todo", owner: ""),
    ]

    @Test func nilFilterKeepsEveryRow() {
        #expect(FilterTable.visible(rows, filter: nil).count == 3)
    }

    @Test func filtersByStatus() {
        #expect(FilterTable.visible(rows, filter: "todo").map(\.task) == ["a", "c"])
        #expect(FilterTable.visible(rows, filter: "done").isEmpty)
    }
}
