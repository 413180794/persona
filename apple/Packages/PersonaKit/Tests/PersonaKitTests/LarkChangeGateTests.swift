import Foundation
import Testing
@testable import PersonaKit

/// An open chat hears of its changes at most once per gap (LarkSync.chatGap, #674).
struct LarkChangeGateTests {
    private let t0 = ContinuousClock.now

    @Test func firstChangeGoesAtOnce() {
        var g = LarkChangeGate(gap: .seconds(1))
        #expect(g.arrive(at: t0) == .now)
        #expect(g.arrive(at: t0 + .seconds(2)) == .now)
    }

    @Test func aBurstFoldsIntoOneAfterTheGap() {
        var g = LarkChangeGate(gap: .seconds(1))
        #expect(g.arrive(at: t0) == .now)
        #expect(g.arrive(at: t0 + .milliseconds(400)) == .later(t0 + .seconds(1)))
        #expect(g.arrive(at: t0 + .milliseconds(800)) == .pending)
        g.fired(at: t0 + .seconds(1))
        // The next one waits for a gap after the delayed one, not after the first.
        #expect(g.arrive(at: t0 + .milliseconds(1500)) == .later(t0 + .seconds(2)))
    }

    @Test func aChangeAfterTheDelayedOneAndAQuietGapGoesAtOnce() {
        var g = LarkChangeGate(gap: .seconds(1))
        _ = g.arrive(at: t0)
        _ = g.arrive(at: t0 + .milliseconds(500))
        g.fired(at: t0 + .seconds(1))
        #expect(g.arrive(at: t0 + .seconds(3)) == .now)
    }
}
