import Foundation
import Testing
@testable import Persona

/// #4369: with several hub groups on the watch, one group's failed refresh keeps that group's last cards and agents,
/// and the same card does not buzz again when the group recovers.
struct WatchGroupRefreshTests {
    private func interaction(_ id: String, group: String) -> Interaction {
        var ix = Interaction(id: id, machineID: "m", sessionID: "s", kind: .permission)
        ix.group = group
        return ix
    }

    private func machine(_ id: String, group: String) -> Machine {
        var m = Machine(id: id, name: id, kind: "ssh", online: true)
        m.group = group
        return m
    }

    private func data(group: String, interaction id: String) -> WatchGroupData {
        WatchGroupData(sessions: [], interactions: [interaction(id, group: group)],
                       machines: [machine("m-\(group)", group: group)])
    }

    @Test func failingGroupKeepsItsLastSuccessfulData() {
        let old = ["g1": data(group: "g1", interaction: "a"), "g2": data(group: "g2", interaction: "b")]
        let newer = ["g1": data(group: "g1", interaction: "a2")]
        let out = WatchGroupRefresh.mergeGroups(old: old, new: newer, failures: ["g2"])
        #expect(out.staleFailed == ["g2"])
        // The failed group is not replaced with an empty answer; the group that answered is.
        #expect(out.data["g2"] == old["g2"])
        #expect(out.data["g1"] == newer["g1"])
    }

    @Test func recoveredGroupReplacesOldDataAndLeavesTheBannerList() {
        let old = ["g2": data(group: "g2", interaction: "b")]
        let out = WatchGroupRefresh.mergeGroups(old: old, new: ["g2": data(group: "g2", interaction: "b2")], failures: [])
        #expect(out.staleFailed.isEmpty)
        #expect(out.data["g2"]?.interactions.first?.id == "b2")
    }

    @Test func everyGroupFailingBeforeItsFirstSuccessHasNothingStaleToShow() {
        let out = WatchGroupRefresh.mergeGroups(old: [:], new: [:], failures: ["", "g2"])
        #expect(out.data.isEmpty)
        #expect(out.staleFailed.isEmpty) // the watch shows its whole-page offline state instead
    }

    @Test func groupNotAttemptedThisRoundKeepsItsDataWithoutABanner() {
        let old = ["g1": data(group: "g1", interaction: "a"), "g2": data(group: "g2", interaction: "b")]
        // g2 has no credential this round, so it is neither fetched nor in failures.
        let out = WatchGroupRefresh.mergeGroups(old: old, new: ["g1": data(group: "g1", interaction: "a2")], failures: [])
        #expect(out.data["g2"] == old["g2"])
        #expect(out.staleFailed.isEmpty)
    }

    @Test func cardBuzzesOnceAcrossFailureAndRecovery() {
        let card = interaction("card", group: "g2")
        var remembered: [String] = []

        // First success: the new card buzzes and is remembered.
        let first = WatchGroupRefresh.newHapticUids(seen: Set(remembered), current: [card])
        #expect(first == ["g2|card"])
        remembered = WatchGroupRefresh.rememberSeen(remembered, adding: first)

        // g2 fails: its old card is kept and marked stale, applying it again buzzes for nothing.
        let stored = ["g2": WatchGroupData(sessions: [], interactions: [card], machines: [])]
        let failed = WatchGroupRefresh.mergeGroups(old: stored, new: [:], failures: ["g2"])
        #expect(failed.staleFailed == ["g2"])
        // The coalescing stays outside #expect: its property-access macro treats the whole expression as the
        // optional root and generates a chain on the already non-optional [String].
        let failedCards = failed.data["g2"]?.interactions ?? []
        #expect(WatchGroupRefresh.newHapticUids(seen: Set(remembered), current: failedCards).isEmpty)

        // g2 recovers with the same card: replaced with fresh data, still no buzz.
        let recovered = WatchGroupRefresh.mergeGroups(old: failed.data, new: stored, failures: [])
        #expect(recovered.staleFailed.isEmpty)
        let recoveredCards = recovered.data["g2"]?.interactions ?? []
        #expect(WatchGroupRefresh.newHapticUids(seen: Set(remembered), current: recoveredCards).isEmpty)

        // A genuinely new card buzzes; the one already seen does not.
        let next = interaction("card2", group: "g2")
        #expect(WatchGroupRefresh.newHapticUids(seen: Set(remembered), current: [card, next]) == ["g2|card2"])
    }

    @Test func rememberedUidsDedupAndStayBounded() {
        #expect(WatchGroupRefresh.rememberSeen(["a", "b"], adding: ["b", "c"]) == ["a", "b", "c"])
        // Past the limit the oldest first-seen uids drop, so the persisted record can not grow without end.
        #expect(WatchGroupRefresh.rememberSeen([], adding: (0..<5).map { "u\($0)" }, limit: 3) == ["u2", "u3", "u4"])
    }
}
