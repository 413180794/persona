import Foundation
import Security
import SwiftUI
import UIKit

/// Which persona the Live Activity draws and in what color (docs/live-activity.md「形象」): the app's choice in
/// 设置 › 外观 › 形象 and the accent it draws that persona in, written by the app and read by the widget extension,
/// which cannot read the app's settings. Kept out of the ContentState on purpose: the hub's pushes replace the whole
/// state, and the hub does not know the phone's look.
nonisolated struct FleetLook: Codable, Hashable, Sendable {
    /// PersonaFace.Design's raw value: a built-in's drawing; for a package, what is drawn while its faces cannot be.
    var design: String
    /// The accent as "0xRRGGBB" values, light and dark.
    var light: UInt32
    var dark: UInt32
    /// A hub package ("<id>/<rev>"): drawn from its faces (`FleetFaces`), saved beside the look. Nil: a built-in.
    var package: String?

    /// Before the app wrote one (or a build without the shared keychain group): the default persona in the app's
    /// default accent.
    static let standard = FleetLook(design: PersonaFace.Design.visor.rawValue, light: 0x007AFF, dark: 0x0A84FF)

    init(design: String, light: UInt32, dark: UInt32, package: String? = nil) {
        self.design = design
        self.light = light
        self.dark = dark
        self.package = package
    }

    /// A design in a (dynamic) color, resolved to its light and dark values.
    init(design: PersonaFace.Design, tint: Color, package: String? = nil) {
        self.init(design: design.rawValue, light: Self.hex(tint, .light), dark: Self.hex(tint, .dark), package: package)
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

    /// What the app saved last; `standard` when nothing is readable. Cheap enough to call on every render.
    static var saved: FleetLook { load(.keychain) }

    static func load(_ store: FleetLookStore) -> FleetLook {
        store.read(FleetLookStore.lookAccount).flatMap { try? JSONDecoder().decode(FleetLook.self, from: $0) } ?? standard
    }

    /// The package's faces as saved, when they are this look's.
    func faces(_ store: FleetLookStore = .keychain) -> FleetFaces? {
        guard let package, let data = store.read(FleetLookStore.facesAccount),
              let faces = try? PropertyListDecoder().decode(FleetFaces.self, from: data), faces.package == package else { return nil }
        return faces
    }

    /// Saves it for the extension, with a package's faces (made only when the saved ones are not this package's). True
    /// when what the extension reads changed. Faces that cannot be made leave the package drawn in its color.
    @discardableResult
    static func save(_ look: FleetLook, faces: () -> FleetFaces?, to store: FleetLookStore = .keychain) -> Bool {
        var changed = false
        if let package = look.package {
            // The faces before the look; a look is drawn only with faces of its own package, never another's.
            if look.faces(store) == nil, let made = faces(), made.package == package,
               let data = try? PropertyListEncoder().encode(made), store.write(data, FleetLookStore.facesAccount) {
                changed = true
            }
        } else {
            _ = store.write(nil, FleetLookStore.facesAccount) // a built-in: no picture of the user's left behind
        }
        if look != load(store), let data = try? JSONEncoder().encode(look), store.write(data, FleetLookStore.lookAccount) {
            changed = true
        }
        return changed
    }
}

/// A hub package's faces on the activity (its pictures are the user's and stay on the hub and the phone, never in the
/// repository): the first frame of each state's dot, the one the app holds still with Reduce Motion, and the avatar
/// for a state the package has no frames for. PNGs as large as the activity's largest face.
nonisolated struct FleetFaces: Codable, Hashable, Sendable {
    var package: String
    /// PersonaState raw value → PNG, only the states the package draws itself.
    var states: [String: Data]
    var avatar: Data?

    /// The largest face on the activity (the lock screen's, the expanded island's) at 3×, in pixels.
    static let pixels: CGFloat = 78

    /// The picture for a state, and whether it is that state's own (else the avatar or the idle frame: the activity
    /// adds the state's cue, as it does on the built-ins).
    func face(_ state: PersonaState) -> (png: Data, own: Bool)? {
        if let own = states[state.rawValue] { return (own, true) }
        if let other = avatar ?? states[PersonaState.idle.rawValue] { return (other, false) }
        return nil
    }

    /// A picture made into one of them: square, `pixels` on a side, PNG.
    static func png(_ image: UIImage) -> Data? {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let side = CGSize(width: pixels, height: pixels)
        return UIGraphicsImageRenderer(size: side, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: side))
        }.pngData()
    }
}

/// Where the app leaves the look for the extension: a keychain item in a group they share (Info.plist
/// `JarvisLookKeychainGroup`), the same way the push key is shared with JarvisNotify. Readable on the lock screen once
/// the phone was unlocked after boot. Tests pass one of their own.
nonisolated struct FleetLookStore: Sendable {
    static let lookAccount = "fleet-look"
    static let facesAccount = "fleet-faces"

    var read: @Sendable (String) -> Data?
    /// Nil deletes. False when it could not be saved.
    var write: @Sendable (Data?, String) -> Bool

    static let keychain = FleetLookStore(read: { account in
        // Any group: the extension has only the shared one, and the app's own groups hold no item of this service.
        var q = query(account, group: nil)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess else { return nil }
        return out as? Data
    }, write: { data, account in
        // Signed without the shared group (a free personal team, or a build that lost it: scripts/entitlements.py),
        // nothing is saved and the extension draws `standard`.
        guard let group else { return false }
        SecItemDelete(query(account, group: group) as CFDictionary)
        guard let data else { return true }
        var add = query(account, group: group)
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        return SecItemAdd(add as CFDictionary, nil) == errSecSuccess
    })

    /// `<team>.<bundle id>.look`; nil without a team (an empty one leaves just the dot).
    private static var group: String? {
        (Bundle.main.object(forInfoDictionaryKey: "JarvisLookKeychainGroup") as? String).flatMap { $0.isEmpty || $0.hasPrefix(".") ? nil : $0 }
    }

    private static func query(_ account: String, group: String?) -> [String: Any] {
        var q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: "jarvis.look",
                                kSecAttrAccount as String: account]
        if let group { q[kSecAttrAccessGroup as String] = group }
        return q
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
/// effects, docs/live-activity.md「坑」), changing with a cross-fade when the state does. A package draws its picture
/// for the state; a state it has no frames for gets the built-ins' cue (the red ring, gray when offline). Its
/// drawing in its color while the faces cannot be read.
struct FleetFace: View {
    var size: CGFloat
    var state: PersonaState
    var look: FleetLook = .saved
    /// The package's faces; read from where the app left them when not given.
    var faces: FleetFaces?

    var body: some View {
        Group {
            if let face = (faces ?? look.faces())?.face(state), let image = UIImage(data: face.png) {
                Image(uiImage: image)
                    .resizable()
                    .interpolation(.high)
                    .scaledToFill()
                    .frame(width: size, height: size)
                    .clipShape(.circle)
                    .grayscale(!face.own && state == .offline ? 1 : 0)
                    .opacity(!face.own && state == .offline ? PersonaFace.offlineOpacity : 1)
                    .overlay {
                        if !face.own && state == .needsYou {
                            Circle().strokeBorder(FleetPalette.needsYou, lineWidth: max(PersonaFace.ringMin, size * PersonaFace.ringShare))
                        }
                    }
            } else {
                PersonaFace(size: size, design: look.faceDesign, state: state, tint: look.tint, mark: FleetPalette.mark)
            }
        }
        .frame(width: size, height: size)
        .id(state)
        .transition(.opacity)
        .accessibilityLabel("Jarvis \(state.title)")
    }
}
