import Foundation
import Testing
@testable import Persona

/// The Agents chat header's status line (docs/archive/apple/plan-chat-topbar.md ③) and the tab names of its strip.
struct HeaderStatusTests {
    private let now = Date(timeIntervalSince1970: 1_793_700_000)

    private func sub(_ id: String, _ minutesAgo: Double?, running: Bool) -> Subagent {
        Subagent(id: id, sessionID: "s1@" + id, description: "task " + id, running: running,
                 startedAt: minutesAgo.map { now.addingTimeInterval(-$0 * 60) })
    }

    @Test func noneRunningIsNoLine() {
        #expect(HeaderStatus.pick([], viewing: nil) == nil)
        #expect(HeaderStatus.pick([sub("a", 5, running: false), sub("b", 1, running: false)], viewing: nil) == nil)
    }

    @Test func oneRunningIsThatOne() {
        let a = sub("a", 5, running: true)
        #expect(HeaderStatus.pick([sub("done", 1, running: false), a], viewing: nil) == .running(a))
    }

    @Test func severalRunningNamesTheOneStartedLast() {
        let all = [sub("a", 9, running: true), sub("b", 2, running: true), sub("c", nil, running: true), sub("d", 1, running: false)]
        #expect(HeaderStatus.pick(all, viewing: nil) == .severalRunning(count: 3, latest: all[1]))
        let same = [sub("x", 4, running: true), sub("y", 4, running: true)]
        #expect(HeaderStatus.pick(same, viewing: nil) == .severalRunning(count: 2, latest: same[0]))
    }

    @Test func viewingOneWinsAndUsesItsLatestState() {
        let stale = sub("a", 5, running: true)
        var fresh = stale
        fresh.running = false
        #expect(HeaderStatus.pick([fresh, sub("b", 1, running: true)], viewing: stale) == .viewing(fresh))
        // A workflow's agent read like a subagent is not in the list: shown as it was opened.
        let workflow = sub("wf", 3, running: true)
        #expect(HeaderStatus.pick([], viewing: workflow) == .viewing(workflow))
    }

    @Test func tabNamesForTheBeautifulChipAreCutAtAboutTheCap() {
        #expect(SiblingTabs.capped("short") == "short")
        #expect(SiblingTabs.capped("12345678901234567890") == "12345678901234567890")
        #expect(SiblingTabs.capped("123456789012345678901") == "12345678901234567890…")
        // A CJK character counts as two.
        #expect(SiblingTabs.capped("一二三四五六七八九十") == "一二三四五六七八九十")
        #expect(SiblingTabs.capped("一二三四五六七八九十一") == "一二三四五六七八九十…")
        #expect(SiblingTabs.capped("refactor 一二三四五六七") == "refactor 一二三四五…")
    }
}
