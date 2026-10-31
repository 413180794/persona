import AppKit
import Foundation
import Testing
@testable import Jarvis

/// Right-click on the Dock icon: what needs you, then what just finished, each opening its chat; 新建 Agent 对话 last.
@MainActor
struct MacDockMenuTests {
    private func session(_ id: String, _ state: AgentState) -> Session {
        try! JSON.decoder.decode(Session.self, from: Data("""
        {"machine_id":"m","session_id":"\(id)","title":"t-\(id)","cwd":"/w/proj","status":"idle","state":"\(state.rawValue)",
         "started_by_jarvis":false,"assistant":false,"pending_interactions":0}
        """.utf8))
    }

    @Test func needsYouThenDoneThenNew() {
        let s = MacMenuBar.Sections(needsYou: [session("n", .needsYou)], working: [session("w", .working)],
                                    unread: [session("u", .unread)])
        let menu = MacDockMenu.shared.menu(s, machineName: { _ in "MacBook" }, paired: true)
        #expect(menu.items.map(\.title) == ["需要你", "t-n · MacBook", "做完了", "t-u · MacBook", "", "新建 Agent 对话…"])
        #expect((menu.items.dropFirst().first?.representedObject as? SessionKey)?.sessionID == "n")
        #expect(menu.items.last?.isEnabled == true)
    }

    @Test func nothingPendingIsJustNew() {
        let menu = MacDockMenu.shared.menu(MacMenuBar.Sections(), machineName: { $0 }, paired: false)
        #expect(menu.items.map(\.title) == ["新建 Agent 对话…"])
        #expect(menu.items.last?.isEnabled == false)
    }

    @Test func atMostEightOfEach() {
        let s = MacMenuBar.Sections(needsYou: (1...12).map { session("n\($0)", .needsYou) })
        #expect(MacDockMenu.shared.menu(s, machineName: { $0 }, paired: true).items.count == 1 + 8 + 2)
    }
}
