import Foundation

/// A look for Persona (docs/persona.md): its name, its face, the animated dot in the Persona tab's top bar and its
/// colors. The built-in ones are the app's own drawings (JarvisAvatar, in the style's accent); every other one is a
/// package on the hub (~/.jarvis/personas/<id>/) that the phone downloads and keeps (`PersonaStore`).
nonisolated struct Persona: Identifiable, Sendable, Hashable {
    static let builtinID = "jarvis"
    /// The default: 面罩.
    static let builtin = Persona(id: builtinID, name: "Persona", subtitle: "默认形象，颜色随主题色", rev: "", manifest: nil)
    /// 汇流 (three traces running into a core with two eyes), a choice next to the default; still named Persona.
    static let circuit = Persona(id: "jarvis-circuit", name: "Persona", subtitle: "汇流，颜色随主题色", rev: "", manifest: nil,
                                 design: "circuit", title: "汇流")
    /// What 设置 › 外观 › 形象 offers before the hub's packages, the default first.
    static let builtins = [builtin, circuit]

    var id: String
    var name: String
    var subtitle: String?
    /// The package's rev on the hub (any byte changes it); "" for a built-in.
    var rev: String
    /// Nil for a built-in.
    var manifest: PersonaManifest?
    /// A built-in's JarvisAvatar.Design (raw value); nil: the default's.
    var design: String?
    /// The card's name in 设置 when it is not `name` (the built-ins are all called Persona).
    var title: String?

    var isBuiltin: Bool { manifest == nil }
    var cardTitle: String { title ?? name }
    var palette: PersonaPalette? {
        manifest?.colors.flatMap { PersonaPalette(light: $0.light, dark: $0.dark) }
    }

    init(id: String, name: String, subtitle: String?, rev: String, manifest: PersonaManifest?, design: String? = nil,
         title: String? = nil) {
        self.id = id
        self.name = name
        self.subtitle = subtitle
        self.rev = rev
        self.manifest = manifest
        self.design = design
        self.title = title
    }

    init(info: PersonaInfo) {
        self.init(id: info.id, name: info.manifest.name, subtitle: info.manifest.subtitle, rev: info.rev, manifest: info.manifest)
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
        if Persona.builtins.contains(where: { $0.id == id }) || id.range(of: "^[a-z0-9][a-z0-9-]{0,31}$", options: .regularExpression) == nil { return "id \(id)" }
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
