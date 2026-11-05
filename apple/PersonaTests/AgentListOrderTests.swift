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

    @Test func rowMenuForwardsEachActionOnceAndUsesUpdatedCallbacks() {
        let actions = AgentRowMenu.Actions()
        let first = session("first", "working")
        let second = session("second", "working")
        actions.close(first)
        actions.rename(first)
        var events: [String] = []
        actions.update(rename: { events.append("rename:" + $0.sessionID) },
                       close: { events.append("close:" + $0.sessionID) })
        actions.rename(first)
        actions.close(second)
        #expect(events == ["rename:first", "close:second"])
        actions.update(rename: { events.append("new-rename:" + $0.sessionID) },
                       close: { events.append("new-close:" + $0.sessionID) })
        actions.close(first)
        actions.rename(second)
        #expect(events == ["rename:first", "close:second", "new-close:first", "new-rename:second"])
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

    // MARK: The open agent held in place (#2213)

    @Test func theOpenAgentSeenByOpeningKeepsItsPlaceUntilLeft() {
        let read = session("read", "idle", updated: 30)
        let list = [session("work", "working", updated: 10), read, session("old", "idle", updated: 40)]
        let held = AgentListOrder(isPinned: { _ in false }, isMuted: { _ in false }, held: read.key)
        #expect(held.stays(read), "out of 还有 N 个空闲 while it is open")
        #expect(held.rank(read) == 1, "still 完成未看's place")
        #expect(held.ordered(list).map(\.sessionID) == ["read", "work", "old"])
        let left = order()
        #expect(!left.stays(read) && left.rank(read) == 5, "folds as soon as it is left")
    }

    @Test func aQuietSpaceWithTheHeldAgentIsNotQuiet() {
        let read = session("read", "idle")
        let g = SpaceGroup(id: "s", machineID: "m", spaceID: "w1", group: "", name: "app", index: 0, sessions: [read])
        #expect(order().isQuiet(g))
        #expect(!AgentListOrder(isPinned: { _ in false }, isMuted: { _ in false }, held: read.key).isQuiet(g))
    }

    @Test func aHeldAgentThatIsWorkingAgainRanksAsWorking() {
        let s = session("again", "working")
        #expect(AgentListOrder(isPinned: { _ in false }, isMuted: { _ in false }, held: s.key).rank(s) == 2)
    }

    // MARK: The rows in order (↑ / ↓, #2213)

    private func rows(_ grouping: DevGrouping, folded: Set<String> = [], expandedIdle: Set<String> = [],
                      held: SessionKey? = nil) -> AgentListRows {
        AgentListRows(order: AgentListOrder(isPinned: { _ in false }, isMuted: { _ in false }, held: held), grouping: grouping,
                      isFolded: { folded.contains($0) }, expandedIdle: expandedIdle)
    }

    @Test func aSpaceListsBusyRowsAndItsIdleOnesOnlyWhenOpened() {
        let list = [session("idle", "idle", updated: 50), session("work", "working", updated: 10), session("needs", "needs_you")]
        let g = SpaceGroup(id: "s", machineID: "m", spaceID: "w1", group: "", name: "app", index: 0, sessions: list)
        #expect(rows(.space).spaces(active: [g], quiet: [], hosts: nil).map(\.sessionID) == ["needs", "work"])
        #expect(rows(.space, expandedIdle: ["s"]).spaces(active: [g], quiet: [], hosts: nil).map(\.sessionID) == ["needs", "work", "idle"])
        #expect(rows(.space, folded: ["s"]).spaces(active: [g], quiet: [], hosts: nil).isEmpty, "a folded space hides its rows")
    }

    @Test func stateSectionsFollowTheRanksAndTheFolds() {
        let live = [session("idle", "idle"), session("work", "working"), session("unread", "unread"), session("needs", "needs_you")]
        #expect(rows(.state, folded: ["state/idle"]).states(live).map(\.sessionID) == ["needs", "unread", "work"])
        #expect(rows(.state).states(live).map(\.sessionID) == ["needs", "unread", "work", "idle"])
    }

    @Test func theHeldAgentStaysWhereTheArrowsFoundIt() {
        let read = session("read", "idle", updated: 30)
        let live = [session("work", "working", updated: 10), read]
        let g = SpaceGroup(id: "s", machineID: "m", spaceID: "w1", group: "", name: "app", index: 0, sessions: live)
        let shown = rows(.space, held: read.key).spaces(active: [g], quiet: [], hosts: nil).map(\.key)
        #expect(shown == [read.key, live[0].key])
        #expect(AgentListRows.step(shown, from: read.key, by: 1) == live[0].key)
        #expect(rows(.state, folded: ["state/idle"], held: read.key).states(live).map(\.sessionID) == ["read", "work"])
    }

    @Test func stepGoesToTheNeighbourAndStopsAtTheEnds() {
        let keys = ["a", "b", "c"].map { session($0, "working").key }
        #expect(AgentListRows.step(keys, from: keys[1], by: 1) == keys[2])
        #expect(AgentListRows.step(keys, from: keys[1], by: -1) == keys[0])
        #expect(AgentListRows.step(keys, from: keys[2], by: 1) == nil)
        #expect(AgentListRows.step(keys, from: nil, by: 1) == keys[0], "nothing open: ↓ opens the first")
        #expect(AgentListRows.step(keys, from: nil, by: -1) == keys[2])
        #expect(AgentListRows.step([], from: nil, by: 1) == nil)
    }

    @Test func machinesListTheirBusyRowsAndOpenedIdleOnes() {
        let a = session("a", "working", machine: "m1"), b = session("b", "idle", machine: "m1"), c = session("c", "unread", machine: "m2")
        let refs = [c.machineRef, a.machineRef]
        #expect(rows(.machine).machines([a, b, c], order: refs).map(\.sessionID) == ["c", "a"])
        #expect(rows(.machine, expandedIdle: ["machine-" + a.machineRef.path]).machines([a, b, c], order: refs).map(\.sessionID) == ["c", "a", "b"])
    }
}
