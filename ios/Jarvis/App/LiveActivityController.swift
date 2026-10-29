import ActivityKit
import Foundation
import OSLog
import UIKit

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
    var enabled: Bool { UserDefaults.standard.object(forKey: Self.enabledKey) as? Bool ?? true }
    var lockDetails: Bool { UserDefaults.standard.bool(forKey: Self.detailsKey) }

    private var watching: Set<String> = []
    private var endTask: Task<Void, Never>?
    private var started = false

    private var activity: Activity<JarvisActivityAttributes>? {
        Activity<JarvisActivityAttributes>.activities.first { $0.activityState == .active || $0.activityState == .stale }
    }

    /// The device's settings changed (or were loaded from the hub): remember them, and apply at once — off ends the
    /// activity, the lock screen details flip on the running one, on starts one from the hub's current fleet.
    func settings(enabled: Bool, lockDetails: Bool) {
        let was = (self.enabled, self.lockDetails)
        UserDefaults.standard.set(enabled, forKey: Self.enabledKey)
        UserDefaults.standard.set(lockDetails, forKey: Self.detailsKey)
        guard was != (enabled, lockDetails) else { return }
        if !enabled {
            endAll()
        } else if let activity {
            var state = activity.content.state
            state.details = lockDetails
            Self.update(activity, ActivityContent(state: state, staleDate: activity.content.staleDate, relevanceScore: Self.relevance(state)))
        } else if let api = AppModel.shared.api {
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
        var state = hubState
        state.details = lockDetails
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
    /// `-showFleetSample needsYou|working|done` (`-fleetDetails YES` for names on the lock screen): starts the activity
    /// with made-up agents, no hub needed, for screenshots.
    func showSample(_ phase: String, details: Bool) {
        UserDefaults.standard.set(details, forKey: Self.detailsKey)
        let now = Date.now.timeIntervalSince1970
        let ask = FleetState.Item(id: "macbook/s1", title: "补齐网络层的单元测试", place: "MacBook · weather", kind: .permission,
                                  detail: "Bash: swift test --parallel", interaction: "ix_sample", since: now - 90)
        let plan = FleetState.Item(id: "studio/s2", title: "照片同步支持 HEIC", place: "Studio · photo-sync", kind: .plan,
                                   detail: "支持 HEIC", interaction: "ix_plan", since: now - 300)
        let work = FleetState.Item(id: "studio/s3", title: "重构设置页", place: "Studio · notes", kind: .working, since: now - 754)
        let done = FleetState.Item(id: "macbook/s4", title: "升级依赖", place: "MacBook · api", kind: .unread, detail: "全部测试通过，已提交", since: now - 60)
        let state: FleetState = switch phase {
        case "working": FleetState(phase: .working, working: 3, unread: 1, items: [work, done], workingSince: now - 754)
        case "done": FleetState(phase: .done, unread: 1, items: [done])
        default: FleetState(phase: .needsYou, needsYou: 2, working: 3, unread: 1, items: [ask, plan, work], workingSince: now - 754)
        }
        Task {
            try? await Task.sleep(for: .seconds(1)) // the scene must be active to start one
            apply(state)
        }
    }
}
#endif

extension Data {
    nonisolated var hex: String { map { String(format: "%02x", $0) }.joined() }
}
