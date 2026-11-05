import Foundation
import Testing
@testable import PersonaKit

/// Chats not on screen are pulled at most once a minute while their agent works, and at once when its turn ends
/// (`AgentSyncPacing.heldUntil`): each pull decodes and rewrites the chat's whole disk cache.
struct AgentSyncPacingTests {
    let t0 = Date(timeIntervalSinceReferenceDate: 1000)

    @Test func aWorkingAgentWaitsForItsTurn() {
        #expect(AgentSyncPacing.heldUntil(working: true, lastPull: nil, now: t0) == nil) // never pulled: now
        let held = AgentSyncPacing.heldUntil(working: true, lastPull: t0, now: t0.addingTimeInterval(10))
        #expect(held == t0.addingTimeInterval(AgentSyncPacing.workingGap))
        #expect(AgentSyncPacing.heldUntil(working: true, lastPull: t0, now: t0.addingTimeInterval(AgentSyncPacing.workingGap)) == nil)
    }

    @Test func aTurnEndingIsPulledAtOnce() {
        #expect(AgentSyncPacing.heldUntil(working: false, lastPull: t0, now: t0.addingTimeInterval(1)) == nil)
    }
}
