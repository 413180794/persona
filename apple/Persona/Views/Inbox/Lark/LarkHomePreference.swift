import SwiftUI

#if os(iOS)
@MainActor @Observable final class LarkHomePreference {
    static let shared = LarkHomePreference()
    static let enabledKey = "integrations.lark-connector.enabled"
    var enabled = UserDefaults.standard.bool(forKey: enabledKey) {
        didSet { UserDefaults.standard.set(enabled, forKey: Self.enabledKey) }
    }
}
#endif
