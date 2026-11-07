import Foundation

/// A Lark chat's loaded messages and where paging and read reporting stand (docs/extensions/feishu.md, docs/lessons/lark-mail.md: the read
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
    /// The newest messages loaded before the window moved away from them (opened at a message, a jump, a Pin):
    /// Jump to Latest goes back to them at once, with no network (#1034).
    private(set) var kept: [LarkMessage]?
    /// The window is the newest this device has, and the hub has more after it than its latest page joins up with:
    /// the newer pages load from the bottom, in order.
    private(set) var catchingUp = false

    /// How the hub's latest page meets what is loaded.
    enum Latest: Equatable {
        /// It joins up: these are newer than the last loaded (the view adds them).
        case fresh([LarkMessage])
        /// Messages are missing in between.
        case gap
    }

    /// Where the next older / newer page starts.
    var olderRequest: String? { olderCursor ?? messages.first?.position }
    var newerRequest: String? { newerCursor ?? messages.last?.position }

    /// A whole page in place of what was loaded (opening, jumping to a message, back to the newest).
    mutating func replace(with r: APIClient.LarkMessages) {
        if r.hasNewer == true {
            // Moving away from the newest: keep them for Jump to Latest.
            if !hasNewer, !messages.isEmpty { kept = Array(messages.suffix(Self.keptMost)) }
        } else {
            kept = nil
        }
        catchingUp = false
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
        if !hasNewer { catchingUp = false; kept = nil }
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

    /// Jump to Latest with no network: back to the newest messages this device has, the ones kept when the window moved away or
    /// `cached` (the chat's cache), whichever reaches further. False when the window is the newest already, the newer
    /// pages are catching up, or there are none.
    mutating func toTail(cached: [LarkMessage]) -> Bool {
        guard hasNewer, !catchingUp else { return false }
        let mine = kept ?? []
        let tail: [LarkMessage]
        if let newest = cached.last, mine.last.map({ LarkPosition.less($0.position, newest.position) }) ?? true {
            tail = cached
        } else {
            tail = mine
        }
        guard let first = tail.first, let last = tail.last else { return false }
        messages = tail
        hasOlder = true // a final empty page says otherwise (LarkHistoryPaging)
        olderCursor = first.position
        hasNewer = false
        newerCursor = last.position
        kept = nil
        return true
    }

    /// The hub's latest page against the newest loaded. Joined up (it starts at or before the message after the last
    /// loaded one, or it is the whole chat): cards that changed update in place and the newer ones come back. Else
    /// messages are missing in between; nothing changes (the view replaces the window or calls `markGap`).
    mutating func latest(_ r: APIClient.LarkMessages) -> Latest {
        if let last = messages.last, let first = r.messages.first, r.hasOlder,
           LarkPosition.less(LarkPosition.next(last.position), first.position) {
            return .gap
        }
        return .fresh(refresh(with: r))
    }

    /// Missing messages after the last loaded: the newer pages load from the bottom (`append`), in order.
    mutating func markGap() {
        guard let last = messages.last else { return }
        hasNewer = true
        newerCursor = last.position
        catchingUp = true
    }

    static let keptMost = 80

    /// Two refreshes can be in flight at once (a push and a send's own): both return the same new message, it goes in once.
    mutating func add(_ fresh: [LarkMessage]) {
        let have = Set(messages.map(\.id))
        messages += fresh.filter { !have.contains($0.id) }
        newerCursor = messages.last?.position
    }

    /// Opened at a message (an inbox item's first new one): reporting starts at the read position when it is before
    /// the message, else just before it, so the message and what follows are reported once on screen. Never moves
    /// the cursor back within a visit.
    mutating func openRead(at focus: String, readPosition: String?) {
        let start = readPosition.flatMap { LarkPosition.less($0, focus) ? $0 : nil } ?? LarkPosition.prev(focus)
        if let readCursor, !LarkPosition.less(readCursor, start) { return }
        readCursor = start
    }

    /// A message reached the screen; false if it had already.
    mutating func see(_ m: LarkMessage) -> Bool { seen.insert(m.position).inserted }

    /// Counted by 「N 条新消息」 once on screen: someone else's message past where the chat was opened (`anchor`).
    static func countsAsNew(_ m: LarkMessage, past anchor: String) -> Bool {
        !m.isMine && !m.isSystem && LarkPosition.less(anchor, m.position)
    }

    /// What to tell Lark is read now: up to the newest message seen past the read cursor, with the ids of others'
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

/// What a chat's rows keep track of as they come on and off screen: the messages seen, read receipts being asked
/// for, the pending read report. A class kept in `@State`, so writing it does not re-run the chat's body: a row
/// scrolling past wrote view state, and every write laid the page out again (half the hitches of a glide, #674).
/// What shows stays view state: `LarkPager.seen` for the jump buttons, the receipts once answered.
final class LarkChatBook {
    /// Every message seen on screen this visit; `LarkPager.seen` gets the ones the jump buttons count at once and
    /// the rest before a read report.
    var seen: Set<String> = []
    var receipts = LarkReadReceipts()
    var reportTask: Task<Void, Never>?
    var receiptTask: Task<Void, Never>?
    /// How many times the user's finger took the list: Jump to Latest tells a reader who went back up from one who did not.
    var drags = 0
    /// The newer-messages spinner at the bottom is on screen.
    var newerVisible = false
}
