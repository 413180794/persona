import Foundation
import Observation

/// A conversation belongs to its paired hub group, not to every hub with the same opaque conversation id.
nonisolated struct AssistantViewScope: Hashable, Sendable {
    var group: String
    var conversation: String
}

/// Current live-card snapshots (#2024). The feed keeps creation positions; updates replace only the referenced view.
/// The server persists the source of truth. This observable cache never writes frequent updates to UserDefaults.
@MainActor @Observable
final class AssistantViewsStore {
    static let shared = AssistantViewsStore()

    private(set) var snapshots: [AssistantViewScope: [String: ExtensionAssistantView]] = [:]
    /// Reply records survive a card update. Keys are opaque: neither slashes nor prefixes are interpreted here.
    private(set) var actions: [AssistantViewScope: [String: ExtensionViewAction]] = [:]

    func action(_ key: String, conversation: String, group: String) -> ExtensionViewAction? {
        actions[AssistantViewScope(group: group, conversation: conversation)]?[key]
    }

    func view(_ id: String, conversation: String, group: String) -> ExtensionAssistantView? {
        snapshots[AssistantViewScope(group: group, conversation: conversation)]?[id]
    }

    /// Ignore invalid ownership and stale or repeated versions, including old creation events after newer updates.
    @discardableResult
    func receive(_ view: ExtensionAssistantView, conversation: String, group: String) -> Bool {
        guard !view.id.isEmpty, !conversation.isEmpty, view.conversation == conversation, view.version > 0 else { return false }
        let scope = AssistantViewScope(group: group, conversation: conversation)
        if let current = snapshots[scope]?[view.id], current.version >= view.version { return false }
        snapshots[scope, default: [:]][view.id] = view
        return true
    }

    /// Feed snapshots are full even when its items use `since`. Cards cannot be deleted through this protocol, so
    /// missing entries in a snapshot that raced a new event must not remove that event's newer card.
    func receive(_ views: [String: ExtensionAssistantView], conversation: String, group: String) {
        for (id, view) in views where id == view.id { receive(view, conversation: conversation, group: group) }
    }

    func receive(_ item: FeedItem, group: String) {
        guard let view = item.assistantView, item.ref == view.id else { return }
        receive(view, conversation: item.conversation, group: group)
    }

    /// Pending/sent/failed are ordered by the server revision, not by arrival time or by a status ranking.
    /// A legacy record without a revision may establish a lock, but cannot overwrite an already cached record.
    @discardableResult
    func receive(_ action: ExtensionViewAction, conversation: String, group: String) -> Bool {
        guard !conversation.isEmpty, !action.key.isEmpty, !action.action.isEmpty,
              !action.clientMsgId.isEmpty, !action.status.isEmpty,
              action.revision.map({ $0 > 0 }) ?? true else { return false }
        let scope = AssistantViewScope(group: group, conversation: conversation)
        if let current = actions[scope]?[action.key] {
            guard current.clientMsgId == action.clientMsgId, current.action == action.action,
                  (current.page ?? "") == (action.page ?? ""), (current.values ?? [:]) == (action.values ?? [:]),
                  (current.revision ?? 0) < (action.revision ?? 0) else { return false }
        }
        actions[scope, default: [:]][action.key] = action
        return true
    }

    func receiveActions(_ records: [String: ExtensionViewAction], conversation: String, group: String) {
        for (key, record) in records where key == record.key { receive(record, conversation: conversation, group: group) }
    }

    func forget() { snapshots = [:]; actions = [:] }
}
