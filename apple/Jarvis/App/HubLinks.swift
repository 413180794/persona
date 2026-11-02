import Foundation
import Observation

/// One group of paired hubs as the app runs it (docs/multi-hub.md): its socket, the hello of the hub it is on, and the
/// per-hub things that came with it. The default group's link is the only one until a second group is paired.
@Observable
final class HubLink: Identifiable {
    let group: String
    var id: String { group }
    let live = LiveConnection()
    /// Who answered on the socket: the hub's id, name, features and group (its hello).
    private(set) var hello = HubHello()
    /// What the hub listed in its hello beyond the first protocol (`terminal_stream`, `file_upload`…).
    private(set) var features: Set<String> = []
    /// The group's last Fleet state (hub/liveactivity.go): merged with the other groups' for the Live Activity.
    var fleet: FleetState?
    /// The desktop apps connected to the hub (the 「Mac 上的 Jarvis 已登录」 banner shows the hub-level group's).
    var desktops: [DesktopDevice] = []
    /// Pinned and muted spaces / agents: each hub keeps its own.
    @ObservationIgnored let pins: PinsStore
    /// Where requests go when the socket has not picked a hub yet: the group's first hub.
    @ObservationIgnored var fallback: ServerConfig

    init(group: String, config: ServerConfig, pins: PinsStore) {
        self.group = group
        self.fallback = config
        self.pins = pins
    }

    /// The hub requests go to: the one the socket is on.
    var config: ServerConfig { live.currentConfig ?? fallback }
    var api: APIClient { APIClient(config: config, group: group) }
    var isDemo: Bool { config.isDemo }

    func received(_ hello: HubHello) {
        self.hello = hello
        let f = Set(hello.features)
        if f != features { features = f }
    }
}

extension AppModel {
    // MARK: Groups (docs/multi-hub.md)

    /// The groups in order (the default group first).
    var groupIDs: [String] { links.map(\.group) }
    /// Several groups: lists say which group each row is from and can show one group only. One group: nothing changes.
    var isMultiHub: Bool { links.count > 1 }

    func link(_ group: String) -> HubLink? { links.first { $0.group == group } }

    /// The group hub-level pages (Jarvis, 收件箱, the hub's settings) show: the one picked, else the first.
    var hub: HubLink? { link(hubGroup) ?? links.first }

    /// A client for the group a session, machine or interaction is from.
    func api(_ group: String) -> APIClient? { link(group)?.api }

    /// Back in the app: every group's socket opens again.
    func resumeLinks() {
        for link in links {
            link.live.suspended = false
            link.live.ensureConnected()
        }
    }

    /// In the background: every socket closes (the hubs wake the phone with a silent push when they need it).
    func suspendLinks() {
        for link in links {
            link.live.suspended = true
            link.live.disconnect()
        }
    }

    /// A silent push in the background: the group that sent it connects for its call (nil: every group).
    func wakeLinks(_ group: String?) {
        for link in links where group == nil || link.group == group { link.live.wake() }
    }

    /// The group's name as lists label it: the user's name for it, else its first hub's (`hub_name`), else what the
    /// hub the socket is on calls itself, else the address. Not the connected hub's first: that changes with the route.
    func groupName(_ group: String) -> String {
        if link(group)?.isDemo == true { return "体验模式" }
        if let n = registry.groupNames[group]?.trimmingCharacters(in: .whitespacesAndNewlines), !n.isEmpty { return n }
        if let n = registry.primary(group)?.hubName?.trimmingCharacters(in: .whitespacesAndNewlines), !n.isEmpty { return n }
        if let name = link(group)?.hello.hubName, !name.isEmpty { return name }
        return registry.name(group)
    }

    /// Picks the group hub-level pages show.
    func selectHub(_ group: String) {
        guard group != hub?.group, link(group) != nil else { return }
        hubGroup = group
        UserDefaults.standard.set(group, forKey: Self.hubGroupKey)
        hubChanged()
    }

    static let hubGroupKey = "hub.group"

