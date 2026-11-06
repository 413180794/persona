import Foundation
import Testing
@testable import Persona

/// What the app does with the fleet while it draws the Live Activity itself (in front: the hub pushes nothing then).
/// Only the assistant answering is something to show on an island that shows it (#4628, the app's side of #4590).
struct LiveActivityStepTests {
    private func state(phase: FleetState.Phase = .none, assistant: Bool = false, island: IslandSlot = .assistant) -> FleetState {
        var s = FleetState(phase: phase, assistant: assistant)
        (s.left, s.right) = (.status, island)
        return s
    }

    @Test func theAssistantAnsweringKeepsARunningActivity() {
        // No agent working, nothing unread: only the assistant answering, its side on the island (Thinking).
        #expect(LiveActivityController.step(state(assistant: true), running: true, active: true) == .update)
        #expect(LiveActivityController.step(state(assistant: true), running: true, active: false) == .update)
    }

    @Test func onceTheAssistantIsDoneTheMinuteRunsAsBefore() {
        #expect(LiveActivityController.step(state(assistant: false), running: true, active: true) == .endSoon)
    }

    @Test func anIslandThatDoesNotShowTheAssistantHasNothingToShow() {
        #expect(LiveActivityController.step(state(assistant: true, island: .tokens), running: true, active: true) == .endSoon)
    }

    @Test func theAssistantAloneStartsNoActivity() {
        // A product decision left open in #4628: only an activity already on is kept.
        #expect(LiveActivityController.step(state(assistant: true), running: false, active: true) == .nothing)
    }

    @Test func agentsStartAndUpdateAsBefore() {
        #expect(LiveActivityController.step(state(phase: .working), running: false, active: true) == .start)
        #expect(LiveActivityController.step(state(phase: .working), running: false, active: false) == .nothing)
        #expect(LiveActivityController.step(state(phase: .done), running: false, active: true) == .nothing)
        #expect(LiveActivityController.step(state(phase: .needsYou), running: true, active: true) == .update)
        #expect(LiveActivityController.step(state(), running: false, active: true) == .nothing)
    }

    @Test func showsNothingFollowsTheIslandsSides() {
        #expect(state().showsNothing)
        #expect(!state(assistant: true).showsNothing)
        #expect(state(assistant: true, island: .none).showsNothing)
        #expect(!state(phase: .done).showsNothing)
    }
}
