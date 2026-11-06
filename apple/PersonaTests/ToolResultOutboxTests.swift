import Foundation
import Testing
@testable import Persona

/// Phone tool results the socket cannot take yet are kept and sent once it connects (#4456).
@MainActor
struct ToolResultOutboxTests {
    @MainActor final class FakeLink: ToolResultLink {
        var canSend: Bool
        var sent: [[String: Any]] = []
        init(up: Bool) { canSend = up }
        func send(_ object: [String: Any]) { sent.append(object) }
        var callIDs: [String] { sent.compactMap { $0["call_id"] as? String } }
    }

    private func result(_ id: String) -> [String: Any] {
        ["type": "tool_result", "call_id": id, "content": [["type": "text", "text": "ok"]], "is_error": false]
    }

    private let t0 = Date(timeIntervalSince1970: 1_800_000_000)

    @Test func aConnectedSocketTakesItAtOnce() {
        let out = ToolResultOutbox(), link = FakeLink(up: true)
        out.send(result("c1"), callID: "c1", group: "", on: link, now: t0)
        #expect(link.callIDs == ["c1"] && out.count("") == 0)
    }

    /// Allow tapped just after coming back from the background: the socket is still reconnecting.
    @Test func aResultWhileDisconnectedGoesOnceTheGroupConnects() {
        let out = ToolResultOutbox(), link = FakeLink(up: false)
        out.send(result("c1"), callID: "c1", group: "", on: link, now: t0)
        out.send(result("c2"), callID: "c2", group: "", on: nil, now: t0) // no socket at all yet
        #expect(link.sent.isEmpty && out.count("") == 2)
        out.flush(group: "", on: link, now: t0.addingTimeInterval(5)) // still down: nothing yet
        #expect(link.sent.isEmpty && out.count("") == 2)
        link.canSend = true
        out.flush(group: "", on: link, now: t0.addingTimeInterval(10))
        #expect(link.callIDs == ["c1", "c2"] && out.count("") == 0)
        out.flush(group: "", on: link, now: t0.addingTimeInterval(11)) // sent once
        #expect(link.callIDs == ["c1", "c2"])
    }

    @Test func eachGroupKeepsItsOwn() {
        let out = ToolResultOutbox(), down = FakeLink(up: false), other = FakeLink(up: true)
        out.send(result("c1"), callID: "c1", group: "g2", on: down, now: t0)
        out.flush(group: "", on: other, now: t0)
        #expect(other.sent.isEmpty && out.count("g2") == 1)
    }

    @Test func whatTheHubNoLongerWaitsForIsDropped() {
        let out = ToolResultOutbox(), link = FakeLink(up: false)
        out.send(result("old"), callID: "old", group: "", on: link, now: t0)
        out.send(result("new"), callID: "new", group: "", on: link, now: t0.addingTimeInterval(ToolResultOutbox.hubWait - 60))
        link.canSend = true
        out.flush(group: "", on: link, now: t0.addingTimeInterval(ToolResultOutbox.hubWait + 1))
        #expect(link.callIDs == ["new"])
    }

    @Test func itKeepsAFewAndOneAnswerPerCall() {
        let out = ToolResultOutbox(), link = FakeLink(up: false)
        for i in 0..<(ToolResultOutbox.most + 4) { out.send(result("c\(i)"), callID: "c\(i)", group: "", on: link, now: t0) }
        out.send(result("c5"), callID: "c5", group: "", on: link, now: t0) // the same call again: one copy
        #expect(out.count("") == ToolResultOutbox.most)
        link.canSend = true
        out.flush(group: "", on: link, now: t0)
        #expect(link.callIDs.count == ToolResultOutbox.most && Set(link.callIDs).count == ToolResultOutbox.most)
        #expect(!link.callIDs.contains("c0") && link.callIDs.last == "c5")
    }

    @Test func aForgottenGroupDropsWhatItKept() {
        let out = ToolResultOutbox(), link = FakeLink(up: false)
        out.send(result("c1"), callID: "c1", group: "g2", on: link, now: t0)
        out.forget(groups: ["g2"])
        link.canSend = true
        out.flush(group: "g2", on: link, now: t0)
        #expect(link.sent.isEmpty)
    }
}
