import Foundation
import Observation

// Mac versions of the iOS app's system integrations that have no macOS counterpart (docs/mac.md「Mac 上没有的」).
// Same names and calls as on iOS, so AppModel and the pages stay as they are.

/// Live Activities / the Dynamic Island do not exist on a Mac. The hub's fleet state is kept here for the
/// menu bar (docs/mac.md: 灵动岛 → 菜单栏); nothing is started or ended.
@Observable final class LiveActivityController {
    static let shared = LiveActivityController()

    /// The hub's latest fleet summary (what the island would show).
    private(set) var fleet: FleetState?
    /// The device's notification settings, as the hub keeps them.
    private(set) var prefs: NotifyPrefs?
    /// Interactions settled from this Mac, hidden until the hub's next fleet push agrees.
    private var resolved: Set<String> = []

    func start() {}
    func settings(_ prefs: NotifyPrefs) { self.prefs = prefs }
    func apply(_ hubState: FleetState) {
        fleet = hubState
        resolved = []
    }
    func resolvedLocally(_ interactionID: String) { resolved.insert(interactionID) }
    func isResolvedLocally(_ interactionID: String) -> Bool { resolved.contains(interactionID) }
    func endAll() { fleet = nil }
    func showSample(_ phase: String, details: Bool, island spec: String?) {}
}

/// The Mac's id at the hub (its notification settings hang off it). Remote push to the Mac needs an APNs
/// entitlement the target does not have yet, so nothing is registered: the Mac keeps its WebSocket open while
/// it runs, and the hub reaches it that way (docs/mac.md).
final class PushRegistrar {
    static let shared = PushRegistrar()

    func registerIfPossible() {}

    static var deviceID: String {
        if let id = UserDefaults.standard.string(forKey: "deviceID") { return id }
        let id = UUID().uuidString
        UserDefaults.standard.set(id, forKey: "deviceID")
        return id
    }
}

/// There is no Apple Watch paired to a Mac: the pairing and summary pushes go nowhere.
final class WatchSync: NSObject {
    static let shared = WatchSync()

    var onConfig: ((ServerConfig) -> Void)?
    var onSummary: ((WatchSummary) -> Void)?
    var onActivated: (() -> Void)?
    var configProvider: (() -> ServerConfig?)?

    func activate() {}
    func push(config: ServerConfig) {}
    func push(summary: @autoclosure () -> WatchSummary) {}
}
