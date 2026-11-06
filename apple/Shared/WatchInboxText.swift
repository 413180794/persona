import Foundation

/// What the watch says about inbox items (pure: the words only, unit-tested on the phone's tests).
nonisolated enum WatchInboxText {
    /// A connector's item's source line: its connector's name and the label it gave the item (Example Chat · @Me), each
    /// only when there is one; the connector's id when it has neither.
    static func connectorSource(_ item: InboxItem) -> String {
        let parts = [item.accountName, item.connector?.label ?? ""].map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        return parts.isEmpty ? item.source : parts.joined(separator: " · ")
    }
}
