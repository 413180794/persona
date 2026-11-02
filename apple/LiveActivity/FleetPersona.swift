import Foundation
import OSLog
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
        let store = FleetLookStore.shared
        let data = store.read(FleetLookStore.lookFile)
        let look = data.flatMap { try? JSONDecoder().decode(FleetLook.self, from: $0) }
        FleetLookReport.note { $0.saved(store.root != nil, look) }
        return look ?? standard
    }

    static func load(_ store: FleetLookStore) -> FleetLook {
        store.read(FleetLookStore.lookFile).flatMap { try? JSONDecoder().decode(FleetLook.self, from: $0) } ?? standard
    }

    /// The package's picture for a state as saved, when this look is a package's; read on its own, the one the
    /// activity is about to draw.
    func face(_ state: PersonaState) -> (png: Data, own: Bool)? {
        guard let package else { return nil }
        let face = FleetLookStore.shared.face(package, state)
        FleetLookReport.note { $0.face(package, face.map { ($0.file, $0.png.count) }) }
        return face.map { ($0.png, $0.own) }
    }

    func faces(_ store: FleetLookStore) -> FleetFaces? {
        package.flatMap { store.faces($0) }
    }

    /// Saves it for the extension, with a package's faces (made only when the saved ones are not this package's). True
    /// when what the extension reads changed. Faces that cannot be made leave the package drawn in its color.
    @discardableResult
    static func save(_ look: FleetLook, faces: () -> FleetFaces?, to store: FleetLookStore = .shared) -> Bool {
        var changed = false
        if let package = look.package {
            // The faces before the look; a look is drawn only with faces of its own package, never another's.
            if !store.hasFaces(package) {
                if let made = faces(), made.package == package {
                    changed = store.save(made)
                } else {
                    FleetLookStore.log.error("no faces for \(package, privacy: .public): drawn in its color")
                }
            }
        } else {
            _ = store.save(nil) // a built-in: no picture of the user's left behind
        }
        if look != load(store), let data = try? JSONEncoder().encode(look), store.write(data, FleetLookStore.lookFile) {
            changed = true
        }
        return changed
    }
}

nonisolated extension FleetState {
    /// The persona to draw: the one the content carries (the app's updates, the hub's pushes), else — an older hub —
    /// what the app saved. The saved one is read either way, so the extension's report says whether it can be.
    var drawnLook: FleetLook {
        let kept = FleetLook.saved
        FleetLookReport.note { $0.content(look) }
        return look ?? kept
    }
}

/// A hub package's faces on the activity (its pictures are the user's and stay on the hub and the phone, never in the
/// repository): one picture a state, the first frame of its dot (the one the app holds still with Reduce Motion), and
/// the avatar only when a state has none. PNGs as large as the activity's largest face, drawn at its size, and small:
/// a phone may not draw a Live Activity whose picture is larger than where it shows (an image decoded at 1× is as many
/// points as it has pixels), and Live Activity libraries keep each picture under 4 KB; the simulator draws anything
/// (docs/live-activity.md「为什么 build 98 还不对」). Each is a file of its own, read only when drawn.
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

