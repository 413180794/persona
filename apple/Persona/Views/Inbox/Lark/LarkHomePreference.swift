import SwiftUI

#if os(iOS)
@MainActor @Observable final class LarkHomePreference {
    static let shared = LarkHomePreference()
    static let enabledKey = "integrations.lark-connector.enabled"
    var enabled = UserDefaults.standard.bool(forKey: enabledKey) {
        didSet { UserDefaults.standard.set(enabled, forKey: Self.enabledKey) }
    }

    /// The 消息 page is there: switched on, and the hub has Feishu chats (`LarkFeatures`, the lark-connector plugin).
    /// The switch keeps its value while the hub has none, so the page comes back with the plugin.
    var shows: Bool { enabled && AppModel.shared.larkFeatures.chats }
}
#endif
