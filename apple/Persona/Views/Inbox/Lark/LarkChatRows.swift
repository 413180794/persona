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
    static func rows(messages: [LarkMessage], outgoing: [LarkOutgoing], like mine: LarkMessage?) -> [Row] {
        var rows: [Row] = []
        rows.reserveCapacity(messages.count + outgoing.count)
        var waiting = outgoing[...]
        for m in messages {
            while let o = waiting.first, let at = m.createdAt, at > o.written {
                rows.append(row(o, like: mine))
                waiting = waiting.dropFirst()
            }
            rows.append(Row(id: m.id, message: m, outgoing: nil))
        }
        rows += waiting.map { row($0, like: mine) }
        return rows
    }

    private static func row(_ o: LarkOutgoing, like mine: LarkMessage?) -> Row {
        let m = o.message(like: mine)
        return Row(id: m.id, message: m, outgoing: o)
    }
}
