import Foundation
import Testing
@testable import Jarvis

/// 显示 › 字号 ⌘+ ⌘- ⌘0 and 前往 › 上一个 / 下一个 Agent ⌘[ ⌘] (docs/mac.md「菜单和快捷键」).
@MainActor
struct MacViewCommandsTests {
    @Test func textSizeSteps() {
        #expect(MacViewCommands.step(.standard, by: 1) == .large)
        #expect(MacViewCommands.step(.large, by: 1) == .large)
        #expect(MacViewCommands.step(.standard, by: -1) == .small)
        #expect(MacViewCommands.step(.small, by: -1) == .small)
    }

    private func session(_ id: String, _ state: AgentState, assistant: Bool = false) -> Session {
        try! JSON.decoder.decode(Session.self, from: Data("""
        {"machine_id":"m","session_id":"\(id)","title":"t","cwd":"/w","status":"idle","state":"\(state.rawValue)",
         "started_by_jarvis":false,"assistant":\(assistant),"pending_interactions":0}
        """.utf8))
    }

    /// The Agents list's order (needs you → unread → working → seen), Jarvis conversations left out, wrapping.
    @Test func neighboursInTheListsOrder() {
        let list = [session("seen", .idle), session("w", .working), session("n", .needsYou), session("j", .needsYou, assistant: true)]
        let order = AgentListOrder(isPinned: { _ in false }, isMuted: { _ in false })
        let k = { SessionKey(machineID: "m", sessionID: $0) }
        #expect(MacViewCommands.neighbour(of: k("n"), by: 1, in: list, order: order) == k("w"))
        #expect(MacViewCommands.neighbour(of: k("seen"), by: 1, in: list, order: order) == k("n"))
        #expect(MacViewCommands.neighbour(of: k("n"), by: -1, in: list, order: order) == k("seen"))
        #expect(MacViewCommands.neighbour(of: nil, by: 1, in: list, order: order) == k("n"))
        #expect(MacViewCommands.neighbour(of: nil, by: 1, in: [], order: order) == nil)
    }
}
