import Foundation
import Testing
@testable import Jarvis

struct SyncEventTests {
    @Test func decodesRevs() throws {
        let data = Data(#"{"type":"revs","machine_id":"m1","revs":{"s1":120,"s2":7}}"#.utf8)
        guard case .revs(let machine, let revs) = try #require(ServerEvent.decode(data)) else { Issue.record("not revs"); return }
        #expect(machine == "m1")
        #expect(revs == ["s1": 120, "s2": 7])
    }

    @Test func decodesLark() throws {
        let data = Data(#"{"type":"lark","feed_ids":["a","b"]}"#.utf8)
        guard case .lark(let ids, let settingsChanged) = try #require(ServerEvent.decode(data)) else { Issue.record("not lark"); return }
        #expect(ids == ["a", "b"])
        #expect(!settingsChanged)
    }

    @Test func decodesSettingsBatchedWithMessages() throws {
        let data = Data(#"{"type":"lark","feed_ids":["a"],"settings_changed":true}"#.utf8)
        guard case .lark(let ids, let settingsChanged) = try #require(ServerEvent.decode(data)) else { Issue.record("not lark"); return }
        #expect(ids == ["a"] && settingsChanged)
    }

    @Test func decodesAgents() throws {
        let data = Data(#"{"type":"agents","session_ids":["removedbridge:agent:grok_bot:a:b/"]}"#.utf8)
        guard case .agents(let ids) = try #require(ServerEvent.decode(data)) else { Issue.record("not agents"); return }
        #expect(ids == ["removedbridge:agent:grok_bot:a:b/"])
    }

    @Test func personalAgentPathEncodesSlashAndColon() {
        #expect(APIClient.agentPath("removedbridge:agent:x:y/") == "/api/v1/agents/sessions/removedbridge%3Aagent%3Ax%3Ay%2F")
    }

    @Test func personalAgentMessageKeepsUnknownBlocks() throws {
        let json = #"{"externalId":"t1","role":"assistant","blocks":[{"type":"text","text":"hi"},{"type":"tool-call","name":"web"},{"type":"image","url":"u"}],"createTimeMs":1,"position":3}"#
        let m = try JSON.decoder.decode(PersonalAgentMessage.self, from: Data(json.utf8))
        #expect(m.blocks.map(\.type) == ["text", "tool-call", "image"])
        #expect(m.blocks[1].name == "web")
        #expect(m.local == nil)
    }

    @Test func sessionCarriesRev() throws {
        let s = try JSON.decoder.decode(Session.self, from: Data(#"{"session_id":"s","status":"idle","rev":42}"#.utf8))
        #expect(s.rev == 42)
    }

    @Test func sessionCarriesModes() throws {
        let s = try JSON.decoder.decode(Session.self, from: Data(#"{"session_id":"s","status":"idle","permission_mode":"plan","effort":"xhigh"}"#.utf8))
        #expect(s.permissionMode == "plan")
        #expect(s.effort == "xhigh")
        #expect(SessionModes.modeLabel(s.permissionMode) == "只做计划")
        #expect(SessionModes.effortLabel(s.effort) == "很高")
        #expect(SessionModes.modeLabel("on-request") == "on-request") // unknown values (Codex) show as they are
        let old = try JSON.decoder.decode(Session.self, from: Data(#"{"session_id":"s","status":"idle"}"#.utf8))
        #expect(old.permissionMode == nil && old.effort == nil)
    }

    @Test func sessionSourcesDecodeConservatively() throws {
        func decode(_ body: String) throws -> Session { try JSON.decoder.decode(Session.self, from: Data(body.utf8)) }
        let old = try decode(#"{"session_id":"aabbccdd","status":"working"}"#)
        #expect(old.canRead && old.canSend && old.canInterrupt && old.canApprove && old.canTerminal)
        #expect(!old.canAttach && old.sourceSubtitle == nil)
        let missing = try decode(#"{"session_id":"opaque","source_kind":"codex_app_server"}"#)
        #expect(!missing.canRead && !missing.canSend && !missing.canInterrupt && !missing.canApprove && !missing.canTerminal && !missing.canAttach)
        let history = try decode(#"{"session_id":"opaque","source_kind":"codex_history","source_thread_id":"thread","source_instance_id":"instance","parent_thread_id":"parent","capabilities":{"read":true},"read_only_reason":"外部进程正在使用此会话"}"#)
        #expect(history.canRead && !history.canSend && !history.canInterrupt && !history.canApprove && !history.canTerminal && !history.canAttach)
        #expect(history.sourceSubtitle == "Codex 历史 · 仅查看：外部进程正在使用此会话")
        #expect(history.resumeCommand == "codex resume")
        #expect(history.sourceThreadID == "thread" && history.sourceInstanceID == "instance" && history.parentThreadID == "parent")
        let roundTrip = try JSON.decoder.decode(Session.self, from: JSON.encoder.encode(history))
        #expect(roundTrip == history)
    }

    @Test func liveSourceMenusFollowCapabilitiesNotStatus() throws {
        var live = try JSON.decoder.decode(Session.self, from: Data(#"{"session_id":"opaque","source_kind":"codex_app_server","status":"working","capabilities":{"read":true,"send":true,"attach":true},"attached":false}"#.utf8))
        #expect(live.canSend && live.canAttach && !live.canTerminal && !live.canInterrupt && !live.canApprove)
        live.attached = true
        #expect(!live.canAttach)
        live.capabilities?.interrupt = true
        #expect(live.canInterrupt)
        #expect(live.agentLabel == "Codex" && live.resumeCommand == "codex resume")
    }

    @MainActor @Test func rowMenuEqualityIncludesPermissions() {
        let actions = AgentRowMenu.Actions()
        let key = SessionKey(machineID: "m", sessionID: "s")
        let old = AgentRowMenu(key: key, pinned: false, muted: false, looping: false, live: true, actions: actions)
        let external = AgentRowMenu(key: key, pinned: false, muted: false, looping: false, live: true, terminal: false, attachable: true, actions: actions)
        #expect(old != external)
        var attached = external
        attached.attachable = false
        #expect(attached != external)
        #expect(external == external)
    }

    @Test func lazyImagesLearnTheirRef() {
        var it = Item(id: "u#1", seq: 3, ts: nil, role: "user", kind: .image, text: nil, toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)
        var img = ImagePayload(mime: "image/png", data: "")
        img.lazy = true
        it.image = img
        let out = ChatStore.withImageRefs([it], SessionKey(machineID: "m", sessionID: "s"))
        #expect(out[0].image?.ref == "m/s/u#1")
    }
}
