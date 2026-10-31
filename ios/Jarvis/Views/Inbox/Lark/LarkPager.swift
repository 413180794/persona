import Foundation

/// A 飞书 chat's loaded messages and where paging and read reporting stand (docs/feishu.md, CLAUDE.md: the read
/// position and the two cursors). A value with no views and no network: `LarkChatView` asks the hub for a page and
/// hands the answer here; what to report as read comes back out. `LarkPagerTests` checks the merging and reporting.
struct LarkPager: Equatable {
    var messages: [LarkMessage] = []
    var hasOlder = false
    var hasNewer = false
    var olderCursor: String?
    var newerCursor: String?
    /// Everything at or before this position has been reported read.
    var readCursor: String?
    /// Positions that reached the screen.
    var seen: Set<String> = []

    /// Where the next older / newer page starts.
    var olderRequest: String? { olderCursor ?? messages.first?.position }
    var newerRequest: String? { newerCursor ?? messages.last?.position }

    /// A whole page in place of what was loaded (opening, jumping to a message, back to the newest).
    mutating func replace(with r: APIClient.LarkMessages) {
        messages = r.messages
        hasOlder = r.hasOlder
        hasNewer = r.hasNewer ?? false
        olderCursor = r.olderCursor ?? r.messages.first?.position
        newerCursor = r.newerCursor ?? r.messages.last?.position
    }

    /// An older page goes on top; messages already loaded are not repeated.
    mutating func prepend(_ r: APIClient.LarkMessages) {
        let have = Set(messages.map(\.id))
        messages = r.messages.filter { !have.contains($0.id) } + messages
        hasOlder = r.hasOlder
        olderCursor = r.olderCursor ?? r.messages.first?.position
    }

    /// A newer page goes below; `cursor` is what was asked from, kept when the page brings none.
    mutating func append(_ r: APIClient.LarkMessages, from cursor: String) {
        let have = Set(messages.map(\.id))
        messages += r.messages.filter { !have.contains($0.id) }
        hasNewer = r.hasNewer ?? false
        newerCursor = r.newerCursor ?? r.messages.last?.position ?? cursor
    }

    /// The latest page from a poll: loaded messages that changed (a card's status) update in place; the ones after
    /// the last loaded are returned for the view to add (animated), not added here.
    mutating func refresh(with r: APIClient.LarkMessages) -> [LarkMessage] {
        let byID = Dictionary(r.messages.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        let updated = messages.map { byID[$0.id] ?? $0 }
        if updated != messages { messages = updated }
        return r.messages.filter { m in
            !messages.contains { $0.id == m.id } && (messages.last.map { LarkPosition.less($0.position, m.position) } ?? true)
        }
    }

    mutating func add(_ fresh: [LarkMessage]) {
        messages += fresh
        newerCursor = messages.last?.position
    }

    /// A message reached the screen; false if it had already.
    mutating func see(_ m: LarkMessage) -> Bool { seen.insert(m.position).inserted }

    /// What to tell 飞书 is read now: up to the newest message seen past the read cursor, with the ids of others'
    /// unread messages up to it. Moves the cursor; nil when nothing new was seen.
    mutating func readReport() -> APIClient.LarkReadReport? {
        let cursor = readCursor ?? "0"
        let newer = messages.filter { seen.contains($0.position) && LarkPosition.less(cursor, $0.position) }
        guard let top = newer.max(by: { LarkPosition.less($0.position, $1.position) }) else { return nil }
        let ids = messages.filter { m in
            !m.isMine && m.isRead != true && !m.isSystem && LarkPosition.less(cursor, m.position) && !LarkPosition.less(top.position, m.position)
        }.map(\.id)
        readCursor = top.position
        return APIClient.LarkReadReport(position: top.position, badgeCount: top.badgeCount, messageIDs: ids.isEmpty ? nil : ids)
    }
}
