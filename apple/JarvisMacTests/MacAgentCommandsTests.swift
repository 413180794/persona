import Foundation
import Testing
@testable import Jarvis

/// The Agent menu (docs/mac.md「菜单和快捷键」): acts on the chat on screen.
@MainActor
struct MacAgentCommandsTests {
    private func ix(_ json: String) throws -> Interaction { try JSON.decoder.decode(Interaction.self, from: Data(json.utf8)) }

    private func session(_ id: String, _ state: AgentState, ago: TimeInterval = 0, assistant: Bool = false) -> Session {
        var s = try! JSON.decoder.decode(Session.self, from: Data("""
        {"machine_id":"m","session_id":"\(id)","title":"t","cwd":"/w","status":"idle","state":"\(state.rawValue)",
         "started_by_jarvis":false,"assistant":\(assistant),"pending_interactions":0}
        """.utf8))
        s.stateSince = Date(timeIntervalSince1970: 1_000_000 - ago)
        return s
    }

    @Test func approveAllowsAToolOrAPlan() throws {
        let p = try ix(#"{"id":"1","machine_id":"m","session_id":"a","kind":"permission"}"#)
        #expect(MacAgentCommands.approve(p)?.allow == true)
        let plan = try ix(#"{"id":"2","machine_id":"m","session_id":"a","kind":"plan","plan":"x"}"#)
        #expect(MacAgentCommands.approve(plan)?.approve == true)
        #expect(MacAgentCommands.approve(plan)?.mode == "acceptEdits")
        // A question or a terminal dialog has no single "yes".
        let q = try ix(#"{"id":"3","machine_id":"m","session_id":"a","kind":"question","questions":[]}"#)
        #expect(MacAgentCommands.approve(q) == nil)
    }

    @Test func rejectOnlyDeniesATool() throws {
        let p = try ix(#"{"id":"1","machine_id":"m","session_id":"a","kind":"permission"}"#)
        #expect(MacAgentCommands.reject(p)?.allow == false)
        // Sending a plan back needs words: that is the chat's 打回.
        let plan = try ix(#"{"id":"2","machine_id":"m","session_id":"a","kind":"plan","plan":"x"}"#)
        #expect(MacAgentCommands.reject(plan) == nil)
    }

    @Test func nextNeedsYouCyclesLongestWaitingFirst() {
        let list = [session("a", .needsYou, ago: 10), session("b", .needsYou, ago: 300), session("c", .working),
                    session("d", .needsYou, ago: 60), session("j", .needsYou, ago: 999, assistant: true)]
        let k = { SessionKey(machineID: "m", sessionID: $0) }
        #expect(MacAgentCommands.nextNeedsYou(after: nil, in: list, muted: { _ in false }) == k("j"))
        #expect(MacAgentCommands.nextNeedsYou(after: k("j"), in: list, muted: { _ in false }) == k("b"))
        #expect(MacAgentCommands.nextNeedsYou(after: k("b"), in: list, muted: { _ in false }) == k("d"))
        #expect(MacAgentCommands.nextNeedsYou(after: k("a"), in: list, muted: { _ in false }) == k("j"))
        #expect(MacAgentCommands.nextNeedsYou(after: nil, in: [session("c", .working)], muted: { _ in false }) == nil)
    }
}

/// Which chat the Agent menu acts on: the key window's (a chat can be open in the main window and in a window of its
/// own at once), not just the last one that appeared.
struct MacFocusedChatTests {
    let a = SessionKey(machineID: "m", sessionID: "a"), b = SessionKey(machineID: "m", sessionID: "b")

    @Test func theKeyWindowsChat() {
        #expect(MacShellState.focusedChat(keyWindow: 1, chats: [1: a, 2: b], fallback: b) == a)
    }

    /// The key window shows no chat (知识库, settings): nothing to act on, not the other window's chat.
    @Test func aKeyWindowWithoutAChat() {
        #expect(MacShellState.focusedChat(keyWindow: 3, chats: [1: a], fallback: a) == nil)
    }

    /// No key window (the app is in the background, a menu bar menu is open): the last chat shown.
    @Test func noKeyWindow() {
        #expect(MacShellState.focusedChat(keyWindow: nil, chats: [1: a], fallback: b) == b)
    }
}
