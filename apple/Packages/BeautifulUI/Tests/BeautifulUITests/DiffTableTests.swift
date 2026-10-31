import SwiftUI
import Testing
@testable import BeautifulUI

@Suite struct DiffTableSelectionTests {
    let rows = [
        DiffTableRow(name: "Rocky Road", tag: "Classic", tagTint: .blue, detail: "a", change: .removal),
        DiffTableRow(name: "Bubblegum", tag: "Retro", tagTint: .gray, detail: "b", change: .removal),
        DiffTableRow(name: "Mint Chip", tag: "Classic", tagTint: .blue, detail: "c"),
        DiffTableRow(name: "Pistachio", tag: "Seasonal", tagTint: .green, detail: "d", change: .addition),
    ]

    @Test func countsIncludedEditsOnly() {
        var s = DiffTableSelection(included: ["Rocky Road", "Bubblegum", "Pistachio"])
        #expect(s.removals(in: rows) == 2 && s.additions(in: rows) == 1)
        s.toggle("Rocky Road")
        s.toggle("Pistachio")
        #expect(s.removals(in: rows) == 1 && s.additions(in: rows) == 0)
    }

    @Test func pluralisesTheSummary() {
        #expect(DiffTableSelection.summary(removals: 2, additions: 1) == "2 removals · 1 addition")
        #expect(DiffTableSelection.summary(removals: 1, additions: 0) == "1 removal · 0 additions")
    }

    @Test func columnsSplitTheWidthByFraction() {
        let widths = [0.34, 0.30, 0.36].map { 320 * $0 }
        #expect(abs(widths.reduce(0, +) - 320) < 0.001)
    }
}
