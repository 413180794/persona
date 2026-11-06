import AppKit
import SwiftUI

/// Which links a click hands to macOS. `NSWorkspace.open` runs whatever a URL names without asking (a `file://`
/// application, an `smb://` share, another app's URL scheme), and links come from agents' replies and other people's
/// messages. The web and mail go out; anything else is copied, with a toast saying so (the web version does the same).
/// Not `persona://` either: a `persona://pair` link in a message would move the app to whoever's hub it names.
nonisolated enum LinkPolicy {
    static let openable: Set<String> = ["http", "https", "mailto"]

    static func opensOnMac(_ url: URL) -> Bool {
        guard let scheme = url.scheme?.lowercased() else { return false }
        return openable.contains(scheme)
    }

    /// What the app opens through `UIApplication.open` (the shim): the same, plus the System Settings panes its own
    /// 「打开系统设置」 buttons name (constants, never text from a message).
    static func opensFromApp(_ url: URL) -> Bool {
        opensOnMac(url) || [UIApplication.openSettingsURLString, UIApplication.openNotificationSettingsURLString].contains(url.absoluteString)
    }

    /// What an `OpenURLAction` returns for a link the app does not show itself.
    @MainActor static func system(_ url: URL) -> OpenURLAction.Result {
        if opensOnMac(url) { return .systemAction }
        copy(url)
        return .handled
    }

    /// `NSWorkspace.open`, behind the same rule.
    @MainActor @discardableResult static func open(_ url: URL) -> Bool {
        if opensFromApp(url) { return NSWorkspace.shared.open(url) }
        copy(url)
        return false
    }

    @MainActor private static func copy(_ url: URL) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(url.absoluteString, forType: .string)
        AppModel.shared.toast = Toast(String(localized: "This link doesn't open in the app, so it was copied: \(String(url.absoluteString.prefix(60)))"))
    }
}
