import Foundation
import Testing
@testable import Jarvis

@MainActor struct TerminalTests {
    @Test func backsOffToFifteenSeconds() {
        #expect([1, 2, 3, 4, 5, 9].map(TerminalPanel.backoff) == [1, 2, 4, 8, 15, 15])
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
