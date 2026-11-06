import Foundation

/// The arithmetic of filing many inbox items at once (Clear All, Mark All Seen / Clear All, and Undo), kept apart from
/// `InboxStore` so it can be tested without a hub (docs/inbox.md「全部清理」).
nonisolated enum InboxBulk {
    /// How items are filed at once (`POST /api/v1/inbox/bulk`'s `action`, and the `done_by` shown meanwhile):
    /// `done` for the Done request (sections, Mac's multi-select, Undo), `dismiss` for Clear All — the view-wide
    /// clean-up, recorded as dismissed (looked at, nothing to do) rather than handled.
    enum Action: String, Sendable, CaseIterable {
        case done
        case dismiss

        /// The `done_by` the item shows before the hub answers.
        var doneBy: String { self == .done ? "user" : "dismissed" }
    }

    /// From this many items on, the clean-up asks first (fewer are done at once, with Undo after); the web's and
    /// Android's threshold too (docs/inbox.md「全部清理」).
    static let confirmFrom = 10

    static func needsConfirm(_ count: Int) -> Bool { count >= confirmFrom }

    /// What Undo sends after a clean-up: the hub's `undo` (only what that request changed), or — on a hub from before
    /// `/bulk` — the Done request bringing back what moved.
    enum Undo: Sendable, Equatable {
        case bulk(InboxBulkReply.Undo)
        case legacy([InboxItem])
    }

    /// The bulk request's answer as the arithmetic below reads it.
    static func result(_ reply: InboxBulkReply) -> InboxBulkResult {
        InboxBulkResult(changed: reply.changed, unchanged: reply.unchanged, skipped: reply.skipped)
    }

    /// Sends a clean-up: the bulk request, or on a hub from before it (404 / 405) the Done one. That request cannot
    /// say dismissed, so an old hub records Clear All as `done_by=user` — acceptable there, better than refusing it.
    /// The requests come in so it can be tested without a hub.
    static func perform(_ action: Action, _ targets: [InboxItem],
                        bulk: (Action, [InboxItem]) async throws -> InboxBulkReply,
                        legacy: ([InboxItem]) async throws -> InboxBulkResult) async throws -> (result: InboxBulkResult, undo: Undo?) {
        do {
            let reply = try await bulk(action, targets)
            let r = result(reply)
            return (r, reply.undo.map(Undo.bulk) ?? (r.changed.isEmpty ? nil : .legacy(r.changed)))
        } catch let e as APIError where e.status == 404 || e.status == 405 {
            let r = try await legacy(targets)
            return (r, r.changed.isEmpty ? nil : .legacy(r.changed))
        }
    }

    /// The list with what a failed request had moved put back as it was.
    static func rollback(_ items: [InboxItem], before: [String: InboxItem]) -> [InboxItem] {
        items.map { before[$0.id] ?? $0 }
    }

    /// What a bulk request moves: the items not in that state yet.
    static func targets(_ list: [InboxItem], done: Bool) -> [InboxItem] {
        var seen = Set<String>()
        return list.filter { $0.isDone != done && seen.insert($0.id).inserted }
    }

    /// The list with `ids` filed (or brought back) at `now`, as it shows before the hub answers.
    static func marking(_ items: [InboxItem], ids: Set<String>, done: Bool, now: Date, by action: Action = .done) -> [InboxItem] {
        items.map { it in
            guard ids.contains(it.id) else { return it }
            var it = it
            it.doneAt = done ? now : nil
            it.doneBy = done ? action.doneBy : nil
            return it
        }
    }

    /// The list after the hub answered: what it moved as it has them now; what it left (new messages since the list,
    /// gone from the hub) as it was before; what was already so as shown.
    static func settle(_ items: [InboxItem], before: [String: InboxItem], result: InboxBulkResult) -> [InboxItem] {
        let fresh = Dictionary(result.changed.map { ($0.id, $0) }, uniquingKeysWith: { _, b in b })
        let left = Set(result.skipped.map(\.id))
        return items.map { it in
            if let f = fresh[it.id] { return f }
            if left.contains(it.id), let b = before[it.id] { return b }
            return it
        }
    }

    /// The items left open because something new came in after the list was drawn.
    static func changedSince(_ result: InboxBulkResult) -> Int { result.skipped.filter { $0.reason == "changed" }.count }

    /// The toast after the hub answered.
    static func message(done: Bool, result: InboxBulkResult, action: Action = .done) -> String {
        let moved = result.changed.count
        let fresh = changedSince(result)
        let head: String
        switch (done, action) {
        case (true, .done): head = moved > 0 ? String(localized: "已将 \(moved) 件标为已处理") : String(localized: "没有要标的")
        case (true, .dismiss): head = moved > 0 ? String(localized: "已清理 \(moved) 件") : String(localized: "没有要清理的")
        case (false, _): head = moved > 0 ? String(localized: "已移回 \(moved) 件") : String(localized: "没有要移回的")
        }
        return fresh > 0 ? head + String(localized: "，\(fresh) 件有新消息，留在收件箱") : head
    }

    /// The question before a large Clear All (the web's and Android's words).
    static var confirmTitle: String { String(localized: "全部清理？") }

    /// The dialog's button: Clear and how many.
    static func confirmButton(count: Int) -> String { String(localized: "清理 \(count) 件") }

    static func confirmMessage(count: Int, filters: [String], query: String) -> String {
        String(localized: "将清理 \(count) 件（\(scope(filters: filters, query: query))）。之后进来的新消息不受影响，完成后可以撤销。")
    }

    /// What Clear All covers, for the menu: the filters' names and the search, or All.
    static func scope(filters: [String], query: String) -> String {
        var parts = filters.filter { !$0.isEmpty }
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        if !q.isEmpty { parts.append(String(localized: "搜索「\(q)」")) }
        return parts.isEmpty ? String(localized: "全部") : parts.joined(separator: " · ")
    }

    /// The menu's line under Clear All: what it covers and how many.
    static func scopeLine(filters: [String], query: String, count: Int) -> String {
        count == 0 ? String(localized: "没有未处理的") : String(localized: "\(scope(filters: filters, query: query)) · \(count) 件")
    }
}
