#if canImport(ActivityKit)
import ActivityKit
#endif
import Foundation
import OSLog
#if canImport(UIKit)
import UIKit
#endif

/// The app's half of the Fleet Live Activity (docs/live-activity.md). The hub decides what it shows and pushes it while
/// the app is away; the app
///   - reports the push-to-start token (so the hub can open an activity when the app is not running) and each
///     activity's own push token (it rotates), and says when an activity ended;
///   - while open, applies the hub's `fleet` events itself (faster than the push, and the only way on the simulator):
///     starts the activity when there is something to show, updates it, ends it after a minute of nothing.
@MainActor
final class LiveActivityController {
    static let shared = LiveActivityController()

    /// 设置 › 通知 › 实时活动. Mirrored from the hub prefs so the app can decide without asking.
    static let enabledKey = "liveActivity.enabled"
    static let detailsKey = "liveActivity.lockDetails"
    static let islandKey = "liveActivity.island"
    var enabled: Bool { UserDefaults.standard.object(forKey: Self.enabledKey) as? Bool ?? true }
    var lockDetails: Bool { UserDefaults.standard.bool(forKey: Self.detailsKey) }
    var island: IslandPrefs {
        UserDefaults.standard.data(forKey: Self.islandKey).flatMap { try? JSONDecoder().decode(IslandPrefs.self, from: $0) } ?? IslandPrefs()
    }

    private var watching: Set<String> = []
    private var endTask: Task<Void, Never>?
    private var started = false

    private var activity: Activity<JarvisActivityAttributes>? {
        Activity<JarvisActivityAttributes>.activities.first { $0.activityState == .active || $0.activityState == .stale }
    }

    /// The device's settings changed (or were loaded from the hub): remember them, and apply at once — off ends the
    /// activity, the lock screen details and the island's sides change on the running one, on starts one from the hub's
    /// current fleet.
    func settings(_ prefs: NotifyPrefs) {
        let was = (enabled, lockDetails, island)
        UserDefaults.standard.set(prefs.liveActivity, forKey: Self.enabledKey)
        UserDefaults.standard.set(prefs.lockDetails, forKey: Self.detailsKey)
        UserDefaults.standard.set(try? JSONEncoder().encode(prefs.island), forKey: Self.islandKey)
        guard was != (prefs.liveActivity, prefs.lockDetails, prefs.island) else { return }
        if !prefs.liveActivity {
            endAll()
            return
        }
        if let activity {
            // At once from what is showing (another machine's load comes with the hub's fleet below).
            let state = activity.content.state.tailored(island: prefs.island, details: prefs.lockDetails)
            Self.update(activity, ActivityContent(state: state, staleDate: activity.content.staleDate, relevanceScore: Self.relevance(state)))
        }
        if let api = AppModel.shared.api {
            Task { if let fleet = try? await api.fleet() { apply(fleet) } }
        }
    }

    /// Once per launch: watch tokens and activities the hub push-started.
    func start() {
        guard !started else { return }
        started = true
        Task {
            for await token in Activity<JarvisActivityAttributes>.pushToStartTokenUpdates {
                report(.init(pushToStartToken: token.hex))
            }
        }
        Task {
            for await activity in Activity<JarvisActivityAttributes>.activityUpdates { watch(activity) }
        }
        for activity in Activity<JarvisActivityAttributes>.activities { watch(activity) }
    }

    /// The hub's current fleet (the `fleet` event, or GET /api/v1/fleet on becoming active).
    func apply(_ hubState: FleetState) {
        let state = hubState.tailored(island: island, details: lockDetails)
        guard enabled, ActivityAuthorizationInfo().areActivitiesEnabled else {
            endAll()
            return
        }
        if state.isEmpty {
            // Agents flicker between turns: end only when nothing came back for a minute (the hub does the same).
            guard endTask == nil, activity != nil else { return }
            endTask = Task { [weak self] in
                try? await Task.sleep(for: .seconds(60))
                guard !Task.isCancelled else { return }
                self?.endAll()
                self?.endTask = nil
            }
            return
        }
        endTask?.cancel()
        endTask = nil
        let content = ActivityContent(state: state, staleDate: .now.addingTimeInterval(15 * 60), relevanceScore: Self.relevance(state))
        if let activity {
            Self.update(activity, content)
        } else if UIApplication.shared.applicationState == .active, state.phase != .done {
            request(content)
        }
    }

