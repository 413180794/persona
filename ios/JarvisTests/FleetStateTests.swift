import Foundation
import Testing
@testable import Jarvis

struct FleetStateTests {
    /// Exactly what hub/liveactivity_test.go pins as the FleetState JSON, read the way ActivityKit reads a push:
    /// a plain JSONDecoder (no date strategy), so times must be plain numbers.
    @Test func decodesTheHubsJSONWithAPlainDecoder() throws {
        let json = #"{"phase":"working","needsYou":0,"working":1,"unread":0,"items":[{"id":"m/s","title":"api","place":"","kind":"working","detail":"","since":1761700000}],"workingSince":1761700000,"details":false}"#
        let s = try JSONDecoder().decode(FleetState.self, from: Data(json.utf8))
        #expect(s.phase == .working)
        #expect(s.working == 1)
        #expect(s.items.first?.kind == .working)
        #expect(s.workingSinceDate == Date(timeIntervalSince1970: 1761700000))
        #expect(s.items.first?.key?.machine == "m")
        #expect(s.items.first?.key?.session == "s")
    }

    /// A newer hub must never kill the activity: unknown phases / kinds and missing fields decode to something.
    @Test func lenientDecoding() throws {
        let s = try JSONDecoder().decode(FleetState.self, from: Data(#"{"phase":"later","items":[{"id":"a/b","kind":"teleport"}]}"#.utf8))
        #expect(s.phase == .none)
        #expect(s.isEmpty)
        #expect(s.items.first?.kind == .screen)
        let empty = try JSONDecoder().decode(FleetState.self, from: Data("{}".utf8))
        #expect(empty == FleetState())
    }

    @Test func roundTripsThroughItsOwnEncoding() throws {
        let s = FleetState(phase: .needsYou, needsYou: 1, items: [.init(id: "m/s", title: "t", kind: .plan, detail: "d", interaction: "ix")], details: true)
        #expect(try JSONDecoder().decode(FleetState.self, from: JSONEncoder().encode(s)) == s)
    }

    @Test func hiddenAndActionable() {
        let s = FleetState(phase: .needsYou, needsYou: 2, working: 3, unread: 1, items: [
            .init(id: "a/1", title: "q", kind: .question, interaction: "x"),
            .init(id: "a/2", title: "p", kind: .permission, interaction: "y"),
            .init(id: "a/3", title: "w", kind: .working),
        ])
        #expect(s.hidden(showing: 2) == 4)
        #expect(s.hidden(showing: 10) == 3)
        #expect(s.firstActionable?.id == "a/2") // questions need the screen
    }

    @Test func wording() {
        #expect(FleetText.headline(FleetState(phase: .needsYou, needsYou: 2)) == "2 个需要你")
        #expect(FleetText.headline(FleetState(phase: .working, working: 3)) == "3 个在工作")
        #expect(FleetText.headline(FleetState(phase: .done, unread: 1)) == "1 个做完了")
        #expect(FleetText.line(.init(id: "a/b", title: "", kind: .permission, detail: "Bash: ls")) == "授权：Bash: ls")
        #expect(FleetText.line(.init(id: "a/b", title: "", kind: .plan)) == "计划等你批准")
        #expect(FleetText.anonymous(.init(id: "a/b", title: "secret", kind: .permission)) == "有个授权等你")
        #expect(FleetText.more(4) == "+4 个")
    }

    @Test func deepLinks() throws {
        let url = try #require(FleetLink.url(.init(id: "box/abc-123", title: "", kind: .working)))
        #expect(url.absoluteString == "jarvis://open?machine=box&session=abc-123")
        let back = try #require(FleetLink.session(url))
        #expect(back.machine == "box" && back.session == "abc-123")
        #expect(FleetLink.session(URL(string: "jarvis://open")!) == nil)
        #expect(FleetLink.session(URL(string: "jarvis://pair?url=x&token=y")!) == nil)
    }
}