    /// A key from outside the app's own lists (a deep link, a notification, Siri, a launch argument). `group` nil:
    /// not said (an older link, a push from an older hub) — the group that has this session, else the first group.
    /// A group that is gone is treated the same way.
    func resolve(machineID: String, sessionID: String, group: String?) -> SessionKey {
        if let group, link(group) != nil { return SessionKey(group: group, machineID: machineID, sessionID: sessionID) }
        let found = links.map(\.group).first { g in
            sessionsByMachine[MachineRef(group: g, id: machineID)]?.contains { $0.sessionID == sessionID } == true
        }
        return SessionKey(group: found ?? links.first?.group ?? "", machineID: machineID, sessionID: sessionID)
    }

    /// A key a hub-level page read off its hub (a routine's conversation, the AI 分身's): in the hub-level group.
    func inHub(_ key: SessionKey) -> SessionKey {
        var k = key
        k.group = hub?.group ?? ""
        return k
    }

    /// `resolve` for a key read from a string (`SessionKey.path`): a path without a group says nothing about it.
    func resolve(path: String) -> SessionKey? {
        guard let k = SessionKey(path: path) else { return nil }
        return resolve(machineID: k.machineID, sessionID: k.sessionID, group: path.contains("|") ? k.group : nil)
    }

    /// The group a push came from: the hub that sent it (`hub_id`), the push key that opened it, else the group that
    /// has its session (docs/multi-hub.md「推送」).
    func pushGroup(_ info: [AnyHashable: Any]) -> String? {
        if let id = info["hub_id"] as? String, let g = registry.group(hubID: id), link(g) != nil { return g }
        if let g = info[PushCrypto.groupKey] as? String, link(g) != nil { return g }
        return nil
    }
}

extension AppModel {
    // MARK: Routes, hellos, groups changing

    /// What a group's socket asks before each attempt when the group has more than one place to go: the quickest
    /// hub that answers (HubRoute). One place: nil, the socket goes where it was told.
    func routePicker(_ group: String) -> (@MainActor () async -> ServerConfig?)? {
        guard HubRoute.candidates(registry, group: group).count > 1 else { return nil }
        return { [weak self] in
            guard let self else { return nil }
            let candidates = HubRoute.candidates(self.registry, group: group)
            return await HubRoute.pick(candidates)?.config
        }
    }

    /// A hello came: remember which hub answered, its name and its group (`mesh`), and regroup the paired hubs when
    /// that changed what belongs together (docs/multi-hub.md「分组」).
    func learned(_ hello: HubHello, group: String) {
        guard let link = link(group), !link.isDemo else { return }
        defer { watchCenter(group) } // on a member now? (after the registry learned who the center is)
        var r = registry
        let on = link.live.currentConfig
        // The paired hub the socket is on; on an address only the mesh gave, the group's first hub stands for it.
        guard let i = r.hubs.firstIndex(where: { $0.group == group && $0.config == on })
                ?? r.hubs.firstIndex(where: { $0.group == group }) else { return }
        let paired = r.hubs[i].config == on
        if paired {
            if let id = hello.hubID, !id.isEmpty { r.hubs[i].hubID = id }
            if let name = hello.hubName, !name.isEmpty { r.hubs[i].hubName = name }
            // No mesh in its hello (a hub before the mesh, or one that left it): it lists nobody now. A mesh kept from
            // an earlier hello would show its old peers (and itself, under an old hub_id) as hubs of the group.
            if hello.mesh == nil { r.hubs[i].mesh = nil }
        }
        if let mesh = hello.mesh {
            r.hubs[i].mesh = mesh
            // Paired hubs the socket was never on: who they are, from the address the mesh lists for them.
            for j in r.hubs.indices where r.hubs[j].hubID == nil || r.hubs[j].hubName == nil {
                let url = r.hubs[j].config.url.absoluteString.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
                guard let m = mesh.first(where: { $0.endpoints?.contains { $0.trimmingCharacters(in: CharacterSet(charactersIn: "/")) == url } == true })
                else { continue }
                if r.hubs[j].hubID == nil { r.hubs[j].hubID = m.hubID }
                if r.hubs[j].hubName == nil, let n = m.name, !n.isEmpty { r.hubs[j].hubName = n }
            }
        }
        let regrouped = HubGrouping.regroup(r)
        guard regrouped != registry else { return }
        defer { relabelMachines() } // the hub may have a new name
        let groupsChanged = regrouped.groups != registry.groups || regrouped.hubs.map(\.group) != registry.hubs.map(\.group)
        registry = regrouped
        saveRegistry(regrouped)
        guard groupsChanged else {
            // Same groups, a new address to try: the socket picks among them from its next attempt.
            for l in links where !l.isDemo { l.live.route = routePicker(l.group) }
            return
        }
        // Groups merged or split: the groups that are gone take their lists with them (the group that stays lists
        // the same computers), and the new ones connect.
        let before = Set(groupIDs)
        buildLinks()
        let gone = before.subtracting(groupIDs)
        // A merged-away group's push key stays: its hubs may still seal to it until the phone registers again.
        if !gone.isEmpty { forgetGroups(gone, keepingPushKeys: true) }
        for l in links where !before.contains(l.group) { l.live.connect(l.fallback) }
        pushWatchConfigs()
    }