    /// Takes a settled request off the activity at once (the hub's update follows).
    func resolvedLocally(_ interactionID: String) {
        guard let activity else { return }
        var state = activity.content.state
        guard let i = state.items.firstIndex(where: { $0.interaction == interactionID }) else { return }
        state.items.remove(at: i)
        state.needsYou = max(0, state.needsYou - 1)
        if state.needsYou == 0 { state.phase = state.working > 0 ? .working : (state.unread > 0 ? .done : .none) }
        Self.update(activity, ActivityContent(state: state, staleDate: activity.content.staleDate))
    }

    /// The persona and its color changed (or the app started): saved for the widget extension, and the running
    /// activity is redrawn with it now rather than at the hub's next push.
    func look(_ look: FleetLook) {
        guard FleetLook.save(look), let activity else { return }
        Self.update(activity, activity.content)
    }

    func endAll() {
        endTask?.cancel()
        endTask = nil
        for activity in Activity<JarvisActivityAttributes>.activities {
            nonisolated(unsafe) let a = activity
            Task { await a.end(nil, dismissalPolicy: .immediate) }
        }
    }

    // Activity is not Sendable, but its methods are safe from any thread (ActivityKit does its own locking).
    private static func update(_ activity: Activity<JarvisActivityAttributes>, _ content: ActivityContent<FleetState>) {
        nonisolated(unsafe) let a = activity
        Task { await a.update(content) }
    }

    static func relevance(_ s: FleetState) -> Double {
        switch s.phase {
        case .needsYou: 1
        case .working: 0.5
        default: 0.2
        }
    }

    private func request(_ content: ActivityContent<FleetState>) {
        do {
            watch(try Activity.request(attributes: JarvisActivityAttributes(), content: content, pushType: .token))
        } catch {
            // The simulator without APNs, or the user turned Live Activities off for Jarvis.
            if let local = try? Activity.request(attributes: JarvisActivityAttributes(), content: content, pushType: nil) {
                watch(local)
            }
        }
    }

    private func watch(_ activity: Activity<JarvisActivityAttributes>) {
        guard watching.insert(activity.id).inserted else { return }
        let id = activity.id
        Task {
            for await token in activity.pushTokenUpdates {
                #if DEBUG
                // For pushing to the simulator by hand (docs/testing.md, TestManualLiveActivityPush).
                Logger(subsystem: "jarvis", category: "live-activity").notice("activity push token \(token.hex, privacy: .public)")
                #endif
                report(.init(activityID: id, pushToken: token.hex))
            }
        }
        Task {
            for await state in activity.activityStateUpdates where state == .ended || state == .dismissed {
                report(.init(activityID: id, ended: true))
                watching.remove(id)
            }
        }
    }

    private func report(_ body: APIClient.LiveActivityReport) {
        #if DEBUG
        if UserDefaults.standard.string(forKey: "pairURL") != nil { return } // simulator automation is not this phone
        #endif
        guard let api = AppModel.shared.api else { return }
        Task { try? await api.reportLiveActivity(device: PushRegistrar.deviceID, body) }
    }
}

