import Foundation

/// The Agents list's order, folds and sections (按状态 / 按机器), as plain functions of the sessions and the user's pins
/// and mutes, so they are unit-tested (`AgentListOrderTests`) apart from the view.
struct AgentListOrder {
    var isPinned: (SessionKey) -> Bool
    var isMuted: (Session) -> Bool
    /// Under a quick filter rows are ordered by time only: an agent seen from there keeps its place.
    var byRank = true

    /// The sections of 按状态, in rank order.
    static let stateKeys = ["needs", "unread", "working", "waiting", "muted", "idle"]

    /// Rows that stay out of the idle fold: busy, pinned, looping, or with background work.
    func stays(_ s: Session) -> Bool {
        !s.isQuiet || isPinned(s.key) || s.loop != nil || !(s.background ?? []).isEmpty
            || (s.subagents ?? []).contains(where: \.running)
    }

    /// One order for every grouping (inside a space, a machine, and the sections of 按状态):
    /// 需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看. Muted agents that are working, finished-unread or waiting
    /// rank as 已屏蔽; one that needs you stays first. The index into `stateKeys`.
    func rank(_ s: Session) -> Int {
        if s.state == .needsYou { return 0 }
        let live = s.state == .working || s.state == .unread || s.isWaiting
        if live && isMuted(s) { return 4 }
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

    /// The fold ids of the sections shown now (全部折叠): one per machine with live agents, or one per rank in use.
    func sectionIDs(_ sessions: [Session], byMachine: Bool) -> [String] {
        let live = sessions.filter { $0.state.isLive }
        if byMachine { return Set(live.map(\.machineID)).map { "machine/" + $0 } }
        let ranks = Set(live.map(rank))
        return Self.stateKeys.enumerated().filter { ranks.contains($0.offset) }.map { "state/" + $0.element }
    }
}