    /// The socket is on a member while the group's center is another hub (docs/multi-hub.md「选路」): ask the center
    /// every few minutes and move there once it answers. Ends when the socket is on the center, or the group or its
    /// center is gone.
    func watchCenter(_ group: String) {
        guard centerWatch[group] == nil, onMember(group) else { return }
        centerWatch[group] = Task { @MainActor [weak self] in
            defer { self?.centerWatch[group] = nil }
            while !Task.isCancelled {
                try? await Task.sleep(for: HubRoute.centerRecheck)
                guard let self, !Task.isCancelled, let link = self.link(group), self.onMember(group) else { return }
                if link.live.suspended { continue } // in the background: nothing opens the socket anyway
                for c in HubRoute.candidates(self.registry, group: group) where c.center {
                    if await HubRoute.healthProbe(c.config) {
                        link.live.reroute()
                        return
                    }
                }
            }
        }
    }

    /// A group's socket is on a hub other than its center (the center known from the hellos).
    func onMember(_ group: String) -> Bool {
        guard let link = link(group), !link.isDemo, let on = link.live.currentConfig else { return false }
        let candidates = HubRoute.candidates(registry, group: group)
        guard candidates.contains(where: \.center) else { return false }
        return !candidates.contains { $0.center && $0.config == on }
    }

    /// 设置 › hub › 断开: a whole group (every hub of it) goes; the others keep everything. The last group: `unpair`.
    func removeGroup(_ group: String) {
        guard links.count > 1 else { unpair(); return }
        var r = registry
        r.hubs.removeAll { $0.group == group }
        r.groupNames[group] = nil
        registry = r
        saveRegistry(r)
        buildLinks()
        forgetGroups([group])
        if hubGroup == group { hubChanged() }
        pushWatchConfigs()
    }

    /// One hub of a group goes (the group stays with its other hubs). Its last hub: the group goes.
    func removeHub(_ id: String) {
        guard let hub = registry.hubs.first(where: { $0.id == id }) else { return }
        guard registry.members(hub.group).count > 1 else { removeGroup(hub.group); return }
        var r = registry
        r.hubs.removeAll { $0.id == id }
        registry = r
        saveRegistry(r)
        buildLinks()
        if let link = link(hub.group), link.live.currentConfig == hub.config {
            link.live.disconnect()
            link.live.connect(link.fallback)
        }
        pushWatchConfigs()
    }

    /// 设置 › hub › 改名: what lists call a group ("" goes back to its hub's own name).
    func renameGroup(_ group: String, to name: String) {
        var r = registry
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        r.groupNames[group] = n.isEmpty ? nil : n
        registry = r
        saveRegistry(r)
        relabelMachines()
        pushWatchConfigs()
    }

    /// The registry with one hub changed (its device token, its watch's credential), saved.
    func updateHub(_ id: String, _ change: (inout PairedHub) -> Void) {
        guard let i = registry.hubs.firstIndex(where: { $0.id == id }) else { return }
        var r = registry
        change(&r.hubs[i])
        registry = r
        saveRegistry(r)
    }

