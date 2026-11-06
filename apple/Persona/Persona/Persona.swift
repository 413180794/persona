import Foundation

/// A look for Persona (docs/persona.md), the one Avatar setting: its name, its face, the animated dot in the Persona
/// tab's top bar, its colors and the home screen icon (`appIcon`). Built-ins and packages are the same model. The built-in ones are the app's own drawings (JarvisAvatar: Mochi in the icon's colors, the others in the
/// style's accent); every other one is a
/// package on the hub (~/.jarvis/personas/<id>/) that the phone downloads and keeps (`PersonaStore`).
nonisolated struct Persona: Identifiable, Sendable, Hashable {
    /// The default's id. Until 2026-11-03 the default was Visor under "jarvis"; a phone that never chose has no id
    /// saved and gets Mochi, one that chose Visor has "jarvis" saved and keeps it (`PersonaStore.chosenID`).
    static let builtinID = "persona"
    /// The default: Mochi, the app icon's character, in the icon's colors.
    static let builtin = Persona(id: builtinID, name: "Persona", subtitle: String(localized: "The waving blob from the app icon"), rev: "", manifest: nil,
                                 design: "mochi", title: String(localized: "Mochi"))
    /// Visor, the default until 2026-11-03, now a choice; still named Persona, in the accent.
    static let visor = Persona(id: "jarvis", name: "Persona", subtitle: String(localized: "Visor, in the theme color"), rev: "", manifest: nil,
                               design: "visor", title: String(localized: "Visor"))
    /// Circuit (three traces running into a core with two eyes), a choice next to the default; still named Persona.
    static let circuit = Persona(id: "jarvis-circuit", name: "Persona", subtitle: String(localized: "Circuit, in the theme color"), rev: "", manifest: nil,
                                 design: "circuit", title: String(localized: "Circuit"))
    /// Pixel, Facet and Jelly (#350, from the style candidates the user picked): after Circuit, in the accent, still named Persona.
    static let pixel = Persona(id: "jarvis-pixel", name: "Persona", subtitle: String(localized: "Pixel, in the theme color"), rev: "", manifest: nil,
                               design: "pixel", title: String(localized: "Pixel"))
    static let facet = Persona(id: "jarvis-facet", name: "Persona", subtitle: String(localized: "Facet, in the theme color"), rev: "", manifest: nil,
                               design: "facet", title: String(localized: "Facet"))
    static let blob = Persona(id: "jarvis-blob", name: "Persona", subtitle: String(localized: "Jelly, in the theme color"), rev: "", manifest: nil,
                              design: "blob", title: String(localized: "Jelly"))
    /// What Settings › Appearance › Avatar offers before the hub's packages, the default first.
    static let builtins = [builtin, visor, circuit, pixel, facet, blob]

    var id: String
    var name: String
    var subtitle: String?
    /// The package's rev on the hub (any byte changes it); "" for a built-in.
    var rev: String
    /// Nil for a built-in.
    var manifest: PersonaManifest?
    /// A built-in's JarvisAvatar.Design (raw value); nil: the default's.
    var design: String?
    /// The card's name in Settings when it is not `name` (the built-ins are all called Persona).
    var title: String?

    var isBuiltin: Bool { manifest == nil }
    var cardTitle: String { title ?? name }
    /// A built-in's line under its card in Settings: whose colors it is drawn in.
    var builtinNote: String { design == PersonaFace.Design.mochi.rawValue ? String(localized: "The app icon's colors") : String(localized: "Follows the theme color") }
    var palette: PersonaPalette? {
        manifest?.colors.flatMap { PersonaPalette(light: $0.light, dark: $0.dark) }
    }
    /// The home screen icon (docs/persona.md「App 图标」): a built-in's own; a package's `app_icon_fallback`, the
    /// built-in whose icon it takes, as iOS switches only to icons the app ships; else the default's.
    var appIcon: AppIconChoice { AppIconChoice(lookID: isBuiltin ? id : manifest?.appIconFallback) }

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
    /// A few words on the look's card in Settings › Appearance › Avatar, in place of its colors (Milk Frog: where its level comes from).
    var about: String?
    var avatar: Avatar
    var dot: Dot
    var colors: Colors?
    /// The built-in Avatar (its id) whose home screen icon this one puts on: the package's own pictures cannot be one.
    var appIconFallback: String?

    enum CodingKeys: String, CodingKey {
        case format, id, name, subtitle, about, avatar, dot, colors
        case appIconFallback = "app_icon_fallback"
    }

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
