import Foundation
import Testing
@testable import Jarvis

/// Find in the code viewer (⌘F over an open file): matching, the ranges in each line, and the order ⏎ / ⇧⏎ go in.
struct CodeFindTests {
    private let lines = ["let Foo = foo()", "// nothing here", "FOOfoo", "", "café Café"]
    private func all(_ q: String) -> [CodeFind.Match] {
        CodeFind.matches(of: q, lineCount: lines.count) { lines[$0] }
    }

    @Test func caseInsensitiveByDefault() {
        #expect(CodeFind.ranges(of: "foo", in: "let Foo = foo()") == [NSRange(location: 4, length: 3), NSRange(location: 10, length: 3)])
        #expect(CodeFind.ranges(of: "FOO", in: "foo") == [NSRange(location: 0, length: 3)])
    }

    @Test func matchesDoNotOverlap() {
        #expect(CodeFind.ranges(of: "aa", in: "aaaa") == [NSRange(location: 0, length: 2), NSRange(location: 2, length: 2)])
    }

    @Test func emptyQueryOrLineFindsNothing() {
        #expect(CodeFind.ranges(of: "", in: "abc").isEmpty)
        #expect(CodeFind.ranges(of: "a", in: "").isEmpty)
        #expect(all("").isEmpty)
    }

    @Test func rangesAreUTF16() {
        // "é" is one UTF-16 unit, "😀" two: the ranges are what the drawn NSAttributedString counts.
        #expect(CodeFind.ranges(of: "café", in: "😀 Café") == [NSRange(location: 3, length: 4)])
    }

    @Test func matchesInReadingOrder() {
        let m = all("foo")
        #expect(m.map(\.line) == [0, 0, 2, 2])
        #expect(m.map(\.range.location) == [4, 10, 0, 3])
        #expect(all("café").map(\.line) == [4, 4])
        #expect(all("missing").isEmpty)
    }

    @Test func stepGoesRound() {
        #expect(CodeFind.step(from: nil, count: 3, forward: true) == 0)
        #expect(CodeFind.step(from: nil, count: 3, forward: false) == 2)
        #expect(CodeFind.step(from: 0, count: 3, forward: true) == 1)
        #expect(CodeFind.step(from: 2, count: 3, forward: true) == 0)
        #expect(CodeFind.step(from: 0, count: 3, forward: false) == 2)
        #expect(CodeFind.step(from: 1, count: 3, forward: false) == 0)
        #expect(CodeFind.step(from: 0, count: 0, forward: true) == nil)
        // A stale index (the matches got fewer) starts again from the ends.
        #expect(CodeFind.step(from: 7, count: 3, forward: true) == 0)
    }

    @Test func firstAtOrAfterALineGoesRound() {
        let m = all("foo") // lines 0, 0, 2, 2
        #expect(CodeFind.first(in: m, atOrAfter: 0) == 0)
        #expect(CodeFind.first(in: m, atOrAfter: 1) == 2)
        #expect(CodeFind.first(in: m, atOrAfter: 2) == 2)
        #expect(CodeFind.first(in: m, atOrAfter: 3) == 0)
        #expect(CodeFind.first(in: [], atOrAfter: 0) == nil)
    }
}
