import AppKit
import Testing
@testable import Persona

/// The Git diff's colored lines on the Mac: an NSTextView fills a line's background to the view's edge only from its
/// line break, so every line, the last one too, ends in one (or the last added / removed line stops half way).
@MainActor struct MacDiffTextTests {
    @Test func everyLineEndsInABreakSoItsBackgroundRunsToTheEdge() {
        let lines = [
            DiffText.Line(id: 0, kind: .context, text: "let a = 1", old: 1, new: 1),
            DiffText.Line(id: 1, kind: .add, text: "let b = 2", old: nil, new: 2),
        ]
        let s = DiffText.attributed(lines).string
        #expect(s.hasSuffix("\n"))
        #expect(s.components(separatedBy: "\n").count == lines.count + 1)
        // The break carries the line's own background, which is what gets filled to the edge.
        let a = DiffText.attributed(lines)
        let bg = a.attribute(.backgroundColor, at: a.length - 1, effectiveRange: nil) as? NSColor
        #expect(bg != nil && bg != .clear)
    }
}
