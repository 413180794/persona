import SwiftUI
#if os(iOS)
import UIKit
#endif

// 设置 › 外观 › App 图标 (docs/ios.md「App 图标」): the home screen icon. The default is the app's own `AppIcon` (纸色,
// the hand-drawn helper on paper, #709); the others are alternate icon sets in the asset catalog
// (`ASSETCATALOG_COMPILER_ALTERNATE_APPICON_NAMES`): 蜜桃 the same helper on peach (kept PNGs), Didot and Aura drawn by
// scripts/assets/make_alt_icons.py. The system remembers the choice itself, so there is nothing to store: what is set
// now is `alternateIconName`. iPhone and iPad only: the watch and the Mac keep their one icon.

/// One icon the user can pick.
nonisolated enum AppIconChoice: String, CaseIterable, Identifiable, Sendable {
    case standard
    case peach
    case didot
    case aura

    var id: String { rawValue }

    /// The icon set's name for `setAlternateIconName`; nil is the primary icon.
    var iconName: String? {
        switch self {
        case .standard: nil
        case .peach: "AppIcon-Peach"
        case .didot: "AppIcon-Didot"
        case .aura: "AppIcon-Aura"
        }
    }

    /// The picture the picker shows (an image set with any / dark, read from the icon set by the script).
    var preview: String {
        switch self {
        case .standard: "app-icon-default"
        case .peach: "app-icon-peach"
        case .didot: "app-icon-didot"
        case .aura: "app-icon-aura"
        }
    }

    var title: String {
        switch self {
        case .standard: "纸色"
        case .peach: "蜜桃"
        case .didot: "p."
        case .aura: "气场"
        }
    }

    var subtitle: String {
        switch self {
        case .standard: "默认"
        case .peach: "手绘小助手"
        case .didot: "刊头"
        case .aura: "光晕"
        }
    }

    /// 纸色 (the default) and 蜜桃 follow 设置 › 外观 › 深浅色 (#710). The system picks a set's look by the home screen's
    /// appearance, not the app's, so each also has a set with only its light picture and one with only its dark
    /// picture (`AppIcon-PaperLight` / `…Dark`, `AppIcon-PeachLight` / `…Dark`); the other choices are one set whatever
    /// the app is.
    private var fixedLooks: (light: String, dark: String)? {
        switch self {
        case .standard: ("AppIcon-PaperLight", "AppIcon-PaperDark")
        case .peach: ("AppIcon-PeachLight", "AppIcon-PeachDark")
        case .didot, .aura: nil
        }
    }

    var followsScheme: Bool { fixedLooks != nil }

    /// The icon set for the app's 深浅色: 跟随系统 is the set with both looks (nil: the primary icon).
    func iconName(for scheme: ColorSchemeChoice) -> String? {
        guard let fixed = fixedLooks else { return iconName }
        switch scheme {
        case .system: return iconName
        case .light: return fixed.light
        case .dark: return fixed.dark
        }
    }

    /// The look the home screen shows it in: 纸色 / 蜜桃 under 浅色 / 深色 take that look; the others, and 跟随系统,
    /// the system's (`system`).
    func homeScreenLook(_ scheme: ColorSchemeChoice, system: ColorScheme) -> ColorScheme {
        guard followsScheme else { return system }
        switch scheme {
        case .system: return system
        case .light: return .light
        case .dark: return .dark
        }
    }

    /// The alternate icon sets the choice can be on (all of them ship); the primary icon is not one.
    var iconNames: [String] { [iconName, fixedLooks?.light, fixedLooks?.dark].compactMap { $0 } }

    /// The choice an icon name stands for; an unknown name (an icon a later build dropped: 余烬, 薄荷, 石墨 went in #709)
    /// reads as the default.
    init(iconName: String?) {
        self = iconName.flatMap { name in Self.allCases.first { $0.iconNames.contains(name) } } ?? .standard
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

    /// The app's 深浅色 the icon should match (#710): the latest `follow` or `select` gave it.
    @ObservationIgnored private var scheme: ColorSchemeChoice = .system

    /// Puts the choice on, in the set for the app's 深浅色 (`scheme`, else the last one given).
    func select(_ choice: AppIconChoice, scheme: ColorSchemeChoice? = nil) async {
        if let scheme { self.scheme = scheme }
        guard let system, available, choice != current, switching == nil else { return }
        switching = choice
        failure = nil
        do {
            try await system.setAlternateIconName(choice.iconName(for: self.scheme))
            current = choice
            switching = nil
        } catch {
            switching = nil
            failure = "没换成：\(error.localizedDescription)"
            return
        }
        await settle(system) // 深浅色 may have changed while the system was asking
    }

    /// 深浅色 changed, or the app came to the front: an icon that follows it moves to that look's set (the system
    /// confirms with its alert, as for any switch); the others stay as they are, and an icon already right is left
    /// alone (no alert). A change while a switch is in flight is not lost: that switch settles on the latest when it ends.
    func follow(_ scheme: ColorSchemeChoice) async {
        self.scheme = scheme
        guard let system, available, switching == nil else { return }
        await settle(system)
    }

    /// Switches until the icon is on the set for the latest `scheme`. Not when the system has a set this build no
    /// longer ships (余烬 / 薄荷 / 石墨): the home screen already shows the primary icon, and a switch on its own would
    /// pop the system's alert at launch.
    private func settle(_ system: AppIconSetting) async {
        while current.followsScheme, switching == nil {
            if let name = system.alternateIconName, !AppIconChoice.allCases.contains(where: { $0.iconNames.contains(name) }) { return }
            let name = current.iconName(for: scheme)
            guard name != system.alternateIconName else { return }
            switching = current
            failure = nil
            do {
                try await system.setAlternateIconName(name)
                switching = nil
            } catch {
                switching = nil
                failure = "没换成：\(error.localizedDescription)"
                return
            }
        }
    }
}

#if os(iOS)
extension AppIconChoice {
    /// The system's look, the home screen's: read off the screen, as the window carries the app's own 深浅色.
    @MainActor static var systemLook: ColorScheme {
        let screen = UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.screen }.first
        return screen?.traitCollection.userInterfaceStyle == .dark ? .dark : .light
    }
}
#endif

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
