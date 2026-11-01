import Foundation
import SwiftUI
import Testing
@testable import Jarvis

/// The persona on the Live Activity (docs/live-activity.md「形象」): which state it shows for a fleet, and the look
/// the app hands the widget extension.
struct FleetPersonaTests {
    @Test func stateFollowsThePhase() {
        #expect(FleetPersona.state(FleetState(phase: .needsYou, needsYou: 1, working: 2), stale: false) == .needsYou)
        #expect(FleetPersona.state(FleetState(phase: .working, working: 2), stale: false) == .working)
        // The assistant answering does not make working agents "thinking".
        #expect(FleetPersona.state(FleetState(phase: .working, working: 2, assistant: true), stale: false) == .working)
        #expect(FleetPersona.state(FleetState(phase: .done, unread: 1), stale: false) == .idle)
        #expect(FleetPersona.state(FleetState(), stale: false) == .idle)
    }

    /// Only the assistant at work (no agent working or waiting): 思考中, the nearest the fleet has.
    @Test func assistantAloneIsThinking() {
        #expect(FleetPersona.state(FleetState(phase: .done, unread: 1, assistant: true), stale: false) == .thinking)
        #expect(FleetPersona.state(FleetState(assistant: true), stale: false) == .thinking)
    }

    /// Stale content (the hub stopped pushing) wins over everything: 离线, gray.
    @Test func staleIsOffline() {
        for phase in [FleetState.Phase.needsYou, .working, .done, .none] {
            #expect(FleetPersona.state(FleetState(phase: phase), stale: true) == .offline)
        }
    }

    /// The look carries the color the app resolved in each appearance, so the extension draws the same accent.
    @Test func lookResolvesLightAndDark() {
        let look = FleetLook(design: .circuit, tint: Palette.coral)
        #expect(look.design == "circuit")
        #expect(look.faceDesign == .circuit)
        #expect(look.light == 0xD4683F)
        #expect(look.dark == 0xE8835C)
    }

    @Test func unknownDesignFallsBackToTheDefaultFace() throws {
        let look = try JSONDecoder().decode(FleetLook.self, from: Data(#"{"design":"later","light":1,"dark":2}"#.utf8))
        #expect(look.faceDesign == .visor)
    }

    @Test func lookRoundTrips() throws {
        let look = FleetLook(design: .visor, tint: Palette.systemBlue)
        #expect(try JSONDecoder().decode(FleetLook.self, from: JSONEncoder().encode(look)) == look)
        #expect(look == FleetLook.standard)
    }

    /// The persona does not ride in the ContentState: an activity the hub pushes today (or pushed before this change)
    /// decodes to the same state, and the state encodes no look of its own.
    @Test func contentStateUnchanged() throws {
        let json = #"{"phase":"needsYou","needsYou":1,"working":0,"unread":0,"items":[],"details":false,"assistant":true,"left":"status","right":"tpm"}"#
        let s = try JSONDecoder().decode(FleetState.self, from: Data(json.utf8))
        #expect(FleetPersona.state(s, stale: false) == .needsYou)
        let out = try JSONSerialization.jsonObject(with: JSONEncoder().encode(s)) as? [String: Any]
        #expect(out?["design"] == nil)
        #expect(out?["look"] == nil)
    }
}
