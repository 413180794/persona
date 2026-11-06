import Foundation
import Testing
@testable import Persona

@MainActor struct TerminalTests {
    @Test func backsOffToFifteenSeconds() {
        #expect([1, 2, 3, 4, 5, 9].map(TerminalFeed.backoff) == [1, 2, 4, 8, 15, 15])
    }

    @Test func saysHowOldTheScreenIs() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        #expect(TerminalLinkStatus.headline(lastOK: nil, now: now) == "还没有拿到终端")
        #expect(TerminalLinkStatus.headline(lastOK: now.addingTimeInterval(-12), now: now) == "上次更新 12 秒前")
        #expect(TerminalLinkStatus.headline(lastOK: now.addingTimeInterval(-185), now: now) == "上次更新 3 分钟前")
    }

    @Test func countdownComesBeforeTheReason() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        #expect(TerminalLinkStatus.detail(failure: nil, nextTry: now, now: now) == "等待电脑回应…")
        #expect(TerminalLinkStatus.detail(failure: "连不上 hub", nextTry: now.addingTimeInterval(3.2), now: now) == "4 秒后重试 · 连不上 hub")
        #expect(TerminalLinkStatus.detail(failure: "连不上 hub", nextTry: now.addingTimeInterval(-1), now: now) == "正在重试… · 连不上 hub")
    }
}

@MainActor struct TerminalModifierTests {
    @Test func tapArmsForOneKey() {
        var m = TerminalModifiers()
        m.tap(.ctrl, quick: false)
        #expect(m.ctrl == .once && m.apply("c") == "ctrl+c")
        m.pressed()
        #expect(m.ctrl == .off && !m.active && m.apply("c") == "c")
    }

    @Test func doubleTapLocksUntilTappedAgain() {
        var m = TerminalModifiers()
        m.tap(.ctrl, quick: false)
        m.tap(.ctrl, quick: true)
        #expect(m.ctrl == .locked)
        m.pressed(); m.pressed()
        #expect(m.ctrl == .locked)
        m.tap(.ctrl, quick: false)
        #expect(m.ctrl == .off)
    }

    @Test func slowSecondTapDisarms() {
        var m = TerminalModifiers()
        m.tap(.alt, quick: false)
        m.tap(.alt, quick: false)
        #expect(m.alt == .off)
    }

    @Test func holdingKeepsItDownUntilLetGo() {
        var m = TerminalModifiers()
        m.hold(.ctrl)
        m.pressed(); m.pressed()
        #expect(m.ctrl == .held)
        m.release(.ctrl)
        #expect(m.ctrl == .off)
        // Let go without pressing anything: armed for the next key.
        m.hold(.shift)
        m.release(.shift)
        #expect(m.shift == .once)
    }

    @Test func combinesInHerdrOrder() {
        var m = TerminalModifiers()
        m.tap(.shift, quick: false); m.tap(.ctrl, quick: false); m.tap(.alt, quick: false)
        #expect(m.apply("x") == "ctrl+alt+shift+x")
        #expect(m.apply("esc") == "esc")
    }

    @Test func typedCharacters() {
        var m = TerminalModifiers()
        m.ctrl = .once
        #expect(m.typed("C") == ("ctrl+c", nil))
        #expect(m.typed(" ") == ("ctrl+space", nil))
        #expect(m.typed("[") == ("ctrl+[", nil))
        #expect(m.typed("中") == (nil, "中"))
        m.ctrl = .off; m.shift = .once
        #expect(m.typed("a") == (nil, "A"))
    }
}

@MainActor struct TerminalCompletionTests {
    private func rows(_ lines: [String]) -> [TermRow] { TerminalGrid.rows(fromANSI: lines.joined(separator: "\n")) }

    @Test func commandsAfterPrompts() {
        #expect(TerminalCompletion.command(in: "~/app $ git status") == "git status")
        #expect(TerminalCompletion.command(in: "➜  weather git:(main) ✗ make test") == "weather git:(main) ✗ make test")
        #expect(TerminalCompletion.command(in: "Build complete! (8.31s)") == nil)
        #expect(TerminalCompletion.command(in: "$ ") == nil)
    }

    @Test func latestFirstThenWords() {
        let screen = rows(["$ swift build", "Compiling Sources/Weather/Client.swift", "$ swift test --filter NetworkTests", "$ git status"])
        #expect(TerminalCompletion.suggestions(for: "swift", rows: screen) == ["swift test --filter NetworkTests", "swift build"])
        #expect(TerminalCompletion.suggestions(for: "cat Sour", rows: screen) == ["cat Sources/Weather/Client.swift"])
        #expect(TerminalCompletion.suggestions(for: "", rows: screen).isEmpty)
        #expect(TerminalCompletion.suggestions(for: "git status", rows: screen).isEmpty) // already typed in full
    }

    @Test func shortcutsRoundTrip() {
        #expect(TerminalPrefs.decode("") == TerminalPrefs.defaultShortcuts)
        #expect(TerminalPrefs.decode(TerminalPrefs.encode(["a b", "/compact"])) == ["a b", "/compact"])
        #expect(TerminalPrefs.decode(TerminalPrefs.encode([])) == [])
    }
}
