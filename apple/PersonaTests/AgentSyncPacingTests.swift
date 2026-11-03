import Foundation
import Testing
@testable import Jarvis

/// Chats not on screen are pulled at most once a minute while their agent works, and at once when its turn ends
/// (`AgentSync.heldUntil`): each pull decodes and rewrites the chat's whole disk cache.
struct AgentSyncPacingTests {
    let t0 = Date(timeIntervalSinceReferenceDate: 1000)

    @Test func aWorkingAgentWaitsForItsTurn() {
        #expect(AgentSync.heldUntil(working: true, lastPull: nil, now: t0) == nil) // never pulled: now
        let held = AgentSync.heldUntil(working: true, lastPull: t0, now: t0.addingTimeInterval(10))
        #expect(held == t0.addingTimeInterval(AgentSync.workingGap))
        #expect(AgentSync.heldUntil(working: true, lastPull: t0, now: t0.addingTimeInterval(AgentSync.workingGap)) == nil)
    }

    @Test func aTurnEndingIsPulledAtOnce() {
        #expect(AgentSync.heldUntil(working: false, lastPull: t0, now: t0.addingTimeInterval(1)) == nil)
    }
}
