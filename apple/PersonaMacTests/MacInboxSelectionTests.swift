import Foundation
import Testing
@testable import Jarvis

/// 多选后部分项过期消失：已选计数与操作只按仍在列表里的 selection 计算 (#3795)。
@MainActor
struct MacInboxSelectionTests {
    private func item(_ id: String, done: Bool = false) -> InboxItem {
        try! JSON.decoder.decode(InboxItem.self, from: Data("""
        {"id":"\(id)","source":"lark","kind":"dm","account":"","account_name":"","title":"\(id)","sender":"",
         "preview":"","count":1\(done ? #","done_at":"2026-01-01T00:00:00Z""# : "")}
        """.utf8))
    }

    private func agent(_ id: String) -> Interaction {
        try! JSON.decoder.decode(Interaction.self,
                                 from: Data(#"{"id":"\#(id)","machine_id":"m","session_id":"s","kind":"question"}"#.utf8))
    }

    private func state(_ selection: Set<MacInboxSelection>) -> MacInboxState {
        let state = MacInboxState()
        state.selection = selection
        return state
    }

    @Test func expiredItemDroppedFromLiveCount() {
        let state = state([.item("a"), .item("b"), .item("gone")])
        let live = state.liveSelection(items: [item("a"), item("b")], interactions: [])
        #expect(live == [.item("a"), .item("b")])
    }

    @Test func allExpiredLeavesLiveSelectionEmpty() {
        // Empty live selection makes MacInboxDetail fall through to the 选一件事 placeholder, not 已选 0 件.
        let state = state([.item("gone"), .agent("goneAgent")])
        let live = state.liveSelection(items: [], interactions: [])
        #expect(live.isEmpty)
    }

    @Test func vanishedAgentInteractionDropped() {
        let state = state([.item("a"), .agent("here"), .agent("gone")])
        let live = state.liveSelection(items: [item("a")], interactions: [agent("here")])
        #expect(live == [.item("a"), .agent("here")])
    }

    @Test func oneSurvivorFallsBackToSingleRow() {
        // Two selected, one expired: the survivor is the one live row the detail opens on its own.
        let state = state([.item("a"), .item("gone")])
        let live = state.liveSelection(items: [item("a")], interactions: [])
        #expect(live == [.item("a")])
        #expect(live.count < 2)
    }

    @Test func pruneStaleRewritesSelection() {
        let state = state([.item("a"), .item("gone"), .agent("goneAgent")])
        state.pruneStale(items: [item("a")], interactions: [])
        #expect(state.selection == [.item("a")])
        // Nothing stale: no rewrite (keeps list highlights and SwiftUI selection stable).
        state.pruneStale(items: [item("a")], interactions: [])
        #expect(state.selection == [.item("a")])
    }

    @Test func survivingMultiSelectIsWhatMarkDoneActsOn() {
        // 全部标为已处理 resolves its ids against the live items (toggleDone's compactMap / InboxStore re-filter),
        // so the expired id is never sent and the two survivors are.
        let items = [item("a"), item("b"), item("done", done: true)]
        let state = state([.item("a"), .item("b"), .item("gone")])
        let live = state.liveSelection(items: items, interactions: [])
        let acted = live.compactMap { s -> InboxItem? in
            if case .item(let id) = s { return items.first { $0.id == id } } else { return nil }
        }
        #expect(Set(acted.map(\.id)) == ["a", "b"])
        #expect(acted.allSatisfy { !$0.isDone })
    }

    @Test func doneItemsStillExistAndStaySelected() {
        // Filed items stay in the list (已处理 section): existing is what counts, isDone does not remove them.
        let state = state([.item("done")])
        let live = state.liveSelection(items: [item("done", done: true)], interactions: [])
        #expect(live == [.item("done")])
    }
}
