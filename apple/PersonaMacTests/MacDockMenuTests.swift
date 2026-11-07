import AppKit
import Foundation
import Testing
@testable import Persona

/// Right-click on the Dock icon: what needs you, then what just finished, each opening its chat; New Agent Chat last.
@MainActor
struct MacDockMenuTests {
    private func session(_ id: String, _ state: AgentState) -> Session {
        try! JSON.decoder.decode(Session.self, from: Data("""
        {"machine_id":"m","session_id":"\(id)","title":"t-\(id)","cwd":"/w/proj","status":"idle","state":"\(state.rawValue)",
         "started_by_persona":false,"assistant":false,"pending_interactions":0}
        """.utf8))
    }

    @Test func needsYouThenDoneThenNew() {
        let s = MacMenuBar.Sections(needsYou: [session("n", .needsYou)], working: [session("w", .working)],
                                    unread: [session("u", .unread)])
        let menu = MacDockMenu.shared.menu(s, machineName: { _ in "MacBook" }, paired: true)
        #expect(menu.items.map(\.title) == ["需要你", "t-n · MacBook", "完成未看", "t-u · MacBook", "", "新建 Agent 对话…"])
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

    /// A quiet task (#1731) is the main conversation's to tell: the Dock menu runs the same filter the menu bar
    /// does before grouping into sections, so none of its agents shows, in either section.
    @Test func quietTaskAgentsAreLeftOutOfBothSections() {
        let list = [session("qn", .needsYou), session("n", .needsYou),
                    session("qu", .unread), session("u", .unread)]
        let sections = MacMenuBar.sections(MacMenuBar.menuSessions(list, quiet: { $0.sessionID.hasPrefix("q") }),
                                           muted: { _ in false })
        let titles = MacDockMenu.shared.menu(sections, machineName: { _ in "MacBook" }, paired: true)
            .items.map(\.title)
        #expect(titles == ["需要你", "t-n · MacBook", "完成未看", "t-u · MacBook", "", "新建 Agent 对话…"])
        #expect(titles.allSatisfy { !$0.contains("t-qn") && !$0.contains("t-qu") })
    }
}
