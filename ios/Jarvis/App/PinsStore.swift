import SwiftUI

/// Pinned and muted spaces / agents. The hub keeps them; this keeps a copy in UserDefaults so the list orders right
/// before the hub answers, and changes them optimistically. Keys are "machine/space name" and "machine/session id"
/// (`AppModel.pinKey`). Split out of AppModel (ARCH-PLAN step 9); AppModel forwards to it.
@Observable final class PinsStore {
    struct Deps {
        var api: () -> APIClient?
        var report: (Error) -> Void
        var defaults: UserDefaults = .standard
    }

    var pins: Pins { didSet { save(pins, "dev.pins") } }
    /// Muted: the hub sends no notifications about them and the list shows them quietly.
    var mutes: Pins { didSet { save(mutes, "dev.mutes") } }
    @ObservationIgnored private let deps: Deps

    init(deps: Deps) {
        self.deps = deps
        pins = Self.load("dev.pins", deps.defaults)
        mutes = Self.load("dev.mutes", deps.defaults)
    }

    private static func load(_ key: String, _ defaults: UserDefaults) -> Pins {
        guard let data = defaults.data(forKey: key), let p = try? JSON.decoder.decode(Pins.self, from: data) else { return Pins() }
        return p
    }

    private func save(_ p: Pins, _ key: String) {
        if let data = try? JSON.encoder.encode(p) { deps.defaults.set(data, forKey: key) }
    }

    func isPinned(space: String) -> Bool { pins.spaces.contains(space) }
    func isPinned(session: String) -> Bool { pins.sessions.contains(session) }
    func isMuted(space: String) -> Bool { mutes.spaces.contains(space) }
    /// The agent itself, or its space unless the agent was turned back on there (the hub's rule).
    func isMuted(session: String, space: String?) -> Bool { mutes.mutes(session: session, space: space) }

    func togglePin(space: String) { updatePins { Self.toggle(&$0.spaces, space) } }
    func togglePin(session: String) { updatePins { Self.toggle(&$0.sessions, session) } }
    func toggleMute(space: String) { updateMutes { Self.toggle(&$0.spaces, space) } }
    /// One agent's own switch, on top of its space's: muting adds it to the muted agents; turning it on inside a
    /// muted space adds it to the allowed ones.
    func toggleMute(session: String, space: String?) { updateMutes { $0 = $0.togglingMute(session: session, space: space) } }

    /// A space was renamed: its pin and mute follow the new name.
    func renameSpace(_ old: String, to new: String) {
        pins.spaces = pins.spaces.map { $0 == old ? new : $0 }
        mutes.spaces = mutes.spaces.map { $0 == old ? new : $0 }
    }

    func load() async {
        if let p = try? await deps.api()?.pins() { pins = p }
        if let m = try? await deps.api()?.mutes() { mutes = m }
    }

    private static func toggle(_ list: inout [String], _ key: String) {
        if let i = list.firstIndex(of: key) { list.remove(at: i) } else { list.insert(key, at: 0) }
    }

    private func updatePins(_ change: (inout Pins) -> Void) {
        Haptics.tap()
        var p = pins
        change(&p)
        withAnimation(Theme.Motion.standard) { pins = p } // optimistic; the hub keeps the truth
        let api = deps.api, report = deps.report
        Task { do { try await api()?.setPins(p) } catch { report(error) } }
    }

    private func updateMutes(_ change: (inout Pins) -> Void) {
        Haptics.tap()
        var p = mutes
        change(&p)
        withAnimation(Theme.Motion.standard) { mutes = p }
        let api = deps.api, report = deps.report
        Task { do { try await api()?.setMutes(p) } catch { report(error) } }
    }
}
