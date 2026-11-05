import Foundation
import Testing
@testable import Jarvis

/// 思考强度胶囊里切模型 (#3119, the web's #2434): which sessions offer it, when it waits, and what goes to the hub.
struct ModelSwitchTests {
    private func session(_ extra: String = "", state: String = "idle") throws -> Session {
        try JSON.decoder.decode(Session.self, from: Data(
            #"{"machine_id":"box","session_id":"s1","status":"idle","state":"\#(state)","pane_id":"p1"\#(extra)}"#.utf8))
    }

    @Test func onlyClaudeCodeInAPane() throws {
        #expect(SessionModes.switchesModel(try session()))
        #expect(SessionModes.switchesModel(try session(#","agent":"claude""#)))
        #expect(!SessionModes.switchesModel(try session(#","agent":"codex""#)))
        #expect(!SessionModes.switchesModel(try session(#","agent":"traex""#)))
        #expect(!SessionModes.switchesModel(try session(#","assistant":true"#)))
        let noPane = try JSON.decoder.decode(Session.self, from: Data(#"{"machine_id":"box","session_id":"s1","status":"idle","state":"idle"}"#.utf8))
        #expect(!SessionModes.switchesModel(noPane))
        #expect(!SessionModes.switchesModel(nil))
    }

    @Test func waitsWhileItWorksOrAsks() throws {
        #expect(SessionModes.modelBusy(try session()) == nil)
        #expect(SessionModes.modelBusy(try session(state: "unread")) == nil)
        #expect(SessionModes.modelBusy(try session(state: "working")) != nil)
        #expect(SessionModes.modelBusy(try session(state: "needs_you")) != nil)
        #expect(SessionModes.modelBusy(try session(state: "offline")) != nil)
    }

    @MainActor @Test func thePickItRunsSendsNothing() {
        #expect(SessionModes.modelPatch("opus", current: "claude-opus-5-5-20260101") == nil)
        #expect(SessionModes.modelPatch("sonnet", current: "claude-opus-5-5-20260101") == "sonnet")
        #expect(SessionModes.modelPatch("default", current: nil) == nil)
        #expect(SessionModes.modelPatch("haiku", current: nil) == "haiku")
    }

    @Test func modesCarryTheModelAlone() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/sessions/box/s1/modes", json: #"{"ok":true}"#)
        let key = SessionKey(machineID: "box", sessionID: "s1")
        try await hub.client.setModes(key, model: "sonnet")
        try await hub.client.setModes(key, effort: "high")
        let sent = hub.sent("POST", "/api/v1/sessions/box/s1/modes")
        #expect(sent.count == 2)
        let a = try #require(sent.first?.body), b = try #require(sent.last?.body)
        let first = try #require(try JSONSerialization.jsonObject(with: a) as? [String: String])
        #expect(first == ["model": "sonnet"])
        let second = try #require(try JSONSerialization.jsonObject(with: b) as? [String: String])
        #expect(second == ["effort": "high"]) // an older caller's body is unchanged
    }
}