#if DEBUG
extension LiveActivityController {
    /// `-showFleetSample needsYou|working|done|thinking|offline` (`-fleetDetails YES` for names on the lock screen,
    /// `-fleetIsland b`, `c` or `custom:assistant,load` for the island's sides, `-showFleetCount 12` for that many in
    /// each kind there is; `-openPersona jarvis-circuit` for the persona): starts the activity with made-up agents, no
    /// hub needed, for screenshots. `thinking` is only the assistant answering; `offline` is the working sample made
    /// stale once it shows.
    func showSample(_ phase: String, details: Bool, island spec: String?, count: Int = 0) {
        UserDefaults.standard.set(details, forKey: Self.detailsKey)
        var island = IslandPrefs()
        if let spec {
            let parts = spec.split(separator: ":", maxSplits: 1)
            island.preset = IslandPrefs.Preset(rawValue: String(parts[0])) ?? .a
            if parts.count > 1 {
                let sides = parts[1].split(separator: ",").map { IslandSlot(rawValue: String($0)) }
                island.left = sides.first ?? nil
                island.right = sides.count > 1 ? sides[1] : nil
            }
        }
        UserDefaults.standard.set(try? JSONEncoder().encode(island), forKey: Self.islandKey)
        let now = Date.now.timeIntervalSince1970
        let ask = FleetState.Item(id: "macbook/s1", title: "补齐网络层的单元测试", place: "MacBook · weather", kind: .permission,
                                  detail: "Bash: swift test --parallel", interaction: "ix_sample", since: now - 90)
        let plan = FleetState.Item(id: "studio/s2", title: "照片同步支持 HEIC", place: "Studio · photo-sync", kind: .plan,
                                   detail: "支持 HEIC", interaction: "ix_plan", since: now - 300)
        let work = FleetState.Item(id: "studio/s3", title: "重构设置页", place: "Studio · notes", kind: .working, since: now - 754)
        let done = FleetState.Item(id: "macbook/s4", title: "升级依赖", place: "MacBook · api", kind: .unread, detail: "全部测试通过，已提交", since: now - 60)
        var state: FleetState = switch phase {
        case "working": FleetState(phase: .working, working: 3, unread: 1, items: [work, done], workingSince: now - 754)
        case "done", "thinking": FleetState(phase: .done, unread: 1, items: [done])
        default: FleetState(phase: .needsYou, needsYou: 2, working: 3, unread: 1, items: [ask, plan, work], workingSince: now - 754)
        }
        if count > 0 {
            state.needsYou = state.needsYou > 0 ? count : 0
            state.working = state.working > 0 ? count : 0
            state.unread = state.unread > 0 ? count : 0
        }
        state.assistant = phase != "done"
        if phase == "offline" { state = FleetState(phase: .working, working: 3, unread: 1, items: [work, done], workingSince: now - 754) }
        state.stats = .init(tpm: 12000, tokens: 42_000_000, requests: 1200,
                            loads: [.init(id: "studio", name: "Studio", percent: 35), .init(id: "macbook", name: "MacBook", percent: 70)])
        Task {
            try? await Task.sleep(for: .seconds(1)) // the scene must be active to start one
            // `apply` starts no activity for finished agents alone (the hub does not either) and keeps it fresh: these
            // start one directly, and `offline` is then marked stale.
            if ["done", "thinking", "offline"].contains(phase) {
                endAll()
                try? await Task.sleep(for: .seconds(1))
                let content = ActivityContent(state: state.tailored(island: island, details: details), staleDate: .now.addingTimeInterval(15 * 60))
                request(content)
                if phase == "offline", let activity {
                    // A request already stale is refused, and the simulator does not redraw when a stale date passes:
                    // an update dated in the past draws it stale at once.
                    try? await Task.sleep(for: .seconds(1))
                    Self.update(activity, ActivityContent(state: content.state, staleDate: .now.addingTimeInterval(-1)))
                }
                return
            }
            apply(state)
        }
    }
}
#endif

extension AppModel {
    /// What the Live Activity draws (docs/live-activity.md「形象」): a built-in in the accent it has in the app; a
    /// package (its frames are bitmaps, which the activity does not play) as the default face in its own color.
    var fleetLook: FleetLook {
        let p = persona
        if p.isBuiltin {
            return FleetLook(design: p.avatarDesign == .circuit ? .circuit : .visor, tint: builtinAccent)
        }
        return FleetLook(design: .visor, tint: Theme.persona)
    }
}

extension Data {
    nonisolated var hex: String { map { String(format: "%02x", $0) }.joined() }
}
