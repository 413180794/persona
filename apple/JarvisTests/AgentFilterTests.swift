import Foundation
import Testing
@testable import Jarvis

/// The Agents list's quick filters and what a space header says when folded (AgentsListTests drives the screens).
@MainActor struct AgentFilterTests {
    private func session(_ id: String, state: String, updated: TimeInterval = 0, since: TimeInterval = 0) -> Session {
        let f = ISO8601DateFormatter()
        let json = """
        {"machine_id":"m","session_id":"\(id)","title":"\(id)","cwd":"/tmp/p","status":"idle","state":"\(state)",
         "space_id":"w1","space_name":"app","updated_at":"\(f.string(from: Date(timeIntervalSince1970: updated)))",
         "state_since":"\(f.string(from: Date(timeIntervalSince1970: since)))"}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) }
        catch { fatalError("bad fixture: \(error)") }
    }

    @Test func eachFilterKeepsItsState() {
        let all = [session("a", state: "needs_you"), session("b", state: "unread"), session("c", state: "unread"),
                   session("d", state: "working"), session("e", state: "idle")]
        #expect(all.filter { AgentFilter.all.matches($0) }.count == 5)
        #expect(all.filter { AgentFilter.needsYou.matches($0) }.map(\.sessionID) == ["a"])
        #expect(all.filter { AgentFilter.unread.matches($0) }.map(\.sessionID) == ["b", "c"])
        #expect(all.filter { AgentFilter.working.matches($0) }.map(\.sessionID) == ["d"])
        #expect(AgentFilter.all.state == nil)
    }

    /// Muted agents sit under 已屏蔽: 未读 and 工作中 neither count nor list them; 需要你 still does.
    @Test func mutedAgentsAreNotUnreadOrWorking() {
        let unread = session("b", state: "unread"), working = session("d", state: "working"), needs = session("a", state: "needs_you")
        #expect(!AgentFilter.unread.matches(unread, muted: true))
        #expect(!AgentFilter.working.matches(working, muted: true))
        #expect(AgentFilter.needsYou.matches(needs, muted: true))
        #expect(AgentFilter.all.matches(unread, muted: true))
        #expect(AgentFilter.unread.matches(unread, muted: false))
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

    /// Working spaces keep their order while their agents work: by when they started, not their latest line. The real
    /// hub pushes every two seconds; ordered by the latest update the spaces traded places under the finger (2026-11-01).
    @Test func workingSpacesKeepTheirOrderAsTheyWork() {
        func space(_ id: String, _ name: String, updated: TimeInterval, since: TimeInterval) -> Session {
            var s = session(id, state: "working", updated: updated, since: since)
            s.spaceID = name
            s.spaceName = name
            return s
        }
        let started = SpaceGroup.build([space("a", "api", updated: 500, since: 100), space("b", "web", updated: 300, since: 200)]).map(\.spaceID)
        #expect(started == ["web", "api"]) // the later start leads, whoever wrote last
        let later = SpaceGroup.build([space("a", "api", updated: 300, since: 100), space("b", "web", updated: 700, since: 200)]).map(\.spaceID)
        #expect(later == started)
        // Finished: by when they changed, as before.
        var docs = session("c", state: "unread", updated: 900, since: 400), site = session("d", state: "unread", updated: 800, since: 600)
        docs.spaceID = "docs"
        site.spaceID = "site"
        #expect(SpaceGroup.build([site, docs]).map(\.spaceID) == ["docs", "site"])
    }
}
