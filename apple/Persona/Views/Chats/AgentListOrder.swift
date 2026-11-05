import Foundation

/// The Agents list's order, folds and sections (按状态 / 按机器), as plain functions of the sessions and the user's pins
/// and mutes, so they are unit-tested (`AgentListOrderTests`) apart from the view.
struct AgentListOrder {
    var isPinned: (SessionKey) -> Bool
    var isMuted: (Session) -> Bool
    /// Under a quick filter, or sorted by 最近更新 (`AgentSort`), rows are ordered by time only: an agent seen from a
    /// quick filter keeps its place.
    var byRank = true
    /// The open agent that was 完成未看 when it was opened (`AppModel.heldInList`): until it is left it keeps that place
    /// and stays out of the idle folds, so it does not go from under the pointer while it is read (#2213).
    var held: SessionKey? = nil

    /// The sections of 按状态, in rank order.
    static let stateKeys = ["needs", "unread", "working", "waiting", "muted", "idle"]

    /// Rows that stay out of the idle fold: busy, pinned, or waiting to be woken (`Session.isWaiting`: looping, with
    /// background work, or the hub waiting it out).
    func stays(_ s: Session) -> Bool {
        !s.isQuiet || s.key == held || isPinned(s.key) || s.isWaiting
    }

    /// One order for every grouping (inside a space, a machine, and the sections of 按状态):
    /// 需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看. Muted agents that are working, finished-unread or waiting
    /// rank as 已屏蔽; one that needs you stays first. The index into `stateKeys`.
    func rank(_ s: Session) -> Int {
        if s.state == .needsYou { return 0 }
        let unread = s.state == .unread || (s.key == held && s.isQuiet)
        let live = s.state == .working || unread || s.isWaiting
        if live && isMuted(s) { return 4 }
        if unread { return 1 }
        switch s.state {
        case .unread: return 1
        case .working: return 2
        default: return s.isWaiting ? 3 : 5
        }
    }

    /// By rank, then the most recently updated (working: most recently started) first, then by id so equal times
    /// never swap between renders.
    func ordered(_ list: [Session]) -> [Session] {
        list.map { session -> (session: Session, rank: Int, sortDate: Date) in
            (session: session, rank: byRank ? rank(session) : 0, sortDate: session.sortDate)
        }.sorted { a, b in
            if a.rank != b.rank { return a.rank < b.rank }
            return a.sortDate != b.sortDate ? a.sortDate > b.sortDate : a.session.sessionID < b.session.sessionID
        }
        .map(\.session)
    }

    /// The rows of a space or a working tree (`ordered`), each tab's agents together where its first one ranks
    /// (`TabPanes.entries`, #2320): those that stay out of the idle fold (a tab when any of its agents does) and the
    /// idle ones; with `all` off (a quick filter), every row stays.
    func tabEntries(_ list: [Session], all: Bool = true) -> (busy: [TabPanes.Entry], idle: [TabPanes.Entry]) {
        let entries = TabPanes.entries(ordered(list))
        guard all else { return (entries, []) }
        let stays = { (e: TabPanes.Entry) in e.sessions.contains(where: self.stays) }
        return (entries.filter(stays), entries.filter { !stays($0) })
    }

    /// A space that folds into 空闲的空间: every agent in it quiet, and not the held one's.
    func isQuiet(_ g: SpaceGroup) -> Bool { g.isQuiet && !g.sessions.contains { $0.key == held } }
    /// Newest first by `date`, then by `id` so equal times never swap between renders: spaces under a quick filter,
    /// spaces and machines under 最近更新. `date` is a `sortDate`, so a working agent's every step does not move them.
    static func latestFirst<T>(_ items: [T], date: (T) -> Date, id: (T) -> String) -> [T] {
        items.map { (item: $0, at: date($0), id: id($0)) }
            .sorted { $0.at != $1.at ? $0.at > $1.at : $0.id < $1.id }
            .map(\.item)
    }

    /// The fold ids of the sections shown now (全部折叠): one per machine with live agents, or one per rank in use.
    func sectionIDs(_ sessions: [Session], byMachine: Bool) -> [String] {
        let live = sessions.filter { $0.state.isLive }
        if byMachine { return Set(live.map(\.machineRef.path)).map { "machine/" + $0 } }
        let ranks = Set(live.map(rank))
        return Self.stateKeys.enumerated().filter { ranks.contains($0.offset) }.map { "state/" + $0.element }
    }
}
