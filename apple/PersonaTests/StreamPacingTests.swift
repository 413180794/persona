import Foundation
import PersonaKit
import Testing
@testable import Persona

/// A streamed reply draws at most 5 times a second, always its newest text, and never an old one over the final
/// message (`DeltaPacer`); the activity is not redrawn with what it already shows (`LiveActivityController`).
struct StreamPacingTests {
    let key = SessionKey(machineID: "m", sessionID: "s")

    /// Deltas every 80 ms for 3 s, as the node sends them: the first draws at once, then at most one per 200 ms, and
    /// the last text is drawn within 200 ms of arriving.
    @Test func drawsAtMostFiveTimesASecond() {
        var p = DeltaPacer()
        let t0 = Date(timeIntervalSinceReferenceDate: 1000)
        var draws: [(Date, String)] = []
        var due: Date?
        var last = ""
        for i in 0..<38 {
            let now = t0.addingTimeInterval(Double(i) * 0.08)
            if let d = due, d <= now, let t = p.due(key, now: d) { draws.append((d, t)); due = nil }
            last = String(repeating: "字", count: i + 1)
            switch p.offer(key, last, now: now) {
            case .draw(let t): draws.append((now, t))
            case .later(let wait): due = now.addingTimeInterval(wait)
            case .kept: break
            }
        }
        if let d = due, let t = p.due(key, now: d) { draws.append((d, t)) }
        #expect(draws.first?.1 == "字")
        #expect(draws.last?.1 == last)
        for (a, b) in zip(draws, draws.dropFirst()) { #expect(b.0.timeIntervalSince(a.0) >= DeltaPacer.every - 1e-9) }
        let span = draws.last!.0.timeIntervalSince(draws.first!.0)
        #expect(Double(draws.count - 1) / span <= 5.0 + 1e-9)
        #expect(draws.count < 38 / 2)
    }

    /// The reply landed while a newer text was kept: nothing is drawn after it.
    @Test func aDroppedTextNeverComesBack() {
        var p = DeltaPacer()
        let t0 = Date(timeIntervalSinceReferenceDate: 1000)
        #expect(p.offer(key, "a", now: t0) == .draw("a"))
        guard case .later(let wait) = p.offer(key, "ab", now: t0.addingTimeInterval(0.08)) else { Issue.record("not kept"); return }
        #expect(abs(wait - 0.12) < 1e-9)
        p.drop(key)
        #expect(p.due(key, now: t0.addingTimeInterval(0.2)) == nil)
    }

    /// The stream cleared: at once, and what was kept is gone.
    @Test func aClearedStreamIsImmediate() {
        var p = DeltaPacer()
        let t0 = Date(timeIntervalSinceReferenceDate: 1000)
        _ = p.offer(key, "a", now: t0)
        _ = p.offer(key, "ab", now: t0.addingTimeInterval(0.05))
        #expect(p.offer(key, "", now: t0.addingTimeInterval(0.1)) == .draw(""))
        #expect(p.due(key, now: t0.addingTimeInterval(0.2)) == nil)
    }

    /// Each chat streams on its own clock.
    @Test func chatsArePacedApart() {
        var p = DeltaPacer()
        let t0 = Date(timeIntervalSinceReferenceDate: 1000)
        let other = SessionKey(machineID: "m", sessionID: "t")
        #expect(p.offer(key, "a", now: t0) == .draw("a"))
        #expect(p.offer(other, "x", now: t0.addingTimeInterval(0.01)) == .draw("x"))
    }

    /// What `StreamingText` looks up is what the prewarm parsed: the block being written, after the finished ones.
    @Test func thePrewarmKeyIsTheTail() {
        let text = "第一段。\n\n第二段正在写"
        let parts = StreamChunks.split(text)
        #expect(parts.done == ["第一段。"])
        #expect(parts.tail == "第二段正在写")
    }

    @Test func theActivityIsNotRedrawnWithTheSame() {
        let now = Date(timeIntervalSinceReferenceDate: 1000)
        let a = FleetState(phase: .working, working: 1)
        var b = a
        b.working = 2
        #expect(!LiveActivityController.worthUpdating(a, staleDate: now.addingTimeInterval(14 * 60), to: a, now: now))
        #expect(LiveActivityController.worthUpdating(a, staleDate: now.addingTimeInterval(14 * 60), to: b, now: now))
        #expect(LiveActivityController.worthUpdating(a, staleDate: now.addingTimeInterval(4 * 60), to: a, now: now)) // near stale
    }
}
