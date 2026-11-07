import Foundation
import SwiftUI
import Testing
@testable import Persona

/// The two-persona product policy, run against both native apps. Legacy preferences remain available to older builds.
@MainActor struct PersonaSelectionTests {
    private nonisolated static let packID = "nwverse"

    nonisolated static let removed = ["persona-visor", "persona-circuit", "persona-pixel", "persona-facet", "persona-blob", "spark", "halo", "jarvis-visor", "jarvis-circuit", "unknown"]

    private final class Icons: AppIconSetting {
        var supportsAlternateIcons = true
        var alternateIconName: String?
        var calls: [String?] = []
        init(_ name: String?) { alternateIconName = name }
        func setAlternateIconName(_ name: String?) async throws {
            calls.append(name)
            alternateIconName = name
        }
    }

    private func info(_ id: String, rev: String = "1") -> PersonaInfo {
        let manifest = PersonaManifest(format: 1, id: id, name: "奶蛙", avatar: .init(sizes: ["128": "avatar.png"]),
                                       dot: .init(states: ["idle": .init(file: "idle.png")]))
        return PersonaInfo(id: id, rev: rev, bytes: 1, manifest: manifest)
    }

    @Test(arguments: removed) func legacySelectionFallsBackWithoutErasingTheOldPreference(_ id: String) async throws {
        let suite = "persona-selection-\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        let root = FileManager.default.temporaryDirectory.appending(path: suite)
        defer {
            defaults.removePersistentDomain(forName: suite)
            try? FileManager.default.removeItem(at: root)
        }
        defaults.set(id, forKey: PersonaStore.chosenKey)
        let store = PersonaStore(root: root, defaults: defaults)
        #expect(store.chosenID == "persona" && store.current.id == "persona")
        #expect(store.current.avatarDesign == .mochi && store.appIcon == .standard)
        #expect(AppearanceChoice.resolve(style: nil, color: nil, persona: id).color == .style)
        #expect(defaults.string(forKey: PersonaStore.chosenKey) == id)

        let icons = Icons("AppIcon-Visor")
        let model = AppIconModel(system: icons)
        await model.follow(store.appIcon, scheme: .system)
        await model.follow(store.appIcon, scheme: .system)
        #expect(icons.calls.count == 1 && icons.calls[0] == nil)
        #expect(icons.alternateIconName == nil && model.current == .standard)
    }

    @Test(arguments: ["AppIcon-Visor", "AppIcon-Circuit", "AppIcon-Pixel", "AppIcon-Facet", "AppIcon-Blob"])
    func removedAlternateIconIsExplicitlyReset(_ name: String) async {
        let icons = Icons(name)
        let model = AppIconModel(system: icons)
        await model.follow(nil, scheme: .system)
        #expect(icons.calls.count == 1 && icons.calls[0] == nil)
        #expect(icons.alternateIconName == nil)
    }

    @Test func cachedAndHubListsOnlyOfferMochiAndMilkFrog() async throws {
        let suite = "persona-selection-\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        let root = FileManager.default.temporaryDirectory.appending(path: suite)
        defer {
            defaults.removePersistentDomain(forName: suite)
            try? FileManager.default.removeItem(at: root)
        }
        let entries = [info("persona-visor"), info("other"), info("nwverse")]
        try FileManager.default.createDirectory(at: root.appending(path: "persona-visor/1"), withIntermediateDirectories: true)
        try JSONEncoder().encode(entries).write(to: root.appending(path: "index.json"))
        defaults.set("persona-visor", forKey: PersonaStore.chosenKey)
        let store = PersonaStore(root: root, defaults: defaults)
        #expect(store.offered.map(\.id) == ["persona", "nwverse"])
        #expect(store.current.id == "persona")

        let hub = FakeHub()
        let data = try JSONEncoder().encode(entries)
        hub.on("GET", "/api/v1/personas", json: "{\"personas\":\(String(decoding: data, as: UTF8.self))}")
        hub.on("GET", "/api/v1/personas/\(Self.packID)/avatar.png", json: "avatar")
        hub.on("GET", "/api/v1/personas/\(Self.packID)/idle.png", json: "idle")
        await store.refresh(hub.client)
        #expect(store.offered.map(\.id) == ["persona", "nwverse"])
        await store.choose(try #require(store.offered.last), api: hub.client)
        #expect(store.current.id == "nwverse" && store.appIcon == .standard)
        #expect(PersonaStore(root: root, defaults: defaults).current.id == "nwverse")

        await store.choose(Persona(info: info("persona-visor")), api: hub.client)
        #expect(store.current.id == "persona" && store.chosenID == "persona")
        #expect(hub.sent("GET", "/api/v1/personas/persona-visor/avatar.png").isEmpty)
    }

    @Test func retainedIDsAndDrawingVocabulary() {
        #expect(PersonaSelection.resolve(nil) == "persona")
        #expect(PersonaSelection.resolve("persona") == "persona")
        #expect(PersonaSelection.resolve("nwverse") == "nwverse")
        #expect(Persona.builtins.map(\.id) == ["persona"])
        #expect(AssistantAvatar.Design.allCases.map(\.rawValue) == ["mochi"])
        #expect(ExtensionBuiltinLook.all == ["persona"])
        #expect(AppearanceChoice.resolve(style: nil, color: nil, persona: "nwverse").color == .persona)
    }
}
