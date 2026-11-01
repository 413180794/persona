import Foundation
import Security
import SwiftUI

/// Which persona the Live Activity draws and in what color (docs/live-activity.md「形象」): the app's choice in
/// 设置 › 外观 › 形象 and the accent it draws that persona in, written by the app and read by the widget extension,
/// which cannot read the app's settings. Kept out of the ContentState on purpose: the hub's pushes replace the whole
/// state, and the hub does not know the phone's look.
nonisolated struct FleetLook: Codable, Hashable, Sendable {
    /// PersonaFace.Design's raw value.
    var design: String
    /// The accent as "0xRRGGBB" values, light and dark.
    var light: UInt32
    var dark: UInt32

    /// Before the app wrote one (or a build without the shared keychain group): the default persona in the app's
    /// default accent.
    static let standard = FleetLook(design: PersonaFace.Design.visor.rawValue, light: 0x007AFF, dark: 0x0A84FF)

    init(design: String, light: UInt32, dark: UInt32) {
        self.design = design
        self.light = light
        self.dark = dark
    }

    /// A design in a (dynamic) color, resolved to its light and dark values.
    init(design: PersonaFace.Design, tint: Color) {
        self.init(design: design.rawValue, light: Self.hex(tint, .light), dark: Self.hex(tint, .dark))
    }

    static func hex(_ c: Color, _ scheme: ColorScheme) -> UInt32 {
        var env = EnvironmentValues()
        env.colorScheme = scheme
        let r = c.resolve(in: env)
        func ch(_ v: Float) -> UInt32 { UInt32((max(0, min(1, v)) * 255).rounded()) }
        return ch(r.red) << 16 | ch(r.green) << 8 | ch(r.blue)
    }

    var faceDesign: PersonaFace.Design { PersonaFace.Design(rawValue: design) ?? .visor }
    var tint: Color { Color(light: light, dark: dark) }

    // MARK: Storage: a keychain item in a group the app and the widget extension share (Info.plist
    // `JarvisLookKeychainGroup`), the same way the push key is shared with JarvisNotify.

    private static var accessGroup: String? {
        (Bundle.main.object(forInfoDictionaryKey: "JarvisLookKeychainGroup") as? String).flatMap { $0.isEmpty ? nil : $0 }
    }
    private static let service = "jarvis.look"
    private static let account = "fleet-look"

    private static func query(group: String?) -> [String: Any] {
        var q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: account]
        if let group { q[kSecAttrAccessGroup as String] = group }
        return q
    }

    /// What the app saved last; `standard` when nothing is readable. Cheap enough to call on every render.
    static var saved: FleetLook {
        var q = query(group: accessGroup)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data,
              let look = try? JSONDecoder().decode(FleetLook.self, from: data) else { return standard }
        return look
    }

    /// Saves it for the extension (readable on the lock screen once the phone was unlocked after boot). True when it
    /// changed.
    @discardableResult
    static func save(_ look: FleetLook) -> Bool {
        guard look != saved, let data = try? JSONEncoder().encode(look) else { return false }
        let group = accessGroup
        SecItemDelete(query(group: group) as CFDictionary)
        var add = query(group: group)
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        if SecItemAdd(add as CFDictionary, nil) == errSecMissingEntitlement, group != nil {
            // Signed without the shared group (a free personal team): the extension draws `standard`.
            return false
        }
        return true
    }
}

/// The Fleet's state as the persona's (PersonaState): the face says what the counts say.
nonisolated enum FleetPersona {
    /// - needs you → 等你处理 (red ring); working → 工作中; finished-unseen or nothing → 空闲;
    /// - only the assistant answering (no agent in the fleet working) → 思考中: the nearest the fleet has to it;
    /// - stale (the hub stopped updating it) → 离线, gray: the face of "this may be out of date". The fleet has no
    ///   error state of its own; a hub that cannot be reached is the error it shows.
    static func state(_ s: FleetState, stale: Bool) -> PersonaState {
        if stale { return .offline }
        switch s.phase {
        case .needsYou: return .needsYou
        case .working: return .working
        case .done, .none: return s.assistant ? .thinking : .idle
        }
    }
}

/// The persona on the activity: its still frame for the state (a Live Activity runs no timelines or repeating
/// effects, docs/live-activity.md「坑」), changing with a cross-fade when the state does.
struct FleetFace: View {
    var size: CGFloat
    var state: PersonaState
    var look: FleetLook = .saved

    var body: some View {
        PersonaFace(size: size, design: look.faceDesign, state: state, tint: look.tint, mark: FleetPalette.mark)
            .frame(width: size, height: size)
            .id(state)
            .transition(.opacity)
            .accessibilityLabel("Jarvis \(state.title)")
    }
}
