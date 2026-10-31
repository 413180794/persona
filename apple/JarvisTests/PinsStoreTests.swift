import Foundation
import Testing
@testable import Jarvis

/// Pins and mutes (`PinsStore`): kept across launches, changed at once and sent to the hub, the mute rule.
@MainActor struct PinsStoreTests {
    private func store(_ hub: FakeHub, _ defaults: UserDefaults, errors: @escaping (Error) -> Void = { _ in }) -> PinsStore {
        PinsStore(deps: .init(api: { hub.client }, report: errors, defaults: defaults))
    }

    private func defaults() -> UserDefaults {
        let name = "PinsStoreTests-\(UUID().uuidString)"
        let d = UserDefaults(suiteName: name)!
        d.removePersistentDomain(forName: name)
        return d
    }

    private func settle(_ hub: FakeHub, _ path: String) async {
        for _ in 0..<50 where hub.sent("PUT", path).isEmpty { try? await Task.sleep(for: .milliseconds(10)) }
    }

    @Test func pinningShowsAtOnceSendsAndIsRemembered() async {
        let hub = FakeHub()
        hub.on("PUT", "/api/v1/pins", json: #"{"spaces":[],"sessions":["m/s1"]}"#)
        let d = defaults()
        let pins = store(hub, d)
        pins.togglePin(session: "m/s1")
        #expect(pins.isPinned(session: "m/s1"), "before the hub answers")
        await settle(hub, "/api/v1/pins")
        #expect(hub.sent("PUT", "/api/v1/pins").count == 1)
        #expect(store(hub, d).isPinned(session: "m/s1"), "a new launch reads it back")
        pins.togglePin(session: "m/s1")
        #expect(!pins.isPinned(session: "m/s1"))
    }

    @Test func aMutedSpaceMutesItsAgentsUnlessOneIsTurnedBackOn() {
        let hub = FakeHub()
        hub.on("PUT", "/api/v1/mutes", json: "{}")
        let pins = store(hub, defaults())
        pins.toggleMute(space: "m/app")
        #expect(pins.isMuted(session: "m/s1", space: "m/app"))
        pins.toggleMute(session: "m/s1", space: "m/app")
        #expect(!pins.isMuted(session: "m/s1", space: "m/app"), "allowed inside the muted space")
        #expect(pins.isMuted(session: "m/s2", space: "m/app"))
    }

    @Test func aFailedSaveIsReported() async {
        let hub = FakeHub()
        hub.on("PUT", "/api/v1/pins") { _ in throw APIError(status: 500, message: "nope") }
        var errors = 0
        let pins = store(hub, defaults()) { _ in errors += 1 }
        pins.togglePin(space: "m/app")
        for _ in 0..<50 where errors == 0 { try? await Task.sleep(for: .milliseconds(10)) }
        #expect(errors == 1)
    }

    @Test func aRenamedSpaceKeepsItsPinAndMute() {
        let hub = FakeHub()
        hub.on("PUT", "/api/v1/pins", json: "{}")
        hub.on("PUT", "/api/v1/mutes", json: "{}")
        let pins = store(hub, defaults())
        pins.togglePin(space: "m/old")
        pins.toggleMute(space: "m/old")
        pins.renameSpace("m/old", to: "m/new")
        #expect(pins.isPinned(space: "m/new") && pins.isMuted(space: "m/new"))
        #expect(!pins.isPinned(space: "m/old"))
    }
}
