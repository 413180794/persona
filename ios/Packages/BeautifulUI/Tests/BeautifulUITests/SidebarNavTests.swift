import Testing
@testable import BeautifulUI

@Suite struct SidebarNavTests {
    let recents = [SidebarRecent(id: "a", label: "Supplier records"), SidebarRecent(id: "b", label: "Workload summary")]

    @Test func blankQueryKeepsEverything() {
        #expect(SidebarNav.filter(recents, query: "  ").count == 2)
    }

    @Test func matchesCaseInsensitively() {
        #expect(SidebarNav.filter(recents, query: "WORK").map(\.id) == ["b"])
        #expect(SidebarNav.filter(recents, query: "zzz").isEmpty)
    }
}
