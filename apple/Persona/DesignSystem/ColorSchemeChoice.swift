import SwiftUI
#if os(iOS)
import UIKit
#endif

/// 设置 › 外观 › 深浅色 (docs/ios.md「设计语言」): the app light or dark whatever the system is, or following it (the
/// default: what the app always did). Set on the app's windows (`overrideUserInterfaceStyle`) rather than with
/// `preferredColorScheme`: sheets, alerts, the keyboard and web pages follow a window's style, and going back to nil
/// through `preferredColorScheme` does not always bring an open sheet back to the system's. The Live Activity,
/// widgets and the watch are drawn by the system and keep its look.
nonisolated enum ColorSchemeChoice: String, CaseIterable, Identifiable, Sendable {
    case system, light, dark

    var id: String { rawValue }

    var title: String {
        switch self {
        case .system: "跟随系统"
        case .light: "浅色"
        case .dark: "深色"
        }
    }

    var symbol: String {
        switch self {
        case .system: Theme.Icon.colorScheme
        case .light: Theme.Icon.lightMode
        case .dark: Theme.Icon.darkMode
        }
    }

    static let key = "ui.scheme"

    /// `ui.scheme` (also a launch argument: `-ui.scheme dark`).
    static var current: ColorSchemeChoice { ColorSchemeChoice(rawValue: UserDefaults.standard.string(forKey: key) ?? "") ?? .system }

    #if os(iOS)
    var interfaceStyle: UIUserInterfaceStyle {
        switch self {
        case .system: .unspecified
        case .light: .light
        case .dark: .dark
        }
    }

    /// On every window of every scene (an iPad can have several); a window already on it is left alone.
    @MainActor func apply() {
        for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
            for w in scene.windows where w.overrideUserInterfaceStyle != interfaceStyle {
                w.overrideUserInterfaceStyle = interfaceStyle
            }
        }
    }
    #endif
}
