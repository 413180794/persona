import Foundation

/// The arithmetic of filing many inbox items at once (全部标为已处理, 全部看过 / 全部清除, and 撤销), kept apart from
/// `InboxStore` so it can be tested without a hub (docs/inbox.md「全部标为已处理」).
nonisolated enum InboxBulk {
    /// What a bulk request moves: the items not in that state yet.
    static func targets(_ list: [InboxItem], done: Bool) -> [InboxItem] {
        var seen = Set<String>()
        return list.filter { $0.isDone != done && seen.insert($0.id).inserted }
    }

    /// The list with `ids` filed (or brought back) at `now`, as it shows before the hub answers.
    static func marking(_ items: [InboxItem], ids: Set<String>, done: Bool, now: Date) -> [InboxItem] {
        items.map { it in
            guard ids.contains(it.id) else { return it }
            var it = it
            it.doneAt = done ? now : nil
            it.doneBy = done ? "user" : nil
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
    static func message(done: Bool, result: InboxBulkResult) -> String {
        let moved = result.changed.count
        let fresh = changedSince(result)
        let head = done ? (moved > 0 ? "已将 \(moved) 件标为已处理" : "没有要标的") : (moved > 0 ? "已移回 \(moved) 件" : "没有要移回的")
        return fresh > 0 ? head + "，\(fresh) 件有新消息，留在收件箱" : head
    }

    /// What 全部标为已处理 covers, for the menu: the filters' names and the search, or 全部.
    static func scope(filters: [String], query: String) -> String {
        var parts = filters.filter { !$0.isEmpty }
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        if !q.isEmpty { parts.append("搜索「\(q)」") }
        return parts.isEmpty ? "全部" : parts.joined(separator: " · ")
    }

    /// The menu's line under 全部标为已处理: what it covers and how many.
    static func scopeLine(filters: [String], query: String, count: Int) -> String {
        count == 0 ? "没有未处理的" : "\(scope(filters: filters, query: query)) · \(count) 件"
    }
}