    // MARK: Live Activity

    /// Every group's Fleet state as one (docs/multi-hub.md「实时活动」): counts added up, the requests of every group
    /// first, then the working and finished agents; another group's rows carry it in their ids ("group|…"), so a tap
    /// or a button reaches the right hub. One group: its state as the hub sent it.
    func mergedFleet() -> FleetState {
        let states = links.compactMap { l in l.fleet.map { (group: l.group, state: $0) } }
        guard states.count > 1 || states.first?.group.isEmpty == false else { return states.first?.state ?? FleetState() }
        return FleetMerge.merge(states, base: hub?.fleet ?? states.first?.state)
    }

    // MARK: Watch

    /// Each group's credential for the watch: the watch's own answer-only one where there is one, else the phone's.
    func watchConfigs() -> [String: ServerConfig] {
        var out: [String: ServerConfig] = [:]
        for g in registry.groups {
            guard let primary = registry.primary(g) else { continue }
            out[g] = primary.watch ?? primary.config
        }
        return out
    }

    func pushWatchConfigs() {
        guard !isDemo else { return }
        let configs = watchConfigs()
        guard !configs.isEmpty else { return }
        WatchSync.shared.push(configs: configs, names: Dictionary(uniqueKeysWithValues: registry.groups.map { ($0, groupName($0)) }))
    }
}

/// What a push names: its machine and session, and which group it came from when it says (`hub_id` of the hub that
/// sent it, or the group whose key opened it). Read off the main thread; `key` resolves it on it.
nonisolated struct PushRef: Sendable, Equatable {
    var machine: String
    var session: String
    var hubID: String?
    var group: String?

    init?(_ info: [AnyHashable: Any]) {
        guard let machine = info["machine_id"] as? String, let session = info["session_id"] as? String,
              !machine.isEmpty, !session.isEmpty else { return nil }
        self.machine = machine
        self.session = session
        hubID = info["hub_id"] as? String
        group = info[PushCrypto.groupKey] as? String
    }

    /// The session in its group: the hub that sent the push, the key that opened it, else the group that has such a
    /// session, else the first group (docs/multi-hub.md「推送」).
    @MainActor func key(_ model: AppModel) -> SessionKey {
        var info: [AnyHashable: Any] = [:]
        if let hubID { info["hub_id"] = hubID }
        if let group { info[PushCrypto.groupKey] = group }
        return model.resolve(machineID: machine, sessionID: session, group: model.pushGroup(info))
    }
}

/// Merging the groups' Fleet states (pure, for the tests).
nonisolated enum FleetMerge {
    static func merge(_ states: [(group: String, state: FleetState)], base: FleetState?) -> FleetState {
        var out = base ?? FleetState()
        out.needsYou = states.reduce(0) { $0 + $1.state.needsYou }
        out.working = states.reduce(0) { $0 + $1.state.working }
        out.unread = states.reduce(0) { $0 + $1.state.unread }
        out.phase = out.needsYou > 0 ? .needsYou : out.working > 0 ? .working : out.unread > 0 ? .done : .none
        out.workingSince = states.compactMap(\.state.workingSince).min()
        out.assistant = states.contains { $0.state.assistant }
        let items = states.flatMap { g, s in s.items.map { qualified($0, group: g) } }
        out.items = items.filter(\.needsYou) + items.filter { !$0.needsYou }
        return out
    }

    static func qualified(_ item: FleetState.Item, group: String) -> FleetState.Item {
        guard !group.isEmpty else { return item }
        var i = item
        i.id = group + "|" + item.id
        i.interaction = item.interaction.map { group + "|" + $0 }
        return i
    }

    /// An interaction id from the activity: its group ("" when the hub pushed it, unqualified) and the hub's id.
    static func split(_ id: String) -> (group: String?, id: String) {
        guard let bar = id.firstIndex(of: "|") else { return (nil, id) }
        return (String(id[..<bar]), String(id[id.index(after: bar)...]))
    }
}
