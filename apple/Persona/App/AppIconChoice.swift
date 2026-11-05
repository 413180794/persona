import SwiftUI
#if os(iOS) || os(visionOS)
import UIKit
#endif

// The home screen icon is the 形象's (docs/persona.md「App 图标」, #3203): there is no setting of its own. Each built-in
// 形象 has an icon in the asset catalog: 团子, the default, is the app's own `AppIcon` (纸色, the hand-drawn helper on
// paper, #709); 面罩 and 汇流 are alternate icon sets (`ASSETCATALOG_COMPILER_ALTERNATE_APPICON_NAMES`) of their faces,
// drawn by scripts/assets/make_alt_icons.py. iOS switches only to icons the app ships, so a 形象 from the hub or an
// extension puts on the built-in one it names (`Persona.appIcon`). The system remembers the icon itself, so there is
// nothing to store: what is set now is `alternateIconName`. iPhone and iPad only: the watch and the Mac keep their one icon.

/// One icon the app ships.
nonisolated enum AppIconChoice: String, CaseIterable, Identifiable, Sendable {
    case standard
    case visor
    case circuit

    var id: String { rawValue }

    /// The icon set's name for `setAlternateIconName`; nil is the primary icon.
    var iconName: String? {
        switch self {
        case .standard: nil
        case .visor: "AppIcon-Visor"
        case .circuit: "AppIcon-Circuit"
        }
    }

    /// The picture the app draws it with (an image set with any / dark, read from the icon set by the script): the
    /// logo beside Persona's name.
    var preview: String {
        switch self {
        case .standard: "app-icon-default"
        case .visor: "app-icon-visor"
        case .circuit: "app-icon-circuit"
        }
    }

    /// The built-in 形象 whose icon it is.
    var lookID: String {
        switch self {
        case .standard: Persona.builtinID
        case .visor: Persona.visor.id
        case .circuit: Persona.circuit.id
        }
    }

    /// The icon of the built-in 形象 with that id; the default's for any other (a built-in with no icon yet, a typo
    /// in a package's `app_icon_fallback`, none given).
    init(lookID: String?) {
        self = Self.allCases.first { $0.lookID == lookID } ?? .standard
    }

    /// 纸色 (the default) follows 设置 › 外观 › 深浅色 (#710). The system picks a set's look by the home screen's
    /// appearance, not the app's, so it also has a set with only its light picture and one with only its dark
    /// picture (`AppIcon-PaperLight` / `…Dark`); the others are one set whatever the app is.
    private var fixedLooks: (light: String, dark: String)? {
        switch self {
        case .standard: ("AppIcon-PaperLight", "AppIcon-PaperDark")
        case .visor, .circuit: nil
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

    /// The look the home screen shows it in: 纸色 under 浅色 / 深色 takes that look; the others, and 跟随系统, the
    /// system's (`system`).
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

    /// An alternate icon set this build ships.
    static func ships(_ name: String) -> Bool { allCases.contains { $0.iconNames.contains(name) } }

    /// The choice an icon name stands for; an unknown name (an icon a later build dropped: 余烬, 薄荷, 石墨 went in
    /// #709, 蜜桃, p., 气场 in #3203) reads as the default, which is what the home screen shows for it.
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

#if os(iOS) || os(visionOS)
extension UIApplication: AppIconSetting {}
#endif

/// The home screen icon kept on the 形象's (`follow`): what is on now, and the last failure to switch.
@MainActor @Observable final class AppIconModel {
    private let system: AppIconSetting?
    /// The icon on the home screen: the system's at launch, then each switch that went through.
    private(set) var current: AppIconChoice
    private(set) var switching = false
    private(set) var failure: String?

    /// Off where the system cannot change the icon (the Mac, an app with alternate icons turned off).
    var available: Bool { system?.supportsAlternateIcons ?? false }

    init(system: AppIconSetting?) {
        self.system = system
        current = AppIconChoice(iconName: system?.alternateIconName)
    }

    /// The 形象's icon the home screen should have; nil until the 形象 is known (a package the list has not named yet).
    @ObservationIgnored private var wanted: AppIconChoice?
    /// The app's 深浅色 the icon should match (#710).
    @ObservationIgnored private var scheme: ColorSchemeChoice = .system

    /// The 形象 or 深浅色 changed, or the app came to the front: the home screen moves to the 形象's icon (`look`, else
    /// the last one given) in the set for 深浅色 (`scheme`, else the last one given). The system confirms with its
    /// alert, as for any switch; an icon already right is left alone (no alert). This is also the migration from the
    /// old 设置 › 外观 › App 图标: the first time the updated app is in front, the icon goes to the 形象's. A change
    /// while a switch is in flight is not lost: that switch settles on the latest when it ends.
    func follow(_ look: AppIconChoice?, scheme: ColorSchemeChoice? = nil) async {
        if let look { wanted = look }
        if let scheme { self.scheme = scheme }
        guard let system, available, !switching else { return }
        await settle(system)
    }

    /// Switches until the icon is on the set for the latest `wanted` and `scheme`.
    private func settle(_ system: AppIconSetting) async {
        while let wanted, !switching {
            let name = wanted.iconName(for: scheme)
            let shown = system.alternateIconName
            // A set this build no longer ships (蜜桃, p., 气场…) already shows as the primary icon: putting the primary
            // on would only pop the system's alert.
            if name == shown || (name == nil && shown.map { !AppIconChoice.ships($0) } == true) {
                current = wanted
                return
            }
            switching = true
            failure = nil
            do {
                try await system.setAlternateIconName(name)
                current = wanted
                switching = false
            } catch {
                switching = false
                failure = "桌面图标没换成：\(error.localizedDescription)"
                return
            }
        }
    }
}

#if os(iOS) || os(visionOS)
extension AppIconChoice {
    /// The system's look, the home screen's: read off the screen, as the window carries the app's own 深浅色.
    @MainActor static var systemLook: ColorScheme {
        #if os(visionOS)
        return .dark // visionOS draws its windows on glass in the room: there is no light home screen to follow
        #else
        let screen = UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.screen }.first
        return screen?.traitCollection.userInterfaceStyle == .dark ? .dark : .light
        #endif
    }
}
#endif

extension AppIconModel {
    /// The app's own: UIApplication on iPhone and iPad, nothing on the Mac.
    static func live() -> AppIconModel {
        #if os(iOS) || os(visionOS)
        AppIconModel(system: UIApplication.shared)
        #else
        AppIconModel(system: nil)
        #endif
    }
}