/// Where the app leaves the look and a package's pictures for the widget extension: files in the App Group container
/// they share (Info.plist `JarvisAppGroup`), under `Fleet/`. On a phone a Live Activity's extension cannot use the
/// keychain (build 103's 灵动岛诊断, 2026-11-02: the app wrote and read back both items, the extension never reported),
/// so nothing goes there any more. Readable on the lock screen once the phone was unlocked after boot. Tests pass a
/// directory of their own.
///
///     Fleet/look.json                    the look, for content without one (an older hub)
///     Fleet/faces/<package>/<state>.png  one picture a state, avatar.png for the states without one; one package
///     Fleet/seen.json                    what the extension last read (FleetLookReport)
nonisolated struct FleetLookStore: Sendable {
    static let lookFile = "look.json"
    static let seenFile = "seen.json"
    static let facesDir = "faces"
    static let avatarName = "avatar"

    /// The island's log (Console: subsystem `jarvis`, category `island`): every write on either side and every read
    /// that fails, so a phone that draws the wrong persona says which step failed (docs/live-activity.md「诊断」).
    static let log = Logger(subsystem: "jarvis", category: "island")

    /// `Fleet/` in the container; nil when this bundle has none (not signed with the group: nothing is saved, and the
    /// extension draws the content's look, a package in its color).
    var root: URL?

    static var shared: FleetLookStore { FleetLookStore(root: container?.appending(path: "Fleet", directoryHint: .isDirectory)) }

    /// The group's id, `group.<bundle id>` unless the config says otherwise, in both bundles' Info.plist.
    static var group: String? {
        (Bundle.main.object(forInfoDictionaryKey: "JarvisAppGroup") as? String).flatMap { $0.isEmpty ? nil : $0 }
    }

    static var container: URL? {
        group.flatMap { FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: $0) }
    }

    func read(_ name: String) -> Data? {
        guard let url = root?.appending(path: name) else { return nil }
        do {
            return try Data(contentsOf: url)
        } catch CocoaError.fileReadNoSuchFile {
            return nil
        } catch {
            Self.log.error("read \(name, privacy: .public): \(error.localizedDescription, privacy: .public)")
            return nil
        }
    }

    /// Saves (nil deletes) a file, whole or not at all. False when it could not be.
    func write(_ data: Data?, _ name: String) -> Bool {
        let error: Error?
        do {
            guard let root else { throw CocoaError(.fileNoSuchFile) }
            let url = root.appending(path: name)
            if let data {
                try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
                try data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
            } else if FileManager.default.fileExists(atPath: url.path(percentEncoded: false)) {
                try FileManager.default.removeItem(at: url)
            }
            error = nil
        } catch let e {
            error = e
        }
        Self.wrote(name, error, bytes: data?.count)
        return error == nil
    }

    /// The package's directory: its id and rev with anything but letters, digits, `.`, `-` and `_` as `_`.
    static func folder(_ package: String) -> String {
        String(package.map { $0.isASCII && ($0.isLetter || $0.isNumber || "._-".contains($0)) ? $0 : "_" })
    }

    func hasFaces(_ package: String) -> Bool {
        guard let dir = root?.appending(path: Self.facesDir).appending(path: Self.folder(package)) else { return false }
        return FileManager.default.fileExists(atPath: dir.path(percentEncoded: false))
    }

    /// One state's picture, its file and whether it is the state's own (else the avatar, else the idle frame).
    func face(_ package: String, _ state: PersonaState) -> (png: Data, own: Bool, file: String)? {
        let dir = Self.facesDir + "/" + Self.folder(package) + "/"
        for (name, own) in [(state.rawValue, true), (Self.avatarName, false), (PersonaState.idle.rawValue, false)] {
            if let png = read(dir + name + ".png") { return (png, own, name + ".png") }
        }
        return nil
    }

    /// All of a package's pictures (灵动岛诊断, tests).
    func faces(_ package: String) -> FleetFaces? {
        let dir = Self.facesDir + "/" + Self.folder(package) + "/"
        var states: [String: Data] = [:]
        for s in PersonaState.allCases { states[s.rawValue] = read(dir + s.rawValue + ".png") }
        let avatar = read(dir + Self.avatarName + ".png")
        return states.isEmpty && avatar == nil ? nil : FleetFaces(package: package, states: states, avatar: avatar)
    }

    /// Replaces the saved faces with these (nil: none), whole: written beside the old ones, then moved in, so the
    /// extension never reads half of one package and half of another. True when they were.
    func save(_ faces: FleetFaces?) -> Bool {
        let name = Self.facesDir + (faces.map { "/" + Self.folder($0.package) } ?? "")
        let fm = FileManager.default
        let error: Error?
        do {
            guard let root else { throw CocoaError(.fileNoSuchFile) }
            let all = root.appending(path: Self.facesDir, directoryHint: .isDirectory)
            try fm.createDirectory(at: all, withIntermediateDirectories: true)
            var keep: String?
            if let faces {
                let folder = Self.folder(faces.package)
                let new = all.appending(path: ".new-" + folder, directoryHint: .isDirectory)
                try? fm.removeItem(at: new)
                try fm.createDirectory(at: new, withIntermediateDirectories: true)
                var files = faces.states.map { ($0.key, $0.value) }
                if let avatar = faces.avatar { files.append((Self.avatarName, avatar)) }
                for (file, png) in files {
                    try png.write(to: new.appending(path: file + ".png"), options: .completeFileProtectionUntilFirstUserAuthentication)
                }
                let dest = all.appending(path: folder, directoryHint: .isDirectory)
                if fm.fileExists(atPath: dest.path(percentEncoded: false)) { try fm.removeItem(at: dest) }
                try fm.moveItem(at: new, to: dest)
                keep = folder
            }
            for other in try fm.contentsOfDirectory(atPath: all.path(percentEncoded: false)) where other != keep {
                try fm.removeItem(at: all.appending(path: other))
            }
            error = nil
        } catch let e {
            error = e
        }
        Self.wrote(name, error, bytes: faces.map { $0.states.values.reduce($0.avatar?.count ?? 0) { $0 + $1.count } })
        return error == nil
    }

    /// One file as the container lists it.
    struct File: Hashable, Sendable {
        var name: String
        var bytes: Int
        var modified: Date?
    }

    /// Every file under `Fleet/`, by path; nil without a container.
    func files() -> [File]? {
        guard let root, let walk = FileManager.default.enumerator(at: root, includingPropertiesForKeys: [.fileSizeKey, .contentModificationDateKey, .isRegularFileKey]) else {
            return root == nil ? nil : []
        }
        let base = root.standardizedFileURL.path(percentEncoded: false)
        var found: [File] = []
        for case let url as URL in walk {
            guard let v = try? url.resourceValues(forKeys: [.fileSizeKey, .contentModificationDateKey, .isRegularFileKey]), v.isRegularFile == true else { continue }
            let path = url.standardizedFileURL.path(percentEncoded: false)
            found.append(File(name: path.hasPrefix(base) ? String(path.dropFirst(base.count)).trimmingCharacters(in: ["/"]) : url.lastPathComponent,
                              bytes: v.fileSize ?? 0, modified: v.contentModificationDate))
        }
        return found.sorted { $0.name < $1.name }
    }

    /// The last write of each file in this process: name → (when, the error or nil, bytes).
    nonisolated(unsafe) private static var writes: [String: (at: Date, error: String?, bytes: Int?)] = [:]
    private static let lock = NSLock()

    static var lastWrites: [String: (at: Date, error: String?, bytes: Int?)] { lock.withLock { writes } }

    private static func wrote(_ name: String, _ error: Error?, bytes: Int?) {
        if let error {
            log.error("write \(name, privacy: .public), \(bytes ?? 0) bytes: \(error.localizedDescription, privacy: .public)")
        } else {
            log.notice("wrote \(name, privacy: .public), \(bytes ?? 0) bytes")
        }
        lock.withLock { writes[name] = (.now, error?.localizedDescription, bytes) }
    }
}

