import Foundation
import Testing
@testable import Jarvis

/// The Agents list's quick filters and what a space header says when folded (AgentsListTests drives the screens).
@MainActor struct AgentFilterTests {
    private func session(_ id: String, state: String, updated: TimeInterval = 0) -> Session {
        let json = """
        {"machine_id":"m","session_id":"\(id)","title":"\(id)","cwd":"/tmp/p","status":"idle","state":"\(state)",
         "space_id":"w1","space_name":"app","updated_at":"\(ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: updated)))"}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) }
        catch { fatalError("bad fixture: \(error)") }
    }

    @Test func eachFilterKeepsItsState() {
        let all = [session("a", state: "needs_you"), session("b", state: "unread"), session("c", state: "unread"),
                   session("d", state: "working"), session("e", state: "idle")]
        #expect(all.filter(AgentFilter.all.matches).count == 5)
        #expect(all.filter(AgentFilter.needsYou.matches).map(\.sessionID) == ["a"])
        #expect(all.filter(AgentFilter.unread.matches).map(\.sessionID) == ["b", "c"])
        #expect(all.filter(AgentFilter.working.matches).map(\.sessionID) == ["d"])
        #expect(AgentFilter.all.state == nil)
    }

    @Test func foldedHeaderSpeaksItsCounts() {
        let g = SpaceGroup.build([session("a", state: "needs_you"), session("b", state: "unread"), session("c", state: "unread"),
                                  session("d", state: "idle")]).first!
        #expect(StateCounts.spoken(g) == "1 个需要你，2 个完成未看，1 个空闲")
    }

    /// Spaces that changed at the same moment keep one order: they swapped between renders before.
    @Test func spacesWithTheSameTimeKeepTheirOrder() {
        let a = session("a", state: "unread", updated: 100)
        var b = session("b", state: "unread", updated: 100)
        b.spaceID = "w2"
        b.spaceName = "web"
        let ids = SpaceGroup.build([a, b]).map(\.id)
        for _ in 0..<20 { #expect(SpaceGroup.build([b, a]).map(\.id) == ids) }
    }
}
