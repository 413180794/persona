import Foundation

/// The rows of a Feishu chat or thread in the order they are drawn: Feishu's messages and mine still on their way
/// (sending, failed, waiting for their copy) in one list. Mine on its way stays where it was written: what Feishu has
/// from after that (my later messages that went out, others' answers) comes under it, not over it.
nonisolated enum LarkChatRows {
    struct Row: Identifiable, Equatable {
        var id: String
        /// What is drawn: Feishu's message, or mine on its way drawn like one.
        var message: LarkMessage
        /// Mine on its way (its spinner or failed mark beside the bubble); nil for Feishu's messages.
        var outgoing: LarkOutgoing?
    }

    /// `messages` in Feishu's order; `outgoing` in the order written. A message with no time stays above mine on
    /// their way, as before.
    ///
    /// Feishu's copy of one of mine takes over its row: `copies` (LarkOutbox.copies: outgoing id → copy's message id)
    /// draws the copy in place of the outgoing one under the outgoing row's id, and `adopted` (copy's message id →
    /// row id, kept by the page for the visit) keeps that id once the outgoing one is gone. The row then only changes
    /// what it shows: nothing is removed and inserted, the list does not move.
    static func rows(messages: [LarkMessage], outgoing: [LarkOutgoing], like mine: LarkMessage?,
                     copies: [UUID: String] = [:], adopted: [String: String] = [:]) -> [Row] {
        let waitingOut = outgoing.filter { copies[$0.id] == nil }
        let drawnOut = Set(waitingOut.map(\.rowID))
        var rowOf = adopted.filter { !drawnOut.contains($0.value) }
        for (o, copy) in copies { rowOf[copy] = LarkOutgoing.rowID(o) }
        var rows: [Row] = []
        rows.reserveCapacity(messages.count + waitingOut.count)
        var waiting = waitingOut[...]
        for m in messages {
            while let o = waiting.first, let at = m.createdAt, at > o.written {
                rows.append(Row(id: o.rowID, message: o.message(like: mine), outgoing: o))
                waiting = waiting.dropFirst()
            }
            rows.append(Row(id: rowOf[m.id] ?? m.id, message: m, outgoing: nil))
        }
        rows += waiting.map { Row(id: $0.rowID, message: $0.message(like: mine), outgoing: $0) }
        return rows
    }

    /// What the page keeps in `adopted` once rows were drawn with these `copies` (the outgoing ones may be gone by then).
    static func adopting(_ copies: [UUID: String], into adopted: [String: String]) -> [String: String] {
        var next = adopted
        for (o, copy) in copies { next[copy] = LarkOutgoing.rowID(o) }
        return next
    }
}
