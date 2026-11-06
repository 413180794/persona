import Testing
@testable import Persona

/// The Agents card of a terminal waiting with no dialog to read (`ScreenParser.waitingOn`): the tool call and its last
/// output line, without Claude Code's tree glyphs.
struct ScreenWaitingTests {
    private let rule = String(repeating: "─", count: 60)

    @Test func toolCallAndLastLine() {
        let screen = "⏺ Bash(git stash && git rebase origin/master)\n  ⎿  Saved working directory\n     … 40 more files changed\n" +
            "  ⎿  (a git step in this command can move the working tree)\n\n\(rule)\n> \n\(rule)\n  ? for shortcuts"
        #expect(ScreenParser.waitingOn(screen) == "Bash(git stash && git rebase origin/master)\n(a git step in this command can move the working tree)")
    }

    @Test func noToolCall() {
        let screen = "  ⎿  one\n  │ two\n\(rule)\n> \n\(rule)"
        #expect(ScreenParser.waitingOn(screen) == "one\ntwo")
        #expect(ScreenParser.waitingOn("\(rule)\n> \n\(rule)") == nil)
    }

    @Test func plainLinesDropGlyphsAndBlanks() {
        #expect(ScreenParser.plainLines("  ⎿  a\n\n  └ b\n├ c\nd", max: 3) == "a\nb\nc")
    }
}
