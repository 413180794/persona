import SwiftUI
#if os(iOS)
import UIKit
#endif

// 设置 › 外观 › App 图标 (docs/ios.md「App 图标」): the home screen icon. The default is the app's own `AppIcon`; the others
// are alternate icon sets in the asset catalog (`ASSETCATALOG_COMPILER_ALTERNATE_APPICON_NAMES`, drawn by
// scripts/make_alt_icons.py). The system remembers the choice itself, so there is nothing to store: what is set now is
// `alternateIconName`. iPhone and iPad only: the watch and the Mac keep their one icon.

/// One icon the user can pick.
nonisolated enum AppIconChoice: String, CaseIterable, Identifiable, Sendable {
    case standard
    case didot
    case aura

    var id: String { rawValue }

    /// The icon set's name for `setAlternateIconName`; nil is the primary icon.
    var iconName: String? {
        switch self {
        case .standard: nil
        case .didot: "AppIcon-Didot"
        case .aura: "AppIcon-Aura"
        }
    }

    /// The picture the picker shows (an image set with any / dark, read from the icon set by the script).
    var preview: String {
        switch self {
        case .standard: "app-icon-default"
        case .didot: "app-icon-didot"
        case .aura: "app-icon-aura"
        }
    }

    var title: String {
        switch self {
        case .standard: "默认"
        case .didot: "p."
        case .aura: "气场"
        }
    }

    var subtitle: String {
        switch self {
        case .standard: "星芯"
        case .didot: "刊头"
        case .aura: "光晕"
        }
    }

    /// The choice an icon name stands for; an unknown name (an icon a later build dropped) reads as the default.
    init(iconName: String?) {
        self = Self.allCases.first { $0.iconName == iconName } ?? .standard
    }
}

/// What the picker needs from the system, so tests can stand in for it.
@MainActor protocol AppIconSetting: AnyObject {
    var supportsAlternateIcons: Bool { get }
    var alternateIconName: String? { get }
    func setAlternateIconName(_ name: String?) async throws
}

#if os(iOS)
extension UIApplication: AppIconSetting {}
#endif

/// 设置 › 外观 › App 图标's state: which icon is on, and the last failure to switch.
@MainActor @Observable final class AppIconModel {
    private let system: AppIconSetting?
    private(set) var current: AppIconChoice
    private(set) var switching: AppIconChoice?
    private(set) var failure: String?

    /// Off where the system cannot change the icon (the Mac, an app with alternate icons turned off).
    var available: Bool { system?.supportsAlternateIcons ?? false }

    init(system: AppIconSetting?) {
        self.system = system
        current = AppIconChoice(iconName: system?.alternateIconName)
    }

    func select(_ choice: AppIconChoice) async {
        guard let system, available, choice != current, switching == nil else { return }
        switching = choice
        failure = nil
        defer { switching = nil }
        do {
            try await system.setAlternateIconName(choice.iconName)
            current = choice
        } catch {
            failure = "没换成：\(error.localizedDescription)"
        }
    }
}

extension AppIconModel {
    /// The app's own: UIApplication on iPhone and iPad, nothing on the Mac.
    static func live() -> AppIconModel {
        #if os(iOS)
        AppIconModel(system: UIApplication.shared)
        #else
        AppIconModel(system: nil)
        #endif
    }
}
