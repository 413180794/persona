import SwiftUI

/// Settings › Lark › Show Messages on Home (#392): the iPhone's extra Messages tab, the Mac's Messages sidebar section (#3162).
@MainActor @Observable final class LarkHomePreference {
    static let shared = LarkHomePreference()
    static let enabledKey = "integrations.lark.enabled"
    var enabled = UserDefaults.standard.bool(forKey: enabledKey) {
        didSet { UserDefaults.standard.set(enabled, forKey: Self.enabledKey) }
    }

    /// The Messages page is there: switched on, and the hub has Feishu chats (`LarkFeatures`, the Lark extension).
    /// The switch keeps its value while the hub has none, so the page comes back with the plugin.
    var shows: Bool { enabled && AppModel.shared.larkFeatures.chats }
}
