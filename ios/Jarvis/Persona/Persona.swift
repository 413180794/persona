import Foundation

/// A look for Jarvis (docs/persona.md): its name, its face, the animated dot in the Jarvis tab's top bar and its
/// colors. The built-in one is the app's own drawing (JarvisAvatar, in the style's accent); every other one is a
/// package on the hub (~/.jarvis/personas/<id>/) that the phone downloads and keeps (`PersonaStore`).
nonisolated struct Persona: Identifiable, Sendable, Hashable {
    static let builtinID = "jarvis"
    static let builtin = Persona(id: builtinID, name: "Jarvis", subtitle: "默认形象，跟随界面风格的颜色", rev: "", manifest: nil)

    var id: String
    var name: String
    var subtitle: String?
    /// The package's rev on the hub (any byte changes it); "" for the built-in.
    var rev: String
    /// Nil for the built-in.
    var manifest: PersonaManifest?

    var isBuiltin: Bool { manifest == nil }
    var palette: PersonaPalette? {
        manifest?.colors.flatMap { PersonaPalette(light: $0.light, dark: $0.dark) }
    }

    init(id: String, name: String, subtitle: String?, rev: String, manifest: PersonaManifest?) {
        self.id = id
        self.name = name
        self.subtitle = subtitle
        self.rev = rev
        self.manifest = manifest
    }

    init(info: PersonaInfo) {
        self.init(id: info.id, name: info.manifest.name, subtitle: info.manifest.subtitle, rev: info.rev, manifest: info.manifest)
    }
}

/// What the dot shows (docs/persona.md「状态」). Raw values are persona.json's.
nonisolated enum PersonaState: String, CaseIterable, Identifiable, Sendable {
    case idle, thinking, working
    case needsYou = "needs_you"
    case offline

    var id: String { rawValue }
    var title: String {
        switch self {
        case .idle: "空闲"
        case .thinking: "思考中"
        case .working: "工作中"
        case .needsYou: "等你处理"
        case .offline: "离线"
        }
    }
}

/// persona.json, format 1 (docs/persona.md「格式」).
nonisolated struct PersonaManifest: Codable, Sendable, Hashable {
    static let format = 1

    var format: Int
    var id: String
    var name: String
    var subtitle: String?
    var avatar: Avatar
    var dot: Dot
    var colors: Colors?

    struct Avatar: Codable, Sendable, Hashable {
        /// Side in pixels ("128") → file.
        var sizes: [String: String]
        /// The share of the circle what matters stays inside (1: the whole inscribed circle).
        var safe: Double?
    }

    struct Dot: Codable, Sendable, Hashable {
        /// The frames' side in pixels.
        var size: Int?
        /// State (raw value) → its frames.
        var states: [String: DotState]
    }

    struct DotState: Codable, Sendable, Hashable {
        /// An APNG (or a still PNG: one frame).
        var file: String
        var fps: Double?
    }

    /// "#RRGGBB" per token, per appearance.
    struct Colors: Codable, Sendable, Hashable {
        var light: [String: String]?
        var dark: [String: String]?
    }

    /// Every file it names (what a phone downloads).
    var files: [String] {
        Array(Set(avatar.sizes.values).union(dot.states.values.map(\.file))).sorted()
    }

    /// The avatar file for a face `points` wide at `scale`: the smallest at least that many pixels, else the largest.
    func avatarFile(points: Double, scale: Double) -> String? {
        let want = points * scale
        let sizes = avatar.sizes.compactMap { k, v in Double(k).map { ($0, v) } }.sorted { $0.0 < $1.0 }
        return (sizes.first { $0.0 >= want } ?? sizes.last)?.1
    }

    /// The state's frames, idle's for a state the package lacks.
    func dotState(_ s: PersonaState) -> DotState? { dot.states[s.rawValue] ?? dot.states[PersonaState.idle.rawValue] }

    /// What the hub checks too (server/internal/hub/persona.go), so a package it would leave out is refused here.
    var problem: String? {
        if format != Self.format { return "format \(format)" }
        if id == Persona.builtinID || id.range(of: "^[a-z0-9][a-z0-9-]{0,31}$", options: .regularExpression) == nil { return "id \(id)" }
        if name.trimmingCharacters(in: .whitespaces).isEmpty { return "no name" }
        if avatar.sizes.isEmpty { return "no avatar" }
        if dot.states[PersonaState.idle.rawValue] == nil { return "no idle dot" }
        for f in files where f.isEmpty || f.hasPrefix("/") || f.split(separator: "/").contains(where: { $0 == ".." || $0.hasPrefix(".") }) {
            return "file \(f)"
        }
        return nil
    }
}

/// One entry of GET /api/v1/personas.
nonisolated struct PersonaInfo: Codable, Sendable, Hashable {
    var id: String
    var rev: String
    var bytes: Int64
    var manifest: PersonaManifest
}
