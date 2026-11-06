import AppKit
import Testing
@testable import Persona

/// Find in the code viewer (⌘F over an open file): matching, the ranges in each line, and the order ⏎ / ⇧⏎ go in.
struct CodeFindTests {
    private let lines = ["let Foo = foo()", "// nothing here", "FOOfoo", "", "café Café"]
    private func all(_ q: String) -> [CodeFind.Match] {
        CodeFind.matches(of: q, lineCount: lines.count) { lines[$0] }
    }

    /// The find field takes Esc, ⏎ and ⇧⏎ before the window does: the file viewer's Done holds Esc as its shortcut and
    /// would close the whole sheet. Other keys, and these keys while the field is not focused, stay the window's.
    @Test func theFieldTakesEscReturnAndShiftReturn() {
        #expect(CodeFind.barKey(keyCode: 53, shift: false, fieldFocused: true) == .close)
        #expect(CodeFind.barKey(keyCode: 36, shift: false, fieldFocused: true) == .next)
        #expect(CodeFind.barKey(keyCode: 76, shift: false, fieldFocused: true) == .next) // keypad enter
        #expect(CodeFind.barKey(keyCode: 36, shift: true, fieldFocused: true) == .previous)
        #expect(CodeFind.barKey(keyCode: 53, shift: false, fieldFocused: false) == nil)
        #expect(CodeFind.barKey(keyCode: 36, shift: true, fieldFocused: false) == nil)
        #expect(CodeFind.barKey(keyCode: 0, shift: false, fieldFocused: true) == nil) // "a" is typed
        // Composing (pinyin in the field): ⏎ and Esc belong to the input method.
        #expect(CodeFind.barKey(keyCode: 36, shift: false, fieldFocused: true, composing: true) == nil)
        #expect(CodeFind.barKey(keyCode: 53, shift: false, fieldFocused: true, composing: true) == nil)
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

    private func r(_ loc: Int, _ len: Int) -> NSRange { NSRange(location: loc, length: len) }

    @Test func caseSensitive() {
        let o = CodeFind.Options(caseSensitive: true)
        #expect(CodeFind.ranges(of: "foo", in: "let Foo = foo()", options: o) == [r(10, 3)])
        #expect(CodeFind.ranges(of: "Foo", in: "let Foo = foo()", options: o) == [r(4, 3)])
    }

    @Test func wholeWord() {
        let o = CodeFind.Options(wholeWord: true)
        #expect(CodeFind.ranges(of: "foo", in: "foofoo foo_bar foo(x) Foo", options: o) == [r(15, 3), r(22, 3)])
        // A match that is not a word does not hide one starting inside it.
        #expect(CodeFind.ranges(of: "aa", in: "aaa aa", options: o) == [r(4, 2)])
        #expect(CodeFind.ranges(of: "名字", in: "名字 名字们", options: o) == [r(0, 2)])
        #expect(CodeFind.ranges(of: "x", in: "😀x😀", options: o) == [r(2, 1)]) // an emoji is not a word character
    }

    @Test func regularExpressions() {
        let o = CodeFind.Options(regex: true)
        #expect(CodeFind.ranges(of: "f(o+)", in: "fo foo FOOO", options: o) == [r(0, 2), r(3, 3), r(7, 4)])
        #expect(CodeFind.ranges(of: "[a-z]+", in: "Ab cD", options: CodeFind.Options(caseSensitive: true, regex: true)) == [r(1, 1), r(3, 1)])
        #expect(CodeFind.ranges(of: "o+", in: "foo boo", options: CodeFind.Options(wholeWord: true, regex: true)).isEmpty)
        #expect(CodeFind.ranges(of: "\\w+", in: "foo boo", options: CodeFind.Options(wholeWord: true, regex: true)) == [r(0, 3), r(4, 3)])
        // Empty matches are not matches.
        #expect(CodeFind.ranges(of: "^", in: "abc", options: o).isEmpty)
        #expect(CodeFind.ranges(of: "x*", in: "axb", options: o) == [r(1, 1)])
    }

    @Test func aBrokenRegexFindsNothingAndSaysSo() {
        let o = CodeFind.Options(regex: true)
        #expect(CodeFind.ranges(of: "foo(", in: "foo(", options: o).isEmpty)
        #expect(!CodeFind.Matcher.isValid("foo(", options: o))
        #expect(CodeFind.Matcher.isValid("foo(", options: CodeFind.Options()))
        #expect(CodeFind.Matcher.isValid("", options: o))
        #expect(CodeFind.matches(of: "[", options: o, lineCount: lines.count) { lines[$0] }.isEmpty)
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

/// Find Next ⌘G / Find Previous ⇧⌘G over an open file: the bar's model steps through the matches, going round; with the
/// bar closed ⌘G opens it again on the last query, at the first match from the top of what is shown.
@MainActor
struct CodeFindNextTests {
    private func viewer(_ lines: [String]) -> (CodeLinesView, CodeFindModel, LinesSource) {
        let source = LinesSource(lines)
        // In a scroll view, as `CodeLines` puts it (its top line comes from what the scroll view shows).
        let view = CodeLinesView(frame: NSRect(x: 0, y: 0, width: 400, height: 300))
        let scroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: 400, height: 300))
        scroll.documentView = view
        let find = CodeFindModel()
        view.find = find
        find.view = view
        view.source = source
        source.scroll = scroll
        return (view, find, source) // the view holds its source weakly: the caller keeps it (and the scroll view)
    }

    @Test func commandGStepsForwardAndBackGoingRound() {
        let (view, find, source) = viewer(["foo", "bar", "foo foo"])
        find.show()
        find.query = "foo"
        #expect(find.current == 0)
        view.findNext(forward: true)
        #expect(find.current == 1)
        view.findNext(forward: true)
        #expect(find.current == 2)
        view.findNext(forward: true)
        #expect(find.current == 0)
        view.findNext(forward: false)
        #expect(find.current == 2)
        withExtendedLifetime(source) {}
    }

    @Test func commandGWithTheBarClosedOpensItOnTheLastQuery() {
        let (view, find, source) = viewer(["foo", "bar", "foo"])
        find.show()
        find.query = "foo"
        find.close()
        #expect(!find.visible)
        #expect(find.matches.isEmpty)
        view.findNext(forward: true)
        #expect(find.visible)
        #expect(find.matches.count == 2)
        #expect(find.current == 0)
        withExtendedLifetime(source) {}
    }

    /// A switch changed: searched again, staying on the match the bar was on when it still matches.
    @Test func optionsSearchAgain() {
        let (_, find, source) = viewer(["Foo", "bar", "foo", "foo"])
        find.options = CodeFind.Options()
        find.show()
        find.query = "foo"
        #expect(find.matches.count == 3)
        find.step(forward: true)
        #expect(find.currentMatch?.line == 2)
        find.options.caseSensitive = true
        #expect(find.matches.map(\.line) == [2, 3])
        #expect(find.currentMatch?.line == 2)
        find.options.regex = true
        find.query = "fo("
        #expect(find.invalid)
        #expect(find.matches.isEmpty)
        find.options = CodeFind.Options()
        withExtendedLifetime(source) {}
    }

    @Test func commandGWithNoQueryOpensTheBar() {
        let (view, find, source) = viewer(["foo"])
        view.findNext(forward: true)
        #expect(find.visible)
        #expect(find.current == nil)
        withExtendedLifetime(source) {}
    }
}

/// A file's lines for the code view, plain.
@MainActor
final class LinesSource: CodeLineSource {
    let lines: [String]
    var scroll: NSScrollView?
    init(_ lines: [String]) { self.lines = lines }
    var lineCount: Int { lines.count }
    var columns: Int { lines.map(\.count).max() ?? 0 }
    var gutterColumns: Int { 2 }
    func line(_ i: Int) -> NSAttributedString { NSAttributedString(string: lines[i]) }
    func gutter(_ i: Int) -> NSAttributedString { NSAttributedString(string: "\(i + 1)") }
    func tap(_ i: Int) -> Bool { false }
    func menu(_ i: Int) -> UIMenu? { nil }
    var changed: ((Range<Int>?) -> Void)?
}
