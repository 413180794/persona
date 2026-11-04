import Foundation

/// What the hub offers of Feishu through lark-connector (#1530). That path is a plugin's now: the hub has
/// `/api/v1/lark/…` only while the plugin is installed, and the plugin adds `lark` (the 消息 page, the chat views, the
/// native readers of Feishu documents and company-feed posts, every `/api/v1/lark/…` route) and `lark_moments` (the
/// company feed) to the hello's features. Without them the app shows nothing that needs those routes: Feishu links
/// open as ordinary web pages. Inbox items with `lark` / `doc` / `moment` blocks come from the plugin only, so they are
/// drawn as they come.
///
/// Unknown is today's behaviour, everything shown: no hello yet (or the demo hub), and a hub from before the plugin,
/// which served Feishu itself and said so with `lark_attachments` but not `lark`.
nonisolated struct LarkFeatures: Hashable, Sendable {
    /// `lark`: the chats, the 消息 page, the Feishu account in 设置, the native document and post readers.
    var chats: Bool
    /// `lark_moments`: company-feed posts open in the native reader.
    var moments: Bool

    static let all = LarkFeatures(chats: true, moments: true)
    static let none = LarkFeatures(chats: false, moments: false)

    init(chats: Bool, moments: Bool) {
        self.chats = chats
        self.moments = chats && moments
    }

    /// From the hub's hello features.
    init(hub features: Set<String>) {
        if features.isEmpty || (!features.contains("lark") && features.contains("lark_attachments")) {
            self = .all // unknown, or a hub that serves Feishu itself (from before the plugin)
        } else {
            self.init(chats: features.contains("lark"), moments: features.contains("lark_moments"))
        }
    }
}

extension AppModel {
    /// What the hub-level group's hub offers of Feishu through lark-connector (`LarkFeatures`).
    var larkFeatures: LarkFeatures { LarkFeatures(hub: hubFeatures) }
}
