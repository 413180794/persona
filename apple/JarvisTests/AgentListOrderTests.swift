import Foundation
import Testing
@testable import Jarvis

/// The Agents list's order and sections (`AgentListOrder`), without the list.
@MainActor struct AgentListOrderTests {
    private func session(_ id: String, _ state: String, updated: TimeInterval = 0, machine: String = "m") -> Session {
        let json = """
        {"machine_id":"\(machine)","session_id":"\(id)","title":"\(id)","cwd":"/tmp/p","status":"idle","state":"\(state)",
         "space_id":"w1","space_name":"app","updated_at":"\(ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: updated)))"}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) }
        catch { fatalError("bad fixture: \(error)") }
    }

    private func order(pinned: Set<String> = [], muted: Set<String> = [], byRank: Bool = true) -> AgentListOrder {
        AgentListOrder(isPinned: { pinned.contains($0.sessionID) }, isMuted: { muted.contains($0.sessionID) }, byRank: byRank)
    }

    @Test func statesComeInOneOrder() {
        let list = [session("idle", "idle", updated: 50), session("work", "working", updated: 10),
                    session("unread", "unread", updated: 20), session("needs", "needs_you", updated: 1)]
        #expect(order().ordered(list).map(\.sessionID) == ["needs", "unread", "work", "idle"])
    }

    @Test func mutedLiveAgentsGoBelowTheRestButNeedsYouStaysFirst() {
        let list = [session("a", "unread"), session("b", "working"), session("c", "needs_you"), session("d", "idle")]
        let o = order(muted: ["a", "c"])
        #expect(o.rank(list[0]) == 4, "muted and unread ranks as 已屏蔽")
        #expect(o.rank(list[2]) == 0, "muted but needs you stays first")
        #expect(o.ordered(list).map(\.sessionID) == ["c", "b", "a", "d"])
    }

    @Test func sameRankNewestFirstThenById() {
        let list = [session("b", "unread", updated: 5), session("a", "unread", updated: 5), session("c", "unread", updated: 9)]
        #expect(order().ordered(list).map(\.sessionID) == ["c", "a", "b"])
    }

    @Test func underAQuickFilterOnlyTimeCounts() {
        let list = [session("needs", "needs_you", updated: 1), session("idle", "idle", updated: 9)]
        #expect(order(byRank: false).ordered(list).map(\.sessionID) == ["idle", "needs"])
    }

    @Test func pinnedIdleAgentsStayOutOfTheFold() {
        let o = order(pinned: ["p"])
        #expect(o.stays(session("p", "idle")))
        #expect(!o.stays(session("q", "idle")))
        #expect(o.stays(session("w", "working")))
    }

    @Test func sectionsAreTheRanksAndMachinesInUse() {
        let list = [session("a", "needs_you", machine: "x"), session("b", "idle", machine: "y"), session("c", "offline", machine: "z")]
        #expect(order().sectionIDs(list, byMachine: false) == ["state/needs", "state/idle"])
        #expect(Set(order().sectionIDs(list, byMachine: true)) == ["machine/x", "machine/y"], "offline agents make no section")
    }
}
