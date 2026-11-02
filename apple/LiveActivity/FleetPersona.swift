import Foundation
import OSLog
import Security
import SwiftUI
import UIKit

/// The look's drawing and where the app leaves it for the extension (the value itself: Shared/FleetState.swift).
nonisolated extension FleetLook {
    /// Before the app told the activity anything: the default persona in the app's default accent.
    static let standard = FleetLook(design: PersonaFace.Design.visor.rawValue, light: 0x007AFF, dark: 0x0A84FF)

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

    /// What the app saved last; `standard` when nothing is readable. Cheap enough to call on every render. The widget
    /// extension tells what it read (`FleetLookReport`).
    static var saved: FleetLook {
        let (data, status) = FleetKeychain.read(FleetLookStore.lookAccount)
        let look = data.flatMap { try? JSONDecoder().decode(FleetLook.self, from: $0) }
        FleetLookReport.note { $0.look(status, look, bytes: data?.count) }
        return look ?? standard
    }

    static func load(_ store: FleetLookStore) -> FleetLook {
        store.read(FleetLookStore.lookAccount).flatMap { try? JSONDecoder().decode(FleetLook.self, from: $0) } ?? standard
    }

    /// The package's faces as saved, when they are this look's.
    func faces() -> FleetFaces? {
        guard package != nil else { return nil }
        let (data, status) = FleetKeychain.read(FleetLookStore.facesAccount)
        let faces = data.flatMap { try? PropertyListDecoder().decode(FleetFaces.self, from: $0) }
        FleetLookReport.note { $0.faces(status, faces?.package, bytes: data?.count) }
        return faces?.package == package ? faces : nil
    }

    func faces(_ store: FleetLookStore) -> FleetFaces? {
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
            if look.faces(store) == nil {
                if let made = faces(), made.package == package, let data = try? PropertyListEncoder().encode(made) {
                    changed = store.write(data, FleetLookStore.facesAccount)
                } else {
                    FleetKeychain.log.error("no faces for \(package, privacy: .public): drawn in its color")
                }
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

nonisolated extension FleetState {
    /// The persona to draw: the one the content carries (the app's updates, the hub's pushes), else — an older hub —
    /// what the app left in the keychain. The keychain is read either way, so the extension's report says whether it
    /// can be.
    var drawnLook: FleetLook {
        let kept = FleetLook.saved
        FleetLookReport.note { $0.content(look) }
        return look ?? kept
    }
}

/// A hub package's faces on the activity (its pictures are the user's and stay on the hub and the phone, never in the
/// repository): the first frame of each state's dot, the one the app holds still with Reduce Motion, and the avatar
/// for a state the package has no frames for. PNGs as large as the activity's largest face, drawn at its size, and
/// small: a phone may not draw a Live Activity whose picture is larger than where it shows (an image decoded at 1× is
/// as many points as it has pixels), and Live Activity libraries keep each picture under 4 KB; the simulator draws
/// anything (docs/live-activity.md「为什么 build 98 还不对」).
nonisolated struct FleetFaces: Codable, Hashable, Sendable {
    var package: String
    /// PersonaState raw value → PNG, only the states the package draws itself.
    var states: [String: Data]
    var avatar: Data?

    /// The largest face on the activity (the lock screen's, the expanded island's), in points; each picture is drawn
    /// at this size whatever its pixels.
    static let points: CGFloat = 26
    /// Its pixels at 3×, the most a picture gets.
    static let pixels: CGFloat = points * 3
    /// The most bytes a picture takes: a busier one is made with fewer pixels.
    static let maxBytes = 4000

    /// The picture for a state, and whether it is that state's own (else the avatar or the idle frame: the activity
    /// adds the state's cue, as it does on the built-ins).
    func face(_ state: PersonaState) -> (png: Data, own: Bool)? {
        if let own = states[state.rawValue] { return (own, true) }
        if let other = avatar ?? states[PersonaState.idle.rawValue] { return (other, false) }
        return nil
    }

    /// A picture made into one of them: square, PNG, `pixels` on a side or fewer to stay within `maxBytes` (a real
    /// persona's frame is 11–15 KB at 78px and fits at about 36–42px; down to 30, nil when even that is too large).
    static func png(_ image: UIImage) -> Data? {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.preferredRange = .standard // 8 bits a channel, whatever the screen
        for side in stride(from: pixels, through: 30, by: -6) {
            let size = CGSize(width: side, height: side)
            let data = UIGraphicsImageRenderer(size: size, format: format).image { _ in
                image.draw(in: CGRect(origin: .zero, size: size))
            }.pngData()
            if let data, data.count <= maxBytes { return data }
        }
        return nil
    }

    /// A picture as the activity draws it: `points` on a side, at the scale its pixels give.
    static func image(_ png: Data) -> UIImage? {
        guard let cg = UIImage(data: png)?.cgImage else { return nil }
        return UIImage(cgImage: cg, scale: CGFloat(cg.width) / points, orientation: .up)
    }
}

/// Where the app leaves the look for the extension: a keychain item in a group they share (Info.plist
/// `JarvisLookKeychainGroup`), the same way the push key is shared with JarvisNotify. Readable on the lock screen once
/// the phone was unlocked after boot. Tests pass one of their own.
nonisolated struct FleetLookStore: Sendable {
    static let lookAccount = "fleet-look"
    static let facesAccount = "fleet-faces"
    /// What the widget extension last read (`FleetLookReport`), written by the extension.
    static let seenAccount = "fleet-seen"

    var read: @Sendable (String) -> Data?
    /// Nil deletes. False when it could not be saved.
    var write: @Sendable (Data?, String) -> Bool

    static let keychain = FleetLookStore(read: { FleetKeychain.read($0).data }, write: { FleetKeychain.write($0, $1) == errSecSuccess })
}

/// The shared keychain group itself. Both sides use the group by name, so the app compares what it saves with the very
/// item the extension reads (not one of its other groups'), and every answer is logged and kept for 灵动岛诊断.
nonisolated enum FleetKeychain {
    static let service = "jarvis.look"
    /// The island's log (Console: subsystem `jarvis`, category `island`): every keychain answer on either side, so a
    /// phone that draws the wrong persona says which step failed (docs/live-activity.md「诊断」).
    static let log = Logger(subsystem: "jarvis", category: "island")

    /// `<team>.<bundle id>.look`, in both bundles' Info.plist; nil without a team (an empty one leaves just the dot).
    static var group: String? {
        (Bundle.main.object(forInfoDictionaryKey: "JarvisLookKeychainGroup") as? String).flatMap { $0.isEmpty || $0.hasPrefix(".") ? nil : $0 }
    }

    /// The item and the keychain's answer. In the group by name; a bundle not signed with it (errSecMissingEntitlement)
    /// looks in the groups it has.
    static func read(_ account: String) -> (data: Data?, status: OSStatus) {
        func copy(_ group: String?) -> (Data?, OSStatus) {
            var q = query(account, group: group)
            q[kSecReturnData as String] = true
            q[kSecMatchLimit as String] = kSecMatchLimitOne
            var out: AnyObject?
            let status = SecItemCopyMatching(q as CFDictionary, &out)
            return (out as? Data, status)
        }
        var (data, status) = copy(group)
        if status == errSecMissingEntitlement, group != nil { (data, status) = copy(nil) }
        if status != errSecSuccess && status != errSecItemNotFound {
            log.error("read \(account, privacy: .public) in \(group ?? "-", privacy: .public): \(status)")
        }
        return (data, status)
    }

    /// Saves (nil deletes) and reads it back. Signed without the shared group (a free personal team, or a build that
    /// lost it: scripts/entitlements.py), nothing is saved and the extension draws `standard`.
    static func write(_ data: Data?, _ account: String) -> OSStatus {
        guard let group else {
            log.error("write \(account, privacy: .public): no keychain group in Info.plist")
            return remember(account, errSecMissingEntitlement, bytes: data?.count)
        }
        let deleted = SecItemDelete(query(account, group: group) as CFDictionary)
        guard let data else { return remember(account, deleted == errSecItemNotFound ? errSecSuccess : deleted, bytes: nil) }
        var add = query(account, group: group)
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        var status = SecItemAdd(add as CFDictionary, nil)
        if status == errSecSuccess, read(account).data != data { status = errSecDataNotAvailable }
        if status == errSecSuccess {
            log.notice("wrote \(account, privacy: .public), \(data.count) bytes, in \(group, privacy: .public)")
        } else {
            log.error("write \(account, privacy: .public), \(data.count) bytes, in \(group, privacy: .public): \(status)")
        }
        return remember(account, status, bytes: data.count)
    }

    /// The last write of each account in this process: account → (when, answer, bytes).
    nonisolated(unsafe) private static var writes: [String: (at: Date, status: OSStatus, bytes: Int?)] = [:]
    private static let lock = NSLock()

    static var lastWrites: [String: (at: Date, status: OSStatus, bytes: Int?)] { lock.withLock { writes } }

    private static func remember(_ account: String, _ status: OSStatus, bytes: Int?) -> OSStatus {
        lock.withLock { writes[account] = (.now, status, bytes) }
        return status
    }

    /// One item of the service as the keychain lists it.
    struct Item: Hashable, Sendable {
        var account: String
        var group: String
        var bytes: Int
        var modified: Date?
    }

    /// Every item of the service this bundle can see, in any of its groups, and the keychain's answer.
    static func items() -> ([Item], OSStatus) {
        var q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecReturnAttributes as String: true, kSecReturnData as String: true,
                                kSecMatchLimit as String: kSecMatchLimitAll]
        q[kSecAttrSynchronizable as String] = kSecAttrSynchronizableAny
        var out: AnyObject?
        let status = SecItemCopyMatching(q as CFDictionary, &out)
        let found = (out as? [[String: Any]] ?? []).map { a in
            Item(account: a[kSecAttrAccount as String] as? String ?? "?", group: a[kSecAttrAccessGroup as String] as? String ?? "?",
                 bytes: (a[kSecValueData as String] as? Data)?.count ?? 0, modified: a[kSecAttrModificationDate as String] as? Date)
        }
        return (found.sorted { ($0.account, $0.group) < ($1.account, $1.group) }, status)
    }

    private static func query(_ account: String, group: String?) -> [String: Any] {
        var q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: account]
        if let group { q[kSecAttrAccessGroup as String] = group }
        return q
    }
}

/// What the widget extension last drew from, written by it into the shared group (`fleet-seen`) and shown on 灵动岛诊断:
/// the keychain's answer for the look and the faces, and what they held. Written only when it changes or ten minutes
/// on, never on every render. An extension that cannot use the keychain cannot write it either: then only its log
/// says so.
nonisolated struct FleetLookReport: Codable, Hashable, Sendable {
    var at = Date.distantPast
    /// The look in the activity's content ("design" or "design · package"), nil when it carried none.
    var content: String?
    var lookStatus: Int32?
    var design: String?
    var package: String?
    var lookBytes: Int?
    var facesStatus: Int32?
    var facesPackage: String?
    var facesBytes: Int?

    mutating func content(_ look: FleetLook?) {
        content = look.map { Self.describe($0) }
    }

    static func describe(_ look: FleetLook) -> String { [look.design, look.package].compactMap { $0 }.joined(separator: " · ") }

    mutating func look(_ status: OSStatus, _ look: FleetLook?, bytes: Int?) {
        (lookStatus, design, package, lookBytes) = (status, look?.design, look?.package, bytes)
    }

    mutating func faces(_ status: OSStatus, _ package: String?, bytes: Int?) {
        (facesStatus, facesPackage, facesBytes) = (status, package, bytes)
    }

    /// The same reading, whenever it was.
    func same(_ other: FleetLookReport) -> Bool {
        var a = self, b = other
        (a.at, b.at) = (.distantPast, .distantPast)
        return a == b
    }

    /// What the extension wrote last; nil when it never did (or this bundle cannot read it).
    static var seen: FleetLookReport? {
        FleetKeychain.read(FleetLookStore.seenAccount).data.flatMap { try? JSONDecoder().decode(FleetLookReport.self, from: $0) }
    }

    nonisolated(unsafe) private static var current = FleetLookReport()
    nonisolated(unsafe) private static var written: FleetLookReport?
    private static let lock = NSLock()
    private static let inExtension = Bundle.main.bundleURL.pathExtension == "appex"

    /// Records a reading in the widget extension (the app's own readings are not the extension's) and writes the report
    /// when it says something new.
    static func note(_ change: (inout FleetLookReport) -> Void) {
        guard inExtension else { return }
        let due: FleetLookReport? = lock.withLock {
            change(&current)
            if let w = written, w.same(current), Date.now.timeIntervalSince(w.at) < 600 { return nil }
            current.at = .now
            written = current
            return current
        }
        guard let due else { return }
        FleetKeychain.log.notice("extension drew \(due.content ?? "no look in the content", privacy: .public); keychain look \(due.lookStatus ?? 1) \(due.design ?? "-", privacy: .public) \(due.package ?? "-", privacy: .public), faces \(due.facesStatus ?? 1) \(due.facesBytes ?? 0) bytes")
        if let data = try? JSONEncoder().encode(due) { _ = FleetKeychain.write(data, FleetLookStore.seenAccount) }
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
            if let face = (faces ?? look.faces())?.face(state), let image = FleetFaces.image(face.png) {
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