/// What the widget extension last drew from, written by it into the container (`seen.json`) and shown on 灵动岛诊断:
/// whether it has the container, the look saved there, and the package picture it read. Written only when it changes
/// or ten minutes on, never on every render.
nonisolated struct FleetLookReport: Codable, Hashable, Sendable {
    var at = Date.distantPast
    /// The look in the activity's content ("design" or "design · package"), nil when it carried none.
    var content: String?
    /// Whether the extension has the App Group container.
    var container: Bool?
    /// The look saved there, nil when there is none it can read.
    var saved: String?
    /// The package whose picture it looked for last (nil: a built-in, none looked for), the file it read (nil: none
    /// there) and its size.
    var facesPackage: String?
    var faceFile: String?
    var faceBytes: Int?

    mutating func content(_ look: FleetLook?) {
        content = look.map { Self.describe($0) }
    }

    static func describe(_ look: FleetLook) -> String { [look.design, look.package].compactMap { $0 }.joined(separator: " · ") }

    mutating func saved(_ container: Bool, _ look: FleetLook?) {
        (self.container, saved) = (container, look.map { Self.describe($0) })
    }

    mutating func face(_ package: String, _ read: (file: String, bytes: Int)?) {
        (facesPackage, faceFile, faceBytes) = (package, read?.file, read?.bytes)
    }

    /// The same reading, whenever it was.
    func same(_ other: FleetLookReport) -> Bool {
        var a = self, b = other
        (a.at, b.at) = (.distantPast, .distantPast)
        return a == b
    }

    /// What the extension wrote last; nil when it never did (or this bundle cannot read it).
    static var seen: FleetLookReport? {
        FleetLookStore.shared.read(FleetLookStore.seenFile).flatMap { try? JSONDecoder().decode(FleetLookReport.self, from: $0) }
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
        FleetLookStore.log.notice("extension drew \(due.content ?? "no look in the content", privacy: .public); container \(due.container == true ? "yes" : "no", privacy: .public), saved \(due.saved ?? "-", privacy: .public), face \(due.facesPackage ?? "-", privacy: .public) \(due.faceFile ?? "none", privacy: .public) \(due.faceBytes ?? 0) bytes")
        if let data = try? JSONEncoder().encode(due) { _ = FleetLookStore.shared.write(data, FleetLookStore.seenFile) }
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
    /// The package's faces; the state's picture is read from where the app left it when not given.
    var faces: FleetFaces?

    var body: some View {
        Group {
            if let face = faces?.face(state) ?? look.face(state), let image = FleetFaces.image(face.png) {
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
