#if canImport(ActivityKit)
import ActivityKit
#endif
import Foundation
import OSLog
import Security
import SwiftUI
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
            let state = activity.content.state.tailored(island: prefs.island, details: prefs.lockDetails, look: AppModel.shared.fleetLook)
            Self.update(activity, ActivityContent(state: state, staleDate: activity.content.staleDate, relevanceScore: Self.relevance(state)))
        }
        // Every group's fleet, merged (docs/multi-hub.md「实时活动」).
        let model = AppModel.shared
        Task {
            var any = false
            for link in model.links {
                if let fleet = try? await link.api.fleet() { link.fleet = fleet; any = true }
            }
            if any { apply(model.mergedFleet()) }
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
        let state = hubState.tailored(island: island, details: lockDetails, look: AppModel.shared.fleetLook)
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
            guard Self.worthUpdating(activity.content.state, staleDate: activity.content.staleDate, to: state, now: .now) else { return }
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

    /// The persona and its color changed, or the app started or came back (what failed is tried again). It goes three
    /// ways (docs/live-activity.md「形象」):
    ///   - into the running activity's content now, which also makes the system draw it again;
    ///   - to the hub, which puts it in this device's pushes (`look` in the device's prefs);
    ///   - into the App Group container shared with the widget extension, with a package's pictures (`FleetLook.save`).
    /// `force` (灵动岛诊断): written anew everywhere and redrawn even when nothing changed.
    func look(_ look: FleetLook, force: Bool = false, faces: () -> FleetFaces?) {
        _ = Self.forgetKeychainLook
        if force {
            _ = FleetLookStore.shared.write(nil, FleetLookStore.lookFile)
            _ = FleetLookStore.shared.save(nil)
            toldHub = nil
        }
        let saved = FleetLook.save(look, faces: faces)
        tellHub(look)
        guard let activity else { return }
        let content = activity.content
        guard content.state.look != look || saved || force else { return }
        var state = content.state
        state.look = look
        // A new stale date too: an update identical to what is showing may draw nothing.
        let stale = (content.staleDate ?? .now.addingTimeInterval(15 * 60)).addingTimeInterval(1)
        Self.update(activity, ActivityContent(state: state, staleDate: stale, relevanceScore: content.relevanceScore))
    }

    /// The look in the running activity's content: nil without an activity (灵动岛诊断).
    var runningLook: FleetLook?? { activity.map { $0.content.state.look } }

    /// The look the hub has for this device (nil: not told since launch).
    private var toldHub: FleetLook?
    private var telling: Task<Void, Never>?

    private func tellHub(_ look: FleetLook) {
        guard toldHub != look, let api = AppModel.shared.api, !api.config.isDemo else { return }
        telling?.cancel()
        telling = Task { [weak self] in
            do {
                try await api.setFleetLook(device: PushRegistrar.deviceID, look)
                guard !Task.isCancelled else { return }
                self?.toldHub = look
            } catch {
                FleetLookStore.log.error("telling the hub the look: \(error.localizedDescription, privacy: .public)")
            }
        }
    }

    /// Builds 92–103 left the look and the package's pictures in a keychain group the extension could not read on a
    /// phone; keychain items outlive the app, so they go once a launch (the group stays in the app's entitlements
    /// for this).
    private static let forgetKeychainLook: Void = {
        let status = SecItemDelete([kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: "jarvis.look"] as CFDictionary)
        if status == errSecSuccess { FleetLookStore.log.notice("deleted the keychain look of an older build") }
    }()

    func endAll() {
        endTask?.cancel()
        endTask = nil
        for activity in Activity<JarvisActivityAttributes>.activities {
            nonisolated(unsafe) let a = activity
            Task { await a.end(nil, dismissalPolicy: .immediate) }
        }
    }

    // Activity is not Sendable, but its methods are safe from any thread (ActivityKit does its own locking).
    /// Whether `state` needs an update over what the activity shows: each one redraws the island and the lock screen,
    /// and most fleet events change something this phone does not show (another island slot, the numbers of another
    /// machine). The same content still goes when its stale date is near, which each update moves 15 minutes on.
    nonisolated static func worthUpdating(_ shown: FleetState, staleDate: Date?, to state: FleetState, now: Date) -> Bool {
        shown != state || (staleDate ?? .distantPast) < now.addingTimeInterval(5 * 60)
    }

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
        // To the first group's hub only (docs/multi-hub.md「实时活动」): two hubs pushing the same activity would each
        // overwrite it with their own agents. In the app the activity shows every group (`AppModel.mergedFleet`); in
        // the background it follows that hub's pushes, and its unqualified ids are that group's (FleetActions).
        guard let api = AppModel.shared.links.first?.api else { return }
        Task { try? await api.reportLiveActivity(device: PushRegistrar.deviceID, body) }
    }
}

