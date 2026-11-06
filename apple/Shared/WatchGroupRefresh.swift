import Foundation

/// One hub group's last successful watch fetch (docs/multi-hub.md「手表」): the live sessions, pending cards and
/// machines its own hub returned. A failed refresh keeps the group's last value, so a group's cards are never
/// replaced with nothing just because that group did not answer this time (#4369).
nonisolated struct WatchGroupData: Equatable, Sendable {
    var sessions: [Session]
    var interactions: [Interaction]
    var machines: [Machine]

    static let empty = WatchGroupData(sessions: [], interactions: [], machines: [])
}

/// Pure decisions behind the watch's per-group refresh: keeping a failed group's last cards, and never buzzing twice
/// for the same card. The watch and its tests use these; Foundation only, no UI.
nonisolated enum WatchGroupRefresh {
    /// Keep each failed group's last successful data and replace every group that answered. Groups not attempted this
    /// round (no credential yet) keep their old data as well. `staleFailed` lists the failed groups whose earlier data
    /// stays on screen, sorted for a stable banner order; a group that failed before its first success is absent —
    /// there is no earlier data of it to show.
    static func mergeGroups(old: [String: WatchGroupData], new: [String: WatchGroupData], failures: Set<String>)
        -> (data: [String: WatchGroupData], staleFailed: [String]) {
        var stored = old
        for (group, data) in new { stored[group] = data }
        return (stored, failures.filter { stored[$0] != nil }.sorted())
    }

    /// The uids the wrist has never been told about: only those get a haptic. A card that vanished with a failed
    /// group and returns when it recovers is already in `seen`, so it stays quiet.
    static func newHapticUids(seen: Set<String>, current: [Interaction]) -> [String] {
        var seen = seen
        return current.map(\.uid).filter { seen.insert($0).inserted }
    }

    /// The uids already buzzed for, oldest first and de-duplicated. New uids append; when more than `limit` are kept
    /// the oldest drop, so the persisted record cannot grow without end. A card answered on the wrist is removed by
    /// the caller before the next save.
    static func rememberSeen(_ stored: [String], adding uids: [String], limit: Int = 200) -> [String] {
        var known = Set(stored)
        var out = stored
        for uid in uids where known.insert(uid).inserted { out.append(uid) }
        if out.count > limit { out.removeFirst(out.count - limit) }
        return out
    }
}
