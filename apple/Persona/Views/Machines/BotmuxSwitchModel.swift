import Foundation
import Observation

/// Also Show botmux Sessions under Engine (#2575): the hub keeps it per machine (hub.json's machines[].botmux is the default,
/// on when unset). `on` is nil until the hub answered, and stays nil on an older hub without the route: no switch.
@Observable final class BotmuxSwitchModel {
    struct Deps {
        var load: () async throws -> Bool
        var save: (Bool) async throws -> Bool
    }

    private(set) var on: Bool?
    private(set) var saving = false
    private(set) var error: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps) { self.deps = deps }

    func load() async {
        on = try? await deps.load()
    }

    /// Shows the new value at once; a failed save puts the old one back and says why.
    func set(_ next: Bool) async {
        guard !saving, let before = on else { return }
        saving = true
        error = nil
        on = next
        defer { saving = false }
        do {
            on = try await deps.save(next)
        } catch {
            on = before
            self.error = error.shown.map { String(localized: "Couldn't save: \($0) [Image Viewer fcc6eb]") }
        }
    }
}