#if DEBUG
extension LiveActivityController {
    /// `-showFleetSample needsYou|working|done|thinking|assistant|offline` (`-fleetDetails YES` for names on the lock screen,
    /// `-fleetIsland b`, `c` or `custom:assistant,load` for the island's sides, `-showFleetCount 12` for that many in
    /// each kind there is; `-openPersona jarvis-circuit` for the persona): starts the activity with made-up agents, no
    /// hub needed, for screenshots. `thinking` is only the assistant answering (beside one finished agent);
    /// `assistant` is the assistant alone, nothing to count (the minimal island then shows the persona); `offline` is
    /// the working sample made stale once it shows.
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
        case "assistant": FleetState(phase: .none)
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
            if ["done", "thinking", "assistant", "offline"].contains(phase) {
                endAll()
                try? await Task.sleep(for: .seconds(1))
                let content = ActivityContent(state: state.tailored(island: island, details: details, look: AppModel.shared.fleetLook),
                                              staleDate: .now.addingTimeInterval(15 * 60))
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
    /// What the Live Activity draws for the persona on screen (docs/live-activity.md「形象」).
    var fleetLook: FleetLook { FleetLook(persona: persona, builtinAccent: builtinAccent) }
    var fleetFaces: FleetFaces? { FleetFaces(persona: persona, personas: personas) }
}

nonisolated extension FleetLook {
    /// A built-in in the accent it has in the app; a package as its pictures (`FleetFaces`), and while they cannot be
    /// read the default face in its primary color. From the persona itself, not `Theme`: the spec is recomposed after
    /// the persona changes.
    init(persona p: Persona, builtinAccent: Color) {
        guard !p.isBuiltin else {
            self.init(design: p.design.flatMap(PersonaFace.Design.init(rawValue:)) ?? .visor, tint: builtinAccent)
            return
        }
        let color = p.palette?.primary
        self.init(design: PersonaFace.Design.visor.rawValue, light: color?.light ?? Self.standard.light,
                  dark: color?.dark ?? Self.standard.dark, package: "\(p.id)/\(p.rev)")
    }
}

extension FleetFaces {
    /// A package's faces for the activity: one picture a state, its first frame (what the top bar's dot holds still),
    /// and the avatar only for states without one, each made at every face size from the package's own pixels. Nil for
    /// a built-in, or a package not on the phone.
    @MainActor init?(persona p: Persona, personas: PersonaStore) {
        guard let m = p.manifest, personas.isOnPhone(p) else { return nil }
        var states: [String: Picture] = [:]
        for s in PersonaState.allCases where m.dot.states[s.rawValue] != nil {
            if let first = personas.frames(p, s)?.images.first, let picture = Self.picture(first) { states[s.rawValue] = picture }
        }
        // The avatar file at least as large as the largest face, scaled down from it.
        let avatar = states.count == PersonaState.allCases.count ? nil
            : personas.avatar(p, points: CGFloat(Self.sizes[Self.sizes.count - 1]), scale: Self.scale).flatMap(Self.picture)
        guard !states.isEmpty || avatar != nil else { return nil }
        self.init(package: "\(p.id)/\(p.rev)", states: states, avatar: avatar)
    }
}

extension Data {
    nonisolated var hex: String { map { String(format: "%02x", $0) }.joined() }
}
