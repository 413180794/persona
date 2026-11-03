import Foundation
import Observation
import SwiftUI
#if canImport(UIKit)
import UIKit
#endif
import UserNotifications

/// App-wide state: server pairing, live connection, machines, sessions, interactions and per-session chats.
@Observable
final class AppModel {
    static let shared = AppModel()

    /// Every hub paired and the groups they form (HubStore, docs/multi-hub.md). Changed only through the group
    /// functions (HubLinks.swift), which save it (`saveRegistry`).
    var registry: HubRegistry
    /// The keychain could not be read at launch (before the first unlock after boot): `registry` is empty because it is
    /// unknown, not because nothing is paired. Nothing is written until it reads (`retryRegistry`).
    @ObservationIgnored private(set) var registryUnreadable = false
    /// One per group, in the registry's order; the demo's alone in 体验模式.
    private(set) var links: [HubLink] = []
    /// The group hub-level pages show (`selectHub`); the first group when it is not one of them.
    var hubGroup: String
    /// The hub-level group's hub (what Persona, 收件箱 and the hub's settings talk to); nil when not paired.
    var config: ServerConfig? { hub?.config }
    /// Persona's looks (docs/persona.md): the chosen one, what the hub offers, the packages on the phone.
    let personas = PersonaStore()
    /// Which paths conversations mention are there on their machines (only those are linked).
    let filePresence = FilePresence()
    /// Which machine ports links point at are listening (docs/ports.md).
    let portPresence = PortPresence()
    /// The hub-level group's socket (its state for the pages about the hub); every group has its own (`links`).
    var live: LiveConnection { hub?.live ?? idleLive }
    @ObservationIgnored private let idleLive = LiveConnection()
    /// What the titles say about the links (等待网络 / 连接中… / 更新中…).
    let connection: ConnectionStatus
    private(set) var machines: [Machine] = [] {
        didSet {
            let choices = machines.map(MachineChoice.init)
            if choices != machineChoices { machineChoices = choices }
            saveSnapshot()
        }
    }
    /// The machines as menus list them (name, icon): changes only when one is added, removed or renamed.
    private(set) var machineChoices: [MachineChoice] = []
    private(set) var sessionsByMachine: [MachineRef: [Session]] = [:] {
        didSet {
            allSessions = sessionsByMachine.values.flatMap { $0 }
            sessionsNow = allSessions
            var index: [SessionKey: Session] = [:]
            for (machine, sessions) in sessionsByMachine {
                for session in sessions {
                    let key = SessionKey(group: machine.group, machineID: machine.id, sessionID: session.sessionID)
                    if index[key] == nil { index[key] = session }
                }
            }
            sessionIndex = index
            for (key, cell) in sessionCells where !Session.sameForItsPage(cell.value, index[key]) { cell.value = index[key] }
            countBadges()
            saveSnapshot()
        }
    }
    private(set) var allSessions: [Session] = []
    /// `allSessions` without watching it: for an action when it is tapped (the @ menu, the next agent), not for a view.
    /// A view reading `allSessions` is drawn again by every push about any agent; only the lists of agents may
    /// (`AllSessionsReadersTests`).
    @ObservationIgnored private(set) var sessionsNow: [Session] = []
    @ObservationIgnored private var sessionIndex: [SessionKey: Session] = [:]
    /// One observable cell per session a view has looked up (`session(_:)`): a view showing one session is drawn again
    /// when that session changes, not on every push about any of them (the open chat and the tab badges were redrawn
    /// for each machine's list, 2026-10-31). `allSessions` is still there for views about all of them.
    @ObservationIgnored private var sessionCells: [SessionKey: SessionCell] = [:]
    /// Pending interactions by `uid` (the hub's id, with the group for another group).
    private(set) var interactions: [String: Interaction] = [:] { didSet { saveSnapshot(); countBadges() } }
    /// The machine whose engine the pairing asks for (a newly paired group's hub machine).
    var pairingEngineMachine: MachineRef?
    /// A short message (`ToastOverlay`, or in the iPhone's shell its status row, which takes it as it is set:
    /// `toastSink`, docs/shell.md「状态行」).
    var toast: Toast? {
        didSet {
            guard let toast, let toastSink else { return }
            toastSink.take(toast)
            self.toast = nil
        }
    }
    /// The iPhone's shell while it is on screen: it shows the toasts in its status row, one after another.
    @ObservationIgnored weak var toastSink: (any ToastSink)?
    var openChat: SessionKey?
    /// A chat to open at its latest answer, like a finished one from the list (ChatView takes it): a tap on a
    /// "完成" notification. From the background the session list is stale (the agent still reads as working until the
    /// hub's next list), so the chat cannot tell by itself that this turn finished unseen (2026-10-31).
    var latestAnswerFor: SessionKey?
    var pendingConfirmation: PhoneConfirmation?
    /// Remembered across launches together with the open dev chat (restoreChat), so a relaunch lands where you were.
    var selectedTab: AppTab = .assistant {
        didSet {
            UserDefaults.standard.set(selectedTab.rawValue, forKey: "app.tab")
            #if DEBUG
            ShellTrace.shared.note("tab \(oldValue)->\(selectedTab)")
            #endif
            // The shell routes it now, not when a view is next drawn (docs/shell.md).
            if selectedTab != oldValue { tabRouter?.route(selectedTab, in: self) }
            #if PERF_SIGNPOST
            PerfTrace.event("Tab selection", selectedTab.rawValue)
            #endif
        }
    }

    /// The iPhone's shell while it is on screen (`ShellState`): it takes every new tab as it is set.
    @ObservationIgnored weak var tabRouter: (any TabRouter)?

    /// The dev chat that was open; saved when a chat opens, cleared when going back to the list.
    static func rememberChat(_ key: SessionKey?) {
        if let key { UserDefaults.standard.set(key.path, forKey: "dev.openChat") }
        else { UserDefaults.standard.removeObject(forKey: "dev.openChat") }
    }
    /// Session whose chat is on screen; its completion pings are suppressed.
    var visibleSession: SessionKey?
    private(set) var assistantConfig: APIClient.AssistantState? {
        didSet {
            if let g = hub?.group, let a = assistantConfig { assistantByGroup[g] = a }
            saveSnapshot()
        }
    }
    /// The assistant conversation shown in the 对话 tab (persisted across launches).
    var assistantCurrent: SessionKey? {
        didSet {
            let v = assistantCurrent?.path ?? ""
            UserDefaults.standard.set(v, forKey: "assistant.current")
        }
    }
    var debugShowNewChat = false
    /// Bumped whenever another page hands the user to a Persona conversation (`askJarvis`): the Persona tab goes back
    /// to its conversation even when it is the one already shown (a pushed 知识库 page would otherwise stay on top).
    var assistantReveal = 0

    @ObservationIgnored private(set) var chats: [SessionKey: ChatStore] = [:]
    @ObservationIgnored var flushingOutbox = false
    /// Per group: the task asking a member-connected group's center whether it answers again (HubLinks.watchCenter).
    @ObservationIgnored var centerWatch: [String: Task<Void, Never>] = [:]
    @ObservationIgnored let outboxWriter = Debouncer(.milliseconds(300))
    /// By group and machine.
    @ObservationIgnored private(set) var launcherCache: [MachineRef: [Launcher]] = [:]

    /// The hub-level group's client: for pages about the hub (Persona, 收件箱, its settings). A session's, a machine's
    /// or an interaction's requests go to its own group (`api(_:)`).
    var api: APIClient? { hub?.api }
    var isPaired: Bool { !links.isEmpty }
    /// 体验模式: the made-up hub inside the app (DemoHub), no server at all. Remembered across launches until the
    /// user leaves it or pairs. It is a group of its own, the only one.
    var isDemo: Bool { links.first?.isDemo == true }
    static let demoKey = "demo.on"

    init() {
        Task.detached(priority: .utility) { LarkAttachment.staging.sweep() }
        connection = ConnectionStatus()
        DemoHub.install()
        #if DEBUG
        // `-openWelcome YES`: start unpaired and out of 体验模式, on the pairing page (DemoModeTests); with `-demo.on YES`
        // too, 体验模式 from a clean start (UI tests, `launchJarvis`).
        if UserDefaults.standard.bool(forKey: "openWelcome") {
            HubStore.save(HubRegistry()) // known empty: deletes the registry and the old single pairing
            StateCache.remove(Self.snapshotName("")) // and the lists it left: 体验模式 would show them (a paired app)
            UserDefaults.standard.removeObject(forKey: Self.demoKey)
        }
        #endif
        switch HubStore.read() {
        case .registry(let r): registry = r
        case .unreadable:
            // Not "unpaired": launched before the first unlock after boot (a push in the background). Nothing is
            // saved until it reads (`saveRegistry`); it is read again when the app comes to the front.
            registry = HubRegistry()
            registryUnreadable = true
        }
        hubGroup = UserDefaults.standard.string(forKey: Self.hubGroupKey) ?? ""
        if let raw = UserDefaults.standard.string(forKey: "app.tab"), let tab = AppTab(rawValue: raw) {
            selectedTab = tab
        }
        // Remembered as `SessionKey.path`: no group in it is the default group.
        if selectedTab == .chats, let v = UserDefaults.standard.string(forKey: "dev.openChat") {
            openChat = SessionKey(path: v)
        }
        if let v = UserDefaults.standard.string(forKey: "assistant.current") {
            assistantCurrent = SessionKey(path: v)
        }
        connection.lives = { [weak self] in self?.links.map(\.live) ?? [] }
        buildLinks()
        if registry.hubs.isEmpty, !registryUnreadable, UserDefaults.standard.bool(forKey: Self.demoKey) {
            links = [makeLink("", .demo)]
        }
        // Last known machines, agents and pending interactions: the lists are there offline and on a cold start.
        if isPaired { restoreSnapshot(); restoreOutbox() }
        WatchSync.shared.configProvider = { [weak self] in self.flatMap { $0.watchConfigs()[""] ?? $0.watchConfigs().values.first } }
        WatchSync.shared.configsProvider = { [weak self] in
            guard let self, !self.registry.hubs.isEmpty else { return nil }
            return WatchConfigs(configs: self.watchConfigs(), names: Dictionary(uniqueKeysWithValues: self.registry.groups.map { ($0, self.groupName($0)) }))
        }

        WatchSync.shared.onActivated = { [weak self] in
            guard let self, self.isPaired, !self.isDemo else { return }
            self.pushWatchConfigs()
            WatchSync.shared.push(summary: self.watchSummary())
        }
        personas.onChange = { [weak self] in self?.applyPersonaPalette() }
        applyPersonaPalette()
        connection.onCaughtUp = { [weak self] in self?.flushOutbox() }
        #if os(iOS)
        if registryUnreadable {
            NotificationCenter.default.addObserver(forName: UIApplication.protectedDataDidBecomeAvailableNotification, object: nil,
                                                   queue: .main) { @Sendable _ in MainActor.assumeIsolated { AppModel.shared.retryRegistry() } }
        }
        #endif
        DemoHub.shared.onNotSent = { [weak self] in self?.toast = Toast(DemoText.notSent) }
        countBadges()
    }

    /// Saves the registry, unless it could not be read: writing then would replace the real one (or delete it, empty).
    func saveRegistry(_ r: HubRegistry) {
        HubStore.save(r, known: !registryUnreadable)
    }

    /// The keychain could not be read at launch: try again (the app came to the front, the phone was unlocked), and
    /// once it reads, start as a launch with it would have.
    func retryRegistry() {
        guard registryUnreadable, case .registry(let r) = HubStore.read() else { return }
        registryUnreadable = false
        registry = r
        buildLinks()
        if registry.hubs.isEmpty, UserDefaults.standard.bool(forKey: Self.demoKey) {
            links = [makeLink("", .demo)] // 体验模式, as a launch that could read would have opened it
            applyPersonaPalette()
        }
        guard isPaired else { return }
        restoreSnapshot()
        restoreOutbox()
        start()
        PushRegistrar.shared.registerIfPossible()
    }

    /// What `StreamingText` asks for, prepared off the main thread as events arrive.
    nonisolated static func prepare(_ event: ServerEvent) {
        switch event {
        // The block being written (the finished ones are memoized as they finish).
        case .delta(_, let text): _ = MarkdownMemo.content(StreamChunks.split(text).tail + " ▍")
        case .interaction(let interaction):
            if let plan = interaction.plan { _ = MarkdownMemo.content(plan) }
        default: break
        }
    }

    /// One link per group of the registry, keeping the ones that are already there (their sockets stay open).
    func buildLinks() {
        let wanted = registry.groups
        var out: [HubLink] = []
        for g in wanted {
            guard let primary = registry.primary(g) else { continue }
            if let had = links.first(where: { $0.group == g && !$0.isDemo }) {
                had.fallback = primary.config
                out.append(had)
            } else {
                out.append(makeLink(g, primary.config))
            }
        }
        for gone in links where !out.contains(where: { $0 === gone }) { gone.live.disconnect() }
        links = out
        for link in links where !link.isDemo { link.live.route = routePicker(link.group) }
        if link(hubGroup) == nil, let first = links.first { hubGroup = first.group }
        applyPersonaPalette()
        relabelMachines()
    }


    func makeLink(_ group: String, _ config: ServerConfig) -> HubLink {
        let pins = PinsStore(deps: .init(api: { [weak self] in self?.api(group) }, report: { [weak self] in self?.show($0) },
                                         suffix: group))
        let link = HubLink(group: group, config: config, pins: pins)
        link.live.onEvent = { [weak self] ev in self?.handle(ev.stamped(group), group: group) }
        link.live.prepareEvent = { Self.prepare($0) }
        link.live.onConnected = { [weak self] in self?.didConnect(group) }
        link.live.onChange = { [weak self] in self?.connection.update() }
        return link
    }

    #if DEBUG
    /// `-pairURL` (and `-pairURL2`, `-pairURL3`: more hubs, for the multi-hub screenshots and tests): the hubs the
    /// simulator runs with. Hubs already paired with the same address and credential keep their group and ids.
    func adopt(_ configs: [ServerConfig]) {
        var r = HubRegistry()
        for cfg in configs {
            if let had = registry.hubs.first(where: { $0.config == cfg }) {
                r.hubs.append(had)
            } else {
                var hub = PairedHub(config: cfg)
                hub.group = r.newGroupID()
                r.hubs.append(hub)
            }
        }
        r.groupNames = registry.groupNames
        // `-seedSSHTunnel <base64 of the SSHRoute JSON>`: the first hub's SSH tunnel (end-to-end runs, screenshots;
        // docs/network.md). Base64: a launch argument that looks like a dictionary is read as a property list.
        if let s = UserDefaults.standard.string(forKey: "seedSSHTunnel"), !r.hubs.isEmpty, let json = Data(base64Encoded: s),
           let route = try? JSONDecoder().decode(SSHRoute.self, from: json) { r.hubs[0].ssh = route }
        registry = HubGrouping.regroup(r)
        saveRegistry(registry)
        buildLinks()
        if machines.isEmpty { restoreSnapshot() }
    }
    #endif

    func start() {
        guard isPaired else { return }
        for link in links {
            #if os(iOS)
            // Launched in the background (a push, a file from the watch): the socket waits for the app to open.
            link.live.suspended = UIApplication.shared.applicationState == .background
            #endif
            link.live.connect(link.fallback)
        }
        if isDemo { return } // no watch, no device credentials: nothing to talk to
        pushWatchConfigs()
        Task { await upgradeCredentials() }
    }

    /// Adds a hub (scanned / pasted pairing link). Another group's hub, or a hub already paired again, joins what is
    /// there; any other hub starts a group of its own beside the others (docs/multi-hub.md「添加 hub」). The other
    /// groups keep everything.
    /// `ssh`: the SSH tunnel set up on the pairing page (docs/network.md「经 SSH 隧道连 hub」): pairing goes through it
    /// when the address does not answer (or always, with 总是走 SSH), and the hub keeps it.
    func pair(_ new: ServerConfig, ssh: SSHRoute? = nil) async throws {
        retryRegistry()
        // Unknown pairings would be written over (the keychain is not readable before the first unlock).
        guard !registryUnreadable else { throw APIError(status: 0, message: "现在读不到已保存的配对，解锁手机后再试") }
        let tunnels = SSHTunnels.shared
        // Paired again: the tunnel it has, unless the pairing page set one.
        let ssh = ssh ?? registry.hubs.first { $0.config.url == new.url }?.ssh
        if let ssh, ssh.isUsable {
            let direct = ssh.always ? false : await HubRoute.healthProbe(new)
            if !direct { try await tunnels.open(SSHTunnels.pairingID, route: ssh, hub: new.url) }
        }
        let client = APIClient(config: new)
        let pairedMachines: [Machine]
        do {
            pairedMachines = try await client.machines()
        } catch {
            tunnels.close(SSHTunnels.pairingID)
            throw error
        }
        let identity = try? await client.identity()
        if isDemo { leaveDemo() } // a real hub ends 体验模式
        var r = registry
        let group: String
        let same = r.hubs.firstIndex { h in
            if let id = identity?.hubID, !id.isEmpty, let had = h.hubID { return had == id }
            return h.config.url == new.url // a hub that does not say who it is: the same address
        }
        if let i = same {
            // The same hub paired again (a new pairing link): its new credential replaces the old one.
            r.hubs[i].config = new
            r.hubs[i].deviceID = nil
            r.hubs[i].watch = nil
            if let ssh { r.hubs[i].ssh = ssh }
            group = r.hubs[i].group
            tunnels.adopt(SSHTunnels.pairingID, as: r.hubs[i].id)
        } else {
            var hub = PairedHub(config: new, hubID: identity?.hubID, hubName: identity?.hubName)
            hub.ssh = ssh
            tunnels.adopt(SSHTunnels.pairingID, as: hub.id)
            // Listed in a group's mesh already: it joins that group (shown once, not as a group of its own).
            hub.group = identity?.hubID.flatMap { r.group(hubID: $0) } ?? r.newGroupID()
            r.hubs.append(hub)
            group = hub.group
        }
        let fresh = link(group) == nil
        registry = r
        saveRegistry(r)
        buildLinks()
        if fresh {
            replaceMachines(group, pairedMachines.map { var m = $0; m.group = group; return m })
            // The engine step only when the hub reports engines (an older hub cannot save one; the step must not trap).
            pairingEngineMachine = EngineSelectionModel.pairingMachine(pairedMachines).flatMap { $0.engines.isEmpty ? nil : MachineRef(group: group, id: $0.id) }
        }
        if links.count == 1 { hubGroup = group }
        guard let link = link(group) else { return }
        link.live.disconnect()
        link.live.connect(registry.primary(group)?.config ?? new)
        pushWatchConfigs()
        PushRegistrar.shared.registerIfPossible()
        Task { await upgradeCredentials() }
    }


    /// 体验模式 from the pairing page: every tab runs on DemoHub's made-up data, nothing is sent anywhere.
    func enterDemo() {
        guard !isPaired else { return }
        DemoHub.shared.reset()
        InboxStore.shared.forget()
        TodoStore.shared.forget()
        TasksStore.shared.forget()
        forgetDemoChats()
        chats = [:]
        assistantCurrent = DemoHub.shared.mainConversation // Persona opens on a conversation, not a blank new one
        UserDefaults.standard.set(true, forKey: Self.demoKey)
        let demo = makeLink("", .demo)
        links = [demo]
        hubGroup = ""
        applyPersonaPalette()
        machines = []
        sessionsByMachine = [:]
        pendingSessions = [:] // a burst from the hub left before this
        interactions = [:]
        muteWhileDesktop = false
        selectedTab = .chats
        demo.live.connect(.demo)
        toast = Toast("已进入体验模式，数据都是编造的")
    }

    /// Out of 体验模式 (退出 or pairing): what it showed goes with it.
    private func leaveDemo() {
        UserDefaults.standard.set(false, forKey: Self.demoKey)
        for link in links { link.live.disconnect() }
        links = []
        applyPersonaPalette()
        InboxStore.shared.forget()
        TodoStore.shared.forget()
        TasksStore.shared.forget()
        forgetDemoChats()
        chats = [:]
        subscribers = [:]
        shown = []
        machines = []
        sessionsByMachine = [:]
        pendingSessions = [:]
        interactions = [:]
    }

    /// What the demo's conversations left behind: drafts, and cached chats (a new demo starts over).
    private func forgetDemoChats() {
        let demo = DemoHub.shared.sessionKeys
        drafts = drafts.filter { !demo.contains($0.key) }
        if let k = assistantCurrent, demo.contains(k) { assistantCurrent = nil }
        if let k = openChat, demo.contains(k) { openChat = nil }
        for key in demo { ChatCache.remove(key) }
    }

    /// 退出 everything: every group, every hub (设置 › 断开 with one group, a revoked credential of this device).
    func unpair() {
        LarkChatOutbox.forget()
        LarkDrafts.shared.forget()
        pairingEngineMachine = nil
        if isDemo { leaveDemo() }
        for link in links { link.live.disconnect() }
        LiveActivityController.shared.endAll() // its buttons would talk to a hub we no longer know
        links = []
        registry = HubRegistry()
        saveRegistry(registry)
        applyPersonaPalette()
        forgetDeviceCredentials()
        machines = []
        sessionsByMachine = [:]
        pendingSessions = [:] // a burst from the hub left before this
        interactions = [:]
        muteWhileDesktop = false
        chats = [:]
        subscribers = [:]
        shown = []
        saveOutbox()
    }

    /// The PUT response updates immediately; subsequent machines pushes remain authoritative.
    func applyMachine(_ machine: Machine) {
        guard let index = machines.firstIndex(where: { $0.ref == machine.ref }) else { return }
        var machine = machine
        machine.groupLabel = machines[index].groupLabel
        machines[index] = machine
    }

    /// With several groups every machine says which group it is in (`Machine.title`); with one, none does.
    func relabelMachines() {
        let multi = isMultiHub
        let relabeled = machines.map { m in
            var m = m
            m.groupLabel = multi ? groupName(m.group) : nil
            return m
        }
        if relabeled != machines { machines = relabeled }
    }


    /// One group's machines in the merged list (the other groups' stay where they are).
    func replaceMachines(_ group: String, _ list: [Machine]) {
        let label = isMultiHub ? groupName(group) : nil
        let list = list.map { m in
            var m = m
            m.group = group
            m.groupLabel = label
            return m
        }
        var merged: [Machine] = []
        for g in groupIDs {
            merged += g == group ? list : machines.filter { $0.group == g }
        }
        if !groupIDs.contains(group) { merged += list }
        if merged != machines { machines = merged }
    }


    // MARK: Derived

    /// One session, for views about it: its `rev`, `updatedAt` and last line may be older than the list's
    /// (`SessionCell`). An action that sends what the session is now reads `latest(_:)`.
    func session(_ key: SessionKey) -> Session? {
        if let cell = sessionCells[key] { return cell.value }
        let cell = SessionCell(sessionIndex[key])
        sessionCells[key] = cell
        return cell.value
    }

    /// One session as the hub last sent it, all of it, without watching it: for an action when it is tapped (交给
    /// Persona sends its last line), not for a view.
    func latest(_ key: SessionKey) -> Session? { sessionIndex[key] }

    /// A session this app just started, until the hub's next list for its machine (which replaces it) has it: its
    /// chat opened with no title (「对话 · 对话」) and no machine until then.
    func adopt(_ s: Session) {
        guard session(s.key) == nil else { return }
        sessionsByMachine[s.machineRef, default: []].append(s)
    }

    /// A Persona conversation this app just started with `text` as its first message: listed at once (`adopt`) and
    /// opened on that message as sent (`showSent`), not on 还没有消息 until claude has written it (#511, #527). Every
    /// place that starts one goes through here.
    func began(_ s: Session, text: String, images: [ImagePayload] = []) {
        adopt(s)
        // Matched to the transcript's copy by its words: not for a message of images only.
        if !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { showSent(s.key, text: text, images: images) }
    }

    func machine(_ ref: MachineRef) -> Machine? { machines.first { $0.id == ref.id && $0.group == ref.group } }
    /// A machine of the hub-level group (the assistant's, a hub setting's).
    func hubRef(_ id: String) -> MachineRef { MachineRef(group: hub?.group ?? "", id: id) }
    /// The hub-level group's machines (pages about the hub: Persona, its settings).
    var hubMachines: [Machine] { let g = hub?.group ?? ""; return machines.filter { $0.group == g } }
    /// The machine the hub runs on (it reports itself as "local"): where the assistant lives by default.
    var hubMachineID: String { hubMachines.first { $0.kind == "local" }?.id ?? hubMachines.first?.id ?? "" }

    var pendingInteractions: [Interaction] {
        interactions.values.sorted { ($0.createdAt ?? .distantPast) < ($1.createdAt ?? .distantPast) }
    }

    /// The hub-level group's Persona conversations (each hub has its own Persona), newest first.
    var assistantConversations: [Session] {
        let g = hub?.group ?? ""
        return allSessions.filter { $0.assistant && $0.archived != true && $0.group == g }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
    }

    /// The archived ones (已归档), newest first.
    var archivedConversations: [Session] {
        let g = hub?.group ?? ""
        return allSessions.filter { $0.assistant && $0.archived == true && $0.group == g }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
    }

    /// Loads the assistant config; with no remembered conversation, picks up the newest one.
    func refreshAssistant() async {
        guard let api else { return }
        if let st = try? await api.assistant() {
            guard api.group == hub?.group else { return } // another group was picked meanwhile
            assistantConfig = st
            // Only on the very first launch: after "新对话" the key exists but is empty, and the home page stays.
            let neverChosen = UserDefaults.standard.object(forKey: "assistant.current") == nil
            if assistantCurrent == nil, neverChosen {
                if let main = assistantMain {
                    assistantCurrent = main
                } else if let newest = st.conversations.max(by: { ($0.updatedAt ?? .distantPast) < ($1.updatedAt ?? .distantPast) }) {
                    assistantCurrent = SessionKey(group: api.group, machineID: st.machineID, sessionID: newest.sessionID)
                }
            }
            // Routines and the assistant's sense of "now" follow this device's time zone (the Mac app writes its
            // own too; the hub keeps whichever came last).
            let tz = TimeZone.current.identifier
            if st.timezone != tz, let updated = try? await api.setAssistant(timezone: tz) { assistantConfig = updated }
        }
    }

    /// The main conversation (the Persona tab's long-running one), when the hub has one.
    var assistantMain: SessionKey? {
        guard let st = assistantConfig, let id = st.main, !id.isEmpty else { return nil }
        return SessionKey(group: hub?.group ?? "", machineID: st.machineID, sessionID: id)
    }

    /// Shows a Persona conversation from another page (定时任务's 立即运行 / its own conversation): the Persona tab or the
    /// shell's 助手, back at the conversation even from a page pushed on it or the shell's 设置 sheet.
    func openAssistantConversation(_ key: SessionKey) {
        shellSettings = false
        selectedTab = .assistant
        selectHub(key.group)
        assistantCurrent = key
        assistantReveal += 1
    }

    /// Sends text to Persona' main conversation (starting it when there is none) and opens the Persona tab: how other
    /// pages hand something to Persona with its context.
    func askJarvis(_ text: String) async {
        guard let api else { return }
        selectedTab = .assistant
        assistantReveal += 1
        do {
            if let main = assistantMain { // an ended one is resumed by the hub
                assistantCurrent = main
                try await api.send(main, text: text)
            } else {
                let s = try await api.startConversation(prompt: text, main: true)
                began(s, text: text)
                assistantCurrent = s.key
                await refreshAssistant()
            }
            Haptics.success()
        } catch {
            show(error)
        }
    }

    @ObservationIgnored private var warmed: (launcher: String?, at: Date)?
    /// The new-conversation page is showing: have the hub start claude now, so the first message answers sooner.
    func warmAssistant(launcher: String?) async {
        guard let api else { return }
        if let w = warmed, w.launcher == launcher, Date().timeIntervalSince(w.at) < 180 { return }
        warmed = (launcher, Date())
        try? await api.warmAssistant(launcher: launcher)
    }

    func setAssistantLauncher(_ launcher: String) async {
        guard let api else { return }
        do {
            assistantConfig = try await api.setAssistant(launcher: launcher)
            // The main conversation follows the setting (the hub resumes it on this launcher): drop a one-off pick.
            if let main = assistantMain { assistantLaunchers[main] = nil }
            toast = Toast("Persona 下一条消息起使用 \(launcher)", style: .success)
        } catch { show(error) }
    }

    /// Routes a notification / deep link to the right tab; `latestAnswer` lands on the start of the agent's last answer.
    func open(_ key: SessionKey, latestAnswer: Bool = false) {
        if latestAnswer { latestAnswerFor = key }
        if session(key)?.assistant == true {
            selectedTab = .assistant
            selectHub(key.group) // a conversation of another group's Persona: that group's Persona
            assistantCurrent = key
        } else {
            selectedTab = .chats
            openChat = key
        }
    }

    func interactions(for key: SessionKey) -> [Interaction] {
        pendingInteractions.filter { $0.sessionKey == key }
    }

    func chat(_ key: SessionKey) -> ChatStore {
        chatTick += 1
        chatUse[key] = chatTick
        if let c = chats[key] { return c }
        let c = ChatStore(key: key)
        c.onArrived = { [weak self] in self?.saveOutbox() }
        chats[key] = c
        evictChats()
        return c
    }

    /// Chats kept in memory besides the ones on screen or subscribed: every session ever opened used to stay, all
    /// its loaded history with it. A dropped one comes back from the disk cache and the hub.
    static let keptChats = 8
    @ObservationIgnored private var chatUse: [SessionKey: Int] = [:]
    @ObservationIgnored private var chatTick = 0

    /// Forgets a chat held in memory (its group went away).
    func dropChat(_ key: SessionKey) {
        chats[key] = nil
        chatUse[key] = nil
        subscribers[key] = nil
        shown.removeAll { $0.key == key }
        if visibleSession == key { visibleSession = shown.last?.key }
    }

    private func evictChats() {
        // A chat with messages still going out stays (its pending list is the outbox).
        let idle = chats.keys.filter { k in
            subscribers[k] == nil && !shown.contains { $0.key == k } && k != visibleSession && chats[k]?.pending.isEmpty != false
        }
        guard idle.count > Self.keptChats else { return }
        for k in idle.sorted(by: { chatUse[$0, default: 0] < chatUse[$1, default: 0] }).prefix(idle.count - Self.keptChats) {
            chats[k] = nil
            chatUse[k] = nil
        }
    }

    // MARK: Subscriptions

    /// Who keeps each chat subscribed (one id per chat view): two views of the same session — opened again from its
    /// space page, or from a notification — must not end each other's subscription when one goes away.
    @ObservationIgnored private var subscribers: [SessionKey: Set<UUID>] = [:]
    /// Chat views on screen, newest last: `visibleSession` is the top one.
    @ObservationIgnored private var shown: [(owner: UUID, key: SessionKey)] = []
    @ObservationIgnored private var filling = Set<SessionKey>()

    func subscribe(_ key: SessionKey, by owner: UUID) {
        let first = subscribers[key, default: []].isEmpty
        subscribers[key, default: []].insert(owner)
        guard first else { return }
        let store = chat(key)
        socket(key.group)?.send(subscribeMessage(key))
        if socket(key.group)?.state != .connected && !store.loadedOnce { Task { await loadInitial(key) } }
    }

    func unsubscribe(_ key: SessionKey, by owner: UUID) {
        guard var owners = subscribers[key], owners.remove(owner) != nil else { return }
        guard owners.isEmpty else { subscribers[key] = owners; return }
        subscribers[key] = nil
        socket(key.group)?.send(["type": "unsubscribe", "machine_id": key.machineID, "session_id": key.sessionID])
    }

    /// The socket of a group.
    func socket(_ group: String) -> LiveConnection? { link(group)?.live }

    func isSubscribed(_ key: SessionKey) -> Bool { subscribers[key] != nil }

    // MARK: Terminal streams

    /// What a group's hub listed in its hello (`terminal_stream`: the terminal tab can subscribe to live frames).
    func features(_ group: String) -> Set<String> { link(group)?.features ?? [] }
    /// What the hub-level group's hub listed (hub-level pages: connectors, skills…).
    var hubFeatures: Set<String> { hub?.features ?? [] }
    /// The desktop apps (the Mac app) connected to the hub-level group's hub: the iPhone's 「Mac 上的 Persona 已登录」
    /// (DesktopPresence.swift).
    var desktops: [DesktopDevice] { hub?.desktops ?? [] }
    /// What the hub-level group's hub said it is in its hello: build and protocol, for the compatibility notices
    /// (docs/hub-update.md「兼容」); nil until it said anything.
    var hubHello: HubHello? {
        guard let hello = hub?.hello, hello != HubHello() else { return nil }
        return hello
    }
    /// The hub's version and its updating itself: fetched on connect, then the `hub_update` events.
    var hubUpdate: HubUpdateStatus? {
        didSet {
            guard hubUpdate?.restarting == true, oldValue?.restarting != true else { return }
            // A hub that is not back after its confirmation window and the watchdog's rollback (5 min) is not updating
            // any more: stop saying so (a reconnect fetches the real state anyway).
            hubRestartTimeout?.cancel()
            hubRestartTimeout = Task { [weak self] in
                try? await Task.sleep(for: .seconds(300))
                guard !Task.isCancelled, let self, self.hubUpdate?.restarting == true else { return }
                self.hubUpdate = nil
            }
        }
    }
    @ObservationIgnored private var hubRestartTimeout: Task<Void, Never>?
    /// This app's build (CFBundleVersion).
    static let appBuild = Int(Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "") ?? 0
    var hubCompat: HubCompat { HubCompat.of(hubHello, appBuild: Self.appBuild) }
    /// The lines over the Agents list the user closed (`HubNotice.dismissKey`), the last few. Written only on a tap.
    private(set) var hubNoticeDismissed: [String] = UserDefaults.standard.stringArray(forKey: "hubNotice.dismissed") ?? []
    func dismissHubNotice(_ key: String) {
        hubNoticeDismissed = Array((hubNoticeDismissed.filter { $0 != key } + [key]).suffix(8))
        UserDefaults.standard.set(hubNoticeDismissed, forKey: "hubNotice.dismissed")
    }
    /// The one line about the hub over the Agents list, if any (docs/hub-update.md「App 里」).
    var hubNotice: HubNotice? { isDemo ? nil : HubNotice.of(hubUpdate, compat: hubCompat, dismissed: hubNoticeDismissed) }
    /// 手机静音: this device's NotifyPrefs.muteWhileDesktop, shown on that banner.
    private(set) var muteWhileDesktop = false
    @ObservationIgnored private var muteWrites = 0
    /// The terminal views streaming each session (one at a time; a newer one replaces the handler).
    @ObservationIgnored private var terminalHandlers: [SessionKey: (TerminalEvent) -> Void] = [:]

    func canStreamTerminal(_ group: String) -> Bool { features(group).contains("terminal_stream") }
    /// The hub can let the phone drive a terminal at its own size (`terminal_control`).
    func canControlTerminal(_ group: String) -> Bool { features(group).contains("terminal_control") }
    /// The hub takes the wheel over a terminal the app drives (terminal_input scroll) to herdr.
    func canScrollTerminal(_ group: String) -> Bool { features(group).contains("terminal_scroll") }
    /// The hub takes a tap on a terminal the app drives (terminal_input click) to herdr as a click.
    func canClickTerminal(_ group: String) -> Bool { features(group).contains("terminal_click") }
    /// The hub-level group's socket is up.
    var isLive: Bool { live.state == .connected }
    /// A group's socket is up.
    func isLive(_ group: String) -> Bool { socket(group)?.state == .connected }


    /// Starts (or continues, with the last epoch and seq the view holds) a session's terminal stream. grid: drive it at
    /// that size (the phone's); takeover: even if another program on the computer holds it.
    func terminalSubscribe(_ key: SessionKey, epoch: String?, after: Int, grid: TerminalGridSize? = nil, takeover: Bool = false,
                           handler: @escaping (TerminalEvent) -> Void) {
        guard session(key)?.canTerminal == true else { return }
        terminalHandlers[key] = handler
        var msg: [String: Any] = ["type": "terminal_subscribe", "machine_id": key.machineID, "session_id": key.sessionID]
        if let epoch { msg["epoch"] = epoch; msg["after_seq"] = after }
        if let grid { msg["control"] = ["cols": grid.cols, "rows": grid.rows] }
        if takeover { msg["takeover"] = true }
        socket(key.group)?.send(msg)
    }

    /// Typed into a terminal the phone drives: the bytes go straight to it (in order, over the WebSocket). grid: the
    /// terminal's new size (the keyboard came up, the font changed).
    func terminalInput(_ key: SessionKey, _ text: String, grid: TerminalGridSize? = nil) {
        guard session(key)?.canTerminal == true else { return }
        var msg: [String: Any] = ["type": "terminal_input", "machine_id": key.machineID, "session_id": key.sessionID]
        if !text.isEmpty { msg["text"] = text }
        if let grid { msg["control"] = ["cols": grid.cols, "rows": grid.rows] }
        socket(key.group)?.send(msg)
    }

    /// The mouse wheel over a terminal the app drives, in lines (> 0 back in history), over a cell of its screen: herdr
    /// gives it to a program that takes the mouse (Claude Code, Codex scroll their conversation) and scrolls its own
    /// scrollback for one that does not — what a herdr client does with the wheel, as in Moshi.
    func terminalScroll(_ key: SessionKey, lines: Int, at: (col: Int, row: Int)?) {
        guard session(key)?.canTerminal == true, lines != 0 else { return }
        var msg: [String: Any] = ["type": "terminal_input", "machine_id": key.machineID, "session_id": key.sessionID, "scroll": lines]
        if let at { msg["at"] = [at.col, at.row] }
        socket(key.group)?.send(msg)
    }

    /// A tap on a terminal the app drives: a left click at that cell of its screen (0-based).
    func terminalClick(_ key: SessionKey, column: Int, row: Int) {
        guard session(key)?.canTerminal == true else { return }
        socket(key.group)?.send(["type": "terminal_input", "machine_id": key.machineID, "session_id": key.sessionID, "click": [column, row]])
    }

    func terminalUnsubscribe(_ key: SessionKey) {
        guard terminalHandlers.removeValue(forKey: key) != nil else { return }
        socket(key.group)?.send(["type": "terminal_unsubscribe", "machine_id": key.machineID, "session_id": key.sessionID])
    }
    private var subscribed: [SessionKey] { Array(subscribers.keys) }

    /// A chat view shows `key` (its completion pings are suppressed while it does).
    func show(_ key: SessionKey, by owner: UUID) {
        shown.removeAll { $0.owner == owner }
        shown.append((owner, key))
        visibleSession = key
    }

    func hide(by owner: UUID) {
        shown.removeAll { $0.owner == owner }
        visibleSession = shown.last?.key
    }

    /// Asks for everything after what the chat holds without holes (`covered`), or the last exchanges if nothing.
    private func subscribeMessage(_ key: SessionKey) -> [String: Any] {
        var msg: [String: Any] = ["type": "subscribe", "machine_id": key.machineID, "session_id": key.sessionID, "lazy_images": true,
                                  "fold_work": true]
        let covered = chat(key).covered
        if covered > 0 { msg["since_seq"] = covered }
        return msg
    }
    /// The chat's store if one is in memory (never creates one: background sync writes to the disk cache instead).
    func existingChat(_ key: SessionKey) -> ChatStore? { chats[key] }

    func loadInitial(_ key: SessionKey) async {
        guard let api = api(key.group) else { return }
        do {
            let res = try await api.items(key, beforeSeq: nil)
            settle(key, chat(key).apply(res.items, span: res.span, backlog: true, hasMore: res.hasMore))
        } catch {
            chat(key).error = error.shown
        }
    }

    /// Follows up on what a batch left to do (`ChatStore.Gap`).
    private func settle(_ key: SessionKey, _ gap: ChatStore.Gap) {
        switch gap {
        case .none: break
        case .reload: Task { await reload(key) }
        case .fill:
            // A live push can overtake the backlog asked for at subscribe time, and the backlog closes the hole
            // itself: give it a moment before fetching.
            Task {
                try? await Task.sleep(for: .seconds(1.5))
                await fill(key)
            }
        }
    }

    /// Fetches what comes after the chat's `covered`, page by page, until it is whole; past 8 pages it starts over from
    /// the latest page instead.
    func fill(_ key: SessionKey) async {
        guard let api = api(key.group), !filling.contains(key) else { return }
        filling.insert(key)
        defer { filling.remove(key) }
        let store = chat(key)
        for _ in 0..<8 {
            guard let r = try? await api.items(key, afterSeq: store.covered) else { return }
            switch store.apply(r.items, span: r.span, moreAfter: r.moreAfter == true) {
            case .none: return
            case .fill: continue
            case .reload: await reload(key); return
            }
        }
        await reload(key)
    }

    /// Starts the chat over from the latest page (the hole was too big, or the transcript was replaced).
    func reload(_ key: SessionKey) async {
        guard let api = api(key.group), let page = try? await api.items(key, beforeSeq: nil) else { return }
        chat(key).replace(with: page.items, hasMore: page.hasMore, span: page.span)
    }

    /// Fetches the work a fold stands for into the chat, once at a time per fold (several rows of one open work line
    /// may ask together).
    func unfold(_ key: SessionKey, _ fold: Item) async throws {
        guard let api = api(key.group) else { return }
        let id = "\(key.path)/\(fold.id)"
        if let running = unfolding[id] {
            _ = try await running.value
            return
        }
        let task = Task { try await api.foldedWork(key, fold) }
        unfolding[id] = task
        defer { unfolding[id] = nil }
        chat(key).unfold(fold, into: try await task.value)
    }
    @ObservationIgnored private var unfolding: [String: Task<[Item], Error>] = [:]

    /// The page before the oldest item; `turns` whole exchanges (the saved reading place's search asks for more).
    /// False when no page came (nothing older, already loading, or an error).
    @discardableResult
    func loadOlder(_ key: SessionKey, turns: Int = 2) async -> Bool {
        let store = chat(key)
        guard let api = api(key.group), store.hasMore, !store.isLoadingOlder, let first = store.items.first else { return false }
        store.isLoadingOlder = true
        defer { store.isLoadingOlder = false }
        do {
            let res = try await api.items(key, beforeSeq: first.seq, turns: turns)
            store.prepend(res.items, hasMore: res.hasMore)
            return true
        } catch {
            if !error.isOffline { show(error) } // scrolling up while offline: the title says why nothing comes
            return false
        }
    }

    // MARK: Events

    private func didConnect(_ group: String) {
        guard let link = link(group) else { return }
        registerPhone(link)
        if group == hub?.group {
            // The hub's version and whether an update is out or under way (an older hub has no such page: nil).
            Task { hubUpdate = try? await api?.hubUpdate() }
            Task { await personas.refresh(api) }
            LarkSync.shared.reconnected()
            InboxStore.shared.reload()
            TodoStore.shared.reload()
            TasksStore.shared.reconnected()
        }
        AgentSync.shared.schedule()
        // 更新中… until the open chats' backlog and the pending interactions are in (ConnectionStatus).
        let mine = subscribed.filter { $0.group == group }
        connection.startCatchUp(group, [Self.interactionsPart(group)] + mine.map(Self.catchUpPart))
        for key in mine {
            deltas.drop(key)
            chat(key).liveText = "" // a delta that cleared it may have been lost with the old socket
            link.live.send(subscribeMessage(key))
        }
        // Questions answered on the computer while the socket was down are gone from the hub: drop their cards.
        Task {
            defer { connection.caughtUp(Self.interactionsPart(group)) }
            guard let ixs = try? await api(group)?.interactions() else { return }
            replaceInteractions(group, ixs)
            WatchSync.shared.push(summary: watchSummary())
        }
    }

    /// One group's pending interactions (the other groups' stay).
    func replaceInteractions(_ group: String, _ list: [Interaction]) {
        var all = interactions.filter { $0.value.group != group }
        for ix in list where all[ix.uid] == nil { all[ix.uid] = ix }
        if all != interactions { interactions = all }
    }

    private static func catchUpPart(_ key: SessionKey) -> String { "chat:\(key.path)" }
    private static func interactionsPart(_ group: String) -> String { group.isEmpty ? "interactions" : "interactions:\(group)" }

    /// Offers this phone's tools to every group's hub (their assistants may call them).
    func registerPhone() {
        for link in links { registerPhone(link) }
    }

    private func registerPhone(_ link: HubLink) {
        if link.isDemo { return } // nobody would call the phone's tools
        var tools = PhoneToolRegistry.enabledSpecs()
        #if DEBUG
        // Simulator automation (scripts/app/sim.sh) must not take the real iPhone's phone_* tool calls.
        let automation = UserDefaults.standard.string(forKey: "pairURL") != nil
        if automation { tools = [] }
        #if os(iOS)
        tools += RuntimeCLI.hubToolSpecs(features: link.features) // the runtime CLI over the hub, when switched on
        #endif
        if automation && tools.isEmpty { return }
        #endif
        guard let data = try? JSON.encoder.encode(tools),
              let arr = try? JSONSerialization.jsonObject(with: data) else { return }
        #if targetEnvironment(simulator)
        let simulator = true
        #else
        let simulator = false
        #endif
        link.live.send(Self.registration(deviceName: UIDevice.current.name, tools: arr, simulator: simulator, deviceID: PushRegistrar.deviceID))
    }


    /// Turns 手机静音 on or off: optimistic, only this one setting is written (the PUT merges), and only the latest
    /// switch's answer counts (a quick on → off must not end on whichever reply came last).
    func setMuteWhileDesktop(_ on: Bool) async {
        guard let api else { return }
        muteWrites += 1
        let mine = muteWrites
        muteWhileDesktop = on
        do {
            let saved = try await api.setMuteWhileDesktop(device: PushRegistrar.deviceID, on).muteWhileDesktop
            guard mine == muteWrites else { return }
            muteWhileDesktop = saved
            Haptics.success()
        } catch {
            guard mine == muteWrites else { return }
            muteWhileDesktop = !on
            show(error)
        }
    }

    /// 退出 a desktop: revokes its device token; the hub closes its connection and says it is gone (desktops event).
    func signOutDesktop(_ d: DesktopDevice) async {
        guard let api, !d.device.isEmpty else { return }
        do {
            try await api.revokeToken(d.device)
            Haptics.success()
        } catch {
            show(error)
        }
    }

    /// How this device introduces itself to the hub. The iPhone is the phone (its phone_* tools, the wake pushes when
    /// it is away); a Mac is a desktop, like WeChat's 「Mac 微信已登录」 (hub/desktops.go, docs/mac.md「Mac 是桌面端，不是手机」):
    /// registered as the phone it took every phone_* call and kept the hub from waking the iPhone.
    /// `deviceID` is the phone's id for pushes: while it is connected the hub sends its Live Activity no pushes, the app
    /// draws it from the fleet events (hub/liveactivity.go).
    nonisolated static func registration(deviceName: String, tools: Any, simulator: Bool, deviceID: String = "") -> [String: Any] {
        #if os(macOS)
        ["type": "register_desktop", "device_name": deviceName, "device_id": deviceID]
        #else
        ["type": "register_phone", "device_name": deviceName, "tools": tools, "simulator": simulator, "device_id": deviceID]
        #endif
    }

    /// Streamed replies drawn at most 5 times a second (`DeltaPacer`).
    @ObservationIgnored private var deltas = DeltaPacer()
    /// Session lists received and not applied yet (`applyPendingSessions`), by machine.
    @ObservationIgnored private var pendingSessions: [MachineRef: [Session]] = [:]
    @ObservationIgnored private var sessionsFlushScheduled = false
    /// How long session lists wait for the rest of a burst: under a frame and a half, too short to see.
    static let sessionsCoalesce: TimeInterval = 0.025

    private func applyPendingSessions() {
        sessionsFlushScheduled = false
        let pending = pendingSessions
        pendingSessions = [:]
        applySessions(pending)
    }

    /// Machines' new session lists, whether they came whole (`sessions`) or as a delta (`sessions_delta`): the pages
    /// update once for the lot, and only when a list really changed; the notifications, the watch and the chats kept
    /// current in the background (`AgentSync`) follow them. The one place the lists land (msg-render's SessionStore
    /// replaces its inside, docs/ios-state.md).
    func applySessions(_ byMachine: [MachineRef: [Session]]) {
        var lists = sessionsByMachine
        var olds: [(old: [Session], new: [Session])] = []
        for (machineID, list) in byMachine {
            let old = lists[machineID] ?? []
            if old != list { lists[machineID] = list }
            olds.append((old, list))
        }
        if lists != sessionsByMachine { sessionsByMachine = lists }
        WatchSync.shared.push(summary: watchSummary())
        for (old, new) in olds {
            notifyTransitions(old: old, new: new)
            AgentSync.shared.sessions(new)
        }
    }

    private func scheduleSessionsFlush() {
        guard !sessionsFlushScheduled else { return }
        sessionsFlushScheduled = true
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.sessionsCoalesce) { [weak self] in self?.applyPendingSessions() }
    }

    private func handle(_ ev: ServerEvent, group: String) {
        #if DEBUG || PERF_SIGNPOST
        PerfTrace.push(Self.perfName(ev))
        #endif
        let isHub = group == hub?.group
        switch ev {
        case .hello(let hello):
            link(group)?.received(hello)
            #if DEBUG && os(iOS)
            // Registered on connect, before this hello said whether the hub keeps the runtime CLI from the agents.
            if RuntimeCLI.overHub, let link = link(group) { registerPhone(link) }
            #endif
            #if DEBUG
            // `-showDesktopSample YES`: a Mac signed in (and 手机静音 on), for screenshots of the banner (the demo hub sends no desktops).
            if UserDefaults.standard.bool(forKey: "showDesktopSample"), isHub {
                link(group)?.desktops = [DesktopDevice(device: "dev_sample", name: "MacBook Pro", since: Date().addingTimeInterval(-3600))]
                muteWhileDesktop = true
            }
            #endif
            for (key, handler) in terminalHandlers where key.group == group { handler(.reconnected) }
            learned(hello, group: group)
        case .terminal(let key, let event):
            terminalHandlers[key]?(event)
        case .machines(let list):
            // A machine that just came back: re-subscribe its open chats (their backlog failed while it was down).
            let wasOffline = Set(machines.filter { !$0.online && $0.group == group }.map(\.id))
            // Every view that names a machine reads the list: the same list again is no change.
            replaceMachines(group, list)
            for m in list where m.online && wasOffline.contains(m.id) {
                for key in subscribed where key.machineRef == m.ref {
                    chat(key).error = nil
                    socket(group)?.send(subscribeMessage(key))
                }
            }
        case .sessions(let machineID, let list):
            // A busy hub sends each machine's list on its own, one after another: they are applied together a moment
            // later, so the pages that read the sessions update once for the lot (one SwiftUI pass, not one a machine).
            let ref = MachineRef(group: group, id: machineID)
            let old = pendingSessions[ref] ?? sessionsByMachine[ref] ?? []
            pendingSessions[ref] = SessionPatch.snapshot(list, over: old, deltaHub: features(group).contains(SessionPatch.feature))
            scheduleSessionsFlush()
        case .sessionsDelta(let machineID, let upsert, let remove):
            // Over the newest the app has: a list still waiting for the flush, or what is shown.
            let ref = MachineRef(group: group, id: machineID)
            let base = pendingSessions[ref] ?? sessionsByMachine[ref] ?? []
            pendingSessions[ref] = SessionPatch.apply(base, upsert: upsert, remove: remove)
            scheduleSessionsFlush()
        case .revs(let machineID, let revs):
            AgentSync.shared.revs(machine: MachineRef(group: group, id: machineID), revs)
        case .lark(let feedIDs):
            if isHub { LarkSync.shared.received(feedIDs) }
        case .agents(let ids):
            if isHub { PersonalAgentSync.shared.received(ids) }
        case .inbox:
            if isHub { InboxStore.shared.reload() }
        case .todos:
            if isHub { TodoStore.shared.reload() }
            if isHub { TasksStore.shared.reloadTodos() }
        case .assistantFeed(let item):
            if isHub { TasksStore.shared.received(item) }
        case .taskPlan(let plan):
            if isHub { TasksStore.shared.received(plan) }
        case .fleet(let state):
            link(group)?.fleet = state
            LiveActivityController.shared.apply(mergedFleet())
        case .hubUpdate(let status):
            if isHub, hubUpdate != status { hubUpdate = status }
        case .desktops(let list):
            link(group)?.desktops = list
        case .items(let key, let items, let backlog, let hasMore, let span, let moreAfter):
            if backlog { connection.caughtUp(Self.catchUpPart(key)) }
            let store = chat(key)
            settle(key, store.apply(items, span: span, backlog: backlog, hasMore: hasMore, moreAfter: moreAfter))
            // The streamed message landed in the transcript: stop drawing its live copy (and the newer one kept back).
            let live = deltas.pendingText(key) ?? store.liveText
            if !live.isEmpty, items.contains(where: { $0.role == "assistant" && $0.kind == .text && ($0.text ?? "").hasPrefix(live) }) {
                deltas.drop(key)
                store.liveText = ""
            }
        case .screen(let key, let text, let status):
            let c = chat(key)
            c.screen = text
            c.screenStatus = status
            c.screenUpdatedAt = .now
        case .delta(let key, let text):
            switch deltas.offer(key, text, now: .now) {
            case .draw(let t): chat(key).liveText = t
            case .later(let wait):
                DispatchQueue.main.asyncAfter(deadline: .now() + wait) { [weak self] in
                    guard let self, let t = self.deltas.due(key, now: .now) else { return }
                    self.chat(key).liveText = t
                }
            case .kept: break
            }
        case .interaction(let ix):
            let isNew = interactions[ix.uid] == nil
            interactions[ix.uid] = ix
            if isNew { Haptics.attention() }
            WatchSync.shared.push(summary: watchSummary())
        case .interactionResolved(let id):
            interactions[group.isEmpty ? id : group + "|" + id] = nil
            WatchSync.shared.push(summary: watchSummary())
        case .toolCall(let callID, let name, let args):
            Task { await runPhoneTool(callID: callID, name: name, args: args, group: group) }
        case .hookEvent:
            break
        case .error(let key, let message):
            if let key { connection.caughtUp(Self.catchUpPart(key)) }
            if APIError.isTimeoutText(message) { break } // retried by the next poll / push; not shown
            if let key { chat(key).error = message } else { toast = Toast(message, style: .error) }
        }
    }

    #if DEBUG || PERF_SIGNPOST
    private static func perfName(_ ev: ServerEvent) -> String {
        switch ev {
        case .hello: "hello"
        case .machines: "machines"
        case .sessions: "sessions"
        case .sessionsDelta: "sessions_delta"
        case .items: "items"
        case .screen: "screen"
        case .delta: "delta"
        case .interaction: "interaction"
        case .interactionResolved: "interaction_resolved"
        case .toolCall: "tool_call"
        case .hookEvent: "hook"
        case .revs: "revs"
        case .lark: "lark"
        case .agents: "agents"
        case .inbox: "inbox"
        case .todos: "todos"
        case .assistantFeed: "assistant_feed"
        case .taskPlan: "task_plan"
        case .fleet: "fleet"
        case .hubUpdate: "hub_update"
        case .desktops: "desktops"
        case .terminal: "terminal"
        case .error: "error"
        }
    }
    #endif

    /// Haptic tick when a session we care about finishes while the user looks elsewhere.
    private func notifyTransitions(old: [Session], new: [Session]) {
        let before = Dictionary(old.map { ($0.sessionID, $0.status) }, uniquingKeysWith: { a, _ in a })
        for s in new where before[s.sessionID] == .working && (s.status == .idle || s.status == .done) {
            if visibleSession == s.key { markSeen(s.key) }
            if visibleSession == s.key { Haptics.success() }
        }
    }

    // MARK: Phone tools

    private func runPhoneTool(callID: String, name: String, args: JSONValue, group: String) async {
        #if DEBUG && os(iOS)
        if name == RuntimeCLI.hubTool {
            let (text, isError) = await RuntimeCLI.runFromHub(args)
            socket(group)?.send(["type": "tool_result", "call_id": callID, "content": [["type": "text", "text": text]], "is_error": isError])
            return
        }
        #endif
        let (content, isError) = await PhoneToolRegistry.call(name: name, args: args) { [weak self] summary in
            await self?.confirm(summary) ?? false
        }
        guard let data = try? JSON.encoder.encode(content), let arr = try? JSONSerialization.jsonObject(with: data) else { return }
        socket(group)?.send(["type": "tool_result", "call_id": callID, "content": arr, "is_error": isError])
    }


    private func confirm(_ summary: String) async -> Bool {
        Haptics.attention()
        #if os(macOS)
        // The alert hangs off the main window: bring it up (reopen it if it was closed) so the question is seen.
        // From the background activating may not be granted: the Dock icon bounces until the user comes.
        MacShellState.shared.showMain()
        NSApp.requestUserAttention(.criticalRequest)
        #endif
        return await withCheckedContinuation { cont in
            pendingConfirmation = PhoneConfirmation(summary: summary) { cont.resume(returning: $0) }
        }
    }

    // MARK: Actions

    func resolve(_ ix: Interaction, _ r: Resolution) async {
        // To the hub of the interaction's own group (docs/multi-hub.md「操作发到哪」).
        guard session(ix.sessionKey)?.canApprove == true, let api = api(ix.group) else { return }
        // No network: the card stays as it is (taking it away and bringing it back a moment later read as "done").
        guard connection.networkUp else { notDone(); return }
        interactions[ix.uid] = nil
        do {
            try await api.resolve(ix.id, r)
            Haptics.success()
        } catch let e as APIError where e.code == "prompt_changed" {
            // Nothing pressed: the terminal's dialog changed. The hub sends the card for what is there now.
            Haptics.attention()
            toast = Toast("终端上的对话框已经变了，这次没有按任何键：看看新的再答", style: .error)
        } catch {
            if (error as? APIError)?.status != 410 { interactions[ix.uid] = ix }
            show(error)
        }
    }

    /// Unsent composer text per chat (also where an edited queued message lands); kept across launches.
    var drafts: [SessionKey: String] = AppModel.savedDrafts() { didSet { saveDrafts() } }
    @ObservationIgnored let draftWriter = Debouncer()
    /// A send the computer refused because its input box holds a draft (SendSafety.swift).
    var sendRefusal: SendRefusal?

    /// Edit or delete one queued message: everything is pulled back out of claude and the others re-queued.
    func editQueued(_ key: SessionKey, index: Int, delete: Bool) async {
        guard allowSend(key), let api = api(key.group), let queue = session(key)?.queued, queue.indices.contains(index) else { return }
        do {
            let pulled = try await api.editQueue(key, keep: queue.indices.filter { $0 != index })
            if !delete, pulled.indices.contains(index) {
                let current = drafts[key] ?? ""
                drafts[key] = current.isEmpty ? pulled[index] : current + "\n" + pulled[index]
            }
            Haptics.success()
            toast = Toast(delete ? "已从队列删除" : "已撤回到输入框，改好再发送", style: .success)
        } catch {
            show(error)
        }
    }

    /// Launcher chosen in an assistant chat for its next messages (model switch).
    var assistantLaunchers: [SessionKey: String] = [:]

    /// Check at dispatch time too: capabilities can change while a menu or outbox bubble is on screen.
    func allowSend(_ key: SessionKey) -> Bool {
        guard let s = session(key) else { return false }
        guard s.canSend else {
            toast = Toast(s.sourceSubtitle ?? "此会话仅可查看", style: .error)
            return false
        }
        return true
    }

    /// Puts the message on screen immediately and sends it in the background; a failure marks it for retry.
    func sendNow(_ key: SessionKey, text: String, images: [ImagePayload] = []) {
        guard allowSend(key) else { return }
        let msg = PendingMessage(text: text, images: images)
        chat(key).pending.append(msg)
        saveOutbox()
        Haptics.tap()
        deliver(key, msg)
    }

    /// Sends /clear to a herdr pane's claude and waits for the pane to show up under its new session id (Claude Code
    /// starts a new transcript; the node follows the pane, sessions.go). Returns the new session, nil if it did not
    /// appear within 20 seconds.
    func clearContext(_ key: SessionKey) async -> SessionKey? {
        guard let pane = session(key)?.paneID, await send(key, text: "/clear") else { return nil }
        for _ in 0..<40 {
            try? await Task.sleep(for: .milliseconds(500))
            if let s = sessionsByMachine[key.machineRef]?.first(where: { $0.paneID == pane && $0.sessionID != key.sessionID && $0.state.isLive }) {
                return s.key
            }
        }
        toast = Toast("已发送 /clear，新会话出现后在列表里打开", style: .info)
        return nil
    }

    func retry(_ key: SessionKey, _ msg: PendingMessage, clearDraft: String? = nil) {
        guard allowSend(key) else { return }
        guard let i = chat(key).pending.firstIndex(where: { $0.id == msg.id }) else { return }
        if chat(key).pending[i].mayHaveArrived == true {
            // The user looked and wants it sent again: as a new message (the old id is answered "may have arrived").
            chat(key).pending[i].id = UUID()
            chat(key).pending[i].mayHaveArrived = nil
        }
        chat(key).pending[i].state = .sending
        deliver(key, chat(key).pending[i], clearDraft: clearDraft)
    }

    func discard(_ key: SessionKey, _ msg: PendingMessage) {
        chat(key).pending.removeAll { $0.id == msg.id }
        saveOutbox()
    }

    private func deliver(_ key: SessionKey, _ msg: PendingMessage, clearDraft: String? = nil) {
        Task {
            // The hub is out of reach: the message waits (clock, 连上后自动发送) instead of failing after a timeout.
            let socket = socket(key.group)
            let outcome: SendOutcome = socket?.state == .connected && socket?.stalled == false
                ? await sendPending(key, msg, clearDraft: clearDraft) : .waiting
            settleSend(key, msg.id, outcome)
        }
    }
    func send(_ key: SessionKey, text: String, images: [ImagePayload] = [], haptic: Bool = true) async -> Bool {
        guard allowSend(key), let api = api(key.group) else { return false }
        do {
            try await api.send(key, text: text, images: images, launcher: assistantLaunchers[key])
            if haptic { Haptics.tap() }
            return true
        } catch {
            show(error)
            return false
        }
    }

    /// The user looked at this session: finished work stops being "unread" (docs/agent-states.md).
    func markSeen(_ key: SessionKey) {
        sendSeen(key)
        // Optimistic: flip the local copy right away.
        if var list = sessionsByMachine[key.machineRef], let i = list.firstIndex(where: { $0.sessionID == key.sessionID }),
           list[i].state == .unread {
            list[i].state = .idle
            sessionsByMachine[key.machineRef] = list
        }
    }

    private func sendSeen(_ key: SessionKey) {
        if let socket = socket(key.group), socket.state == .connected {
            socket.send(["type": "seen", "machine_id": key.machineID, "session_id": key.sessionID])
        } else if let api = api(key.group) {
            Task { try? await api.post("/api/v1/sessions/\(key.machineID)/\(key.sessionID)/seen") }
        }
    }

    /// 全部标为已读: every finished-unread one of these, in one update of the list.
    func markSeen(_ keys: [SessionKey]) {
        let unread = keys.filter { session($0)?.state == .unread }
        guard !unread.isEmpty else { return }
        for key in unread { sendSeen(key) }
        var lists = sessionsByMachine
        for key in unread {
            if let i = lists[key.machineRef]?.firstIndex(where: { $0.sessionID == key.sessionID }) { lists[key.machineRef]![i].state = .idle }
        }
        sessionsByMachine = lists
        Haptics.success()
    }

    /// Other live agents in the same space as this session (herdr tabs of one workspace), slot order.
    func siblings(of key: SessionKey) -> [Session] {
        guard let s = session(key), let space = s.spaceKey else { return [] }
        // One per pane: a pane can briefly be listed under two session ids (/clear, a resume, a stale entry) and
        // showed up twice in the strip. The open one wins, then the latest.
        var byPane: [String: Session] = [:]
        var rest: [Session] = []
        for c in allSessions where c.spaceKey == space && c.state.isLive {
            guard let pane = c.paneID else { rest.append(c); continue }
            if let had = byPane[pane], had.key == key || (c.key != key && (had.updatedAt ?? .distantPast) >= (c.updatedAt ?? .distantPast)) {
                continue
            }
            byPane[pane] = c
        }
        return (Array(byPane.values) + rest)
            .sorted { ($0.slotIndex ?? 0, $0.sessionID) < ($1.slotIndex ?? 0, $1.sessionID) }
    }

    func interrupt(_ key: SessionKey) async {
        guard session(key)?.canInterrupt == true else { return }
        do { try await api(key.group)?.interrupt(key); Haptics.tap() } catch { show(error) }
    }

    func attach(_ key: SessionKey) async {
        guard session(key)?.canAttach == true, let api = api(key.group) else { return }
        do {
            try await api.attach(key)
            await refresh()
            toast = Toast("已实时接入", style: .success)
        } catch { show(error) }
    }

    /// Changes a session's permission mode / effort level (nil leaves one). Shown right away; the hub's next session
    /// list confirms it. Returns whether it worked.
    @discardableResult
    func setModes(_ key: SessionKey, permissionMode: String? = nil, effort: String? = nil) async -> Bool {
        guard allowSend(key), let api = api(key.group) else { return false }
        do {
            try await api.setModes(key, permissionMode: permissionMode, effort: effort)
            if var list = sessionsByMachine[key.machineRef], let i = list.firstIndex(where: { $0.sessionID == key.sessionID }) {
                if let permissionMode { list[i].permissionMode = permissionMode }
                if let effort { list[i].effort = effort }
                sessionsByMachine[key.machineRef] = list
            }
            Haptics.success()
            return true
        } catch {
            show(error)
            Haptics.error()
            return false
        }
    }

    func keys(_ key: SessionKey, _ keys: [String], text: String = "") async {
        guard session(key)?.canTerminal == true else { return }
        do { try await api(key.group)?.keys(key, keys: keys, text: text) } catch { show(error) }
    }

    @discardableResult
    func close(_ key: SessionKey) async -> Bool {
        guard let session = latest(key), session.canTerminal else {
            toast = Toast("这个 Agent 不支持从手机停止并关闭", style: .error)
            return false
        }
        guard let api = api(key.group) else {
            toast = Toast("未连接到这个 Agent 的 hub，标签已保留", style: .error)
            return false
        }
        do {
            try await api.close(key)
            func ended(_ list: [Session]) -> [Session] {
                list.map { item in
                    guard item.key == key || (session.paneID != nil && item.paneID == session.paneID) else { return item }
                    var item = item
                    item.status = .offline
                    item.state = .offline
                    return item
                }
            }
            if let list = sessionsByMachine[key.machineRef] { sessionsByMachine[key.machineRef] = ended(list) }
            if let list = pendingSessions[key.machineRef] { pendingSessions[key.machineRef] = ended(list) }
            toast = Toast("已停止并关闭 Agent，对话记录已保留", style: .success)
            Haptics.success()
            return true
        } catch {
            toast = Toast(error.isCancellation
                ? "尚未确认 Agent 已停止，标签已保留，请检查后重试"
                : "停止并关闭失败：" + error.localizedDescription, style: .error)
            Haptics.error()
            return false
        }
    }

    /// After a change in 设置 › 模型: the pickers read the machines' lists again.
    func forgetLaunchers() { launcherCache = [:] }

    func launchers(_ machine: MachineRef, refresh: Bool = false) async throws -> [Launcher] {
        if !refresh, let cached = launcherCache[machine] { return cached }
        guard let api = api(machine.group) else { return [] }
        let list = try await api.launchers(machine: machine.id)
        launcherCache[machine] = list
        return list
    }


    // MARK: Style

    /// How the app is drawn (设置 › 界面风格); RootView rebuilds everything when it changes.
    var style: AppStyle = AppStyle.current {
        didSet {
            AppStyle.current = style
            UserDefaults.standard.set(style.rawValue, forKey: AppearanceChoice.styleKey)
        }
    }

    /// The app's colors (设置 › 外观 › 主题色): the style's own, the persona's, or a fixed one; rebuilds everything too.
    var themeColor: ThemeColor = ThemeColor.current {
        didSet {
            ThemeColor.current = themeColor
            UserDefaults.standard.set(themeColor.rawValue, forKey: AppearanceChoice.colorKey)
        }
    }

    /// Where 设置 is (its pages), here and not in SettingsView's state: picking a style or a persona in 设置 › 外观
    /// rebuilds the whole tree (`look`), and the rebuilt settings stay on that page.
    var settingsPath = NavigationPath()

    /// The persona on screen: the chosen one, the built-in in 体验模式 (its packages are the user's hub's).
    var persona: Persona { isDemo ? .builtin : personas.current }

    /// What RootView keys the whole tree by: a new style, 主题色 or persona rebuilds it (tokens are static; a persona's
    /// palette is its own primary, and the colors under 跟随形象).
    var look: Look { Look(style: style, color: themeColor, palette: persona.palette) }

    nonisolated struct Look: Hashable {
        var style: AppStyle
        var color: ThemeColor
        var palette: PersonaPalette?
    }

    /// The accent the built-in persona is drawn in: the 主题色 as it is with no persona colors.
    var builtinAccent: Color { themeColor.colors(style: style, persona: nil).accent }

    /// The persona's palette into the spec (`StyleSpec.compose`), before RootView builds with it.
    private func applyPersonaPalette() {
        let palette = persona.palette
        if PersonaPalette.current != palette { PersonaPalette.current = palette }
    }

    /// Agents (not the assistant) waiting on the user, not muted: the Agents tab's badge, the shell's Agents.
    var agentsWaiting: Int { badges.agents }

    /// 设置 › 外观 › 旧布局 (2026-11-01, docs/ios.md「新外壳」): on, the iPhone keeps the four tabs exactly as before;
    /// off (the default), it is the ChatGPT-style shell (助手 | Agents, the drawer, 设置 as a sheet). Kept for a few days
    /// of transition. iPad, iPhone Duo open and the Mac always use their own layouts.
    var oldLayout: Bool = UserDefaults.standard.bool(forKey: "ui.oldLayout") {
        didSet { UserDefaults.standard.set(oldLayout, forKey: "ui.oldLayout") }
    }

    /// The shell's 设置 sheet is up. Here and not in the shell's state: picking a style in it rebuilds the whole tree
    /// (`look`), and the rebuilt shell puts the sheet back on the same page (`settingsPath`).
    var shellSettings = false

    /// 设置 › 外观 › App 图标: the home screen icon (the system keeps the choice; this mirrors it).
    let appIcon = AppIconModel.live()

    /// 小 / 标准 / 大 on top of the system's text size (设置 › 界面风格 › 字号); RootView applies it.
    var textSize: TextSize = TextSize.current {
        didSet { UserDefaults.standard.set(textSize.rawValue, forKey: "ui.textSize") }
    }

    /// 跟随系统 / 浅色 / 深色 (设置 › 外观 › 深浅色), on the app's windows; RootView puts it on a new window. Nothing is
    /// rebuilt (`look` stays): the tokens are dynamic colors and follow the window's style. The app icons that follow it
    /// (`AppIconChoice.followsScheme`: 纸色, the default, and 蜜桃) switch to that look too.
    var colorScheme: ColorSchemeChoice = ColorSchemeChoice.current {
        didSet {
            UserDefaults.standard.set(colorScheme.rawValue, forKey: ColorSchemeChoice.key)
            #if os(iOS)
            colorScheme.apply()
            // 纸色 / 蜜桃 on the home screen take the same look.
            Task { await appIcon.follow(colorScheme) }
            #endif
        }
    }

    // MARK: Snapshot

    nonisolated private struct DevSnapshot: Codable, Sendable {
        var machines: [Machine]
        var sessions: [Session]
        var interactions: [Interaction]
        var assistant: APIClient.AssistantState?
    }

    @ObservationIgnored private let snapshotWriter = Debouncer(.seconds(2))
    /// The other groups' Persona settings, kept for when one of them is picked (`assistantConfig` is the hub-level one).
    @ObservationIgnored private var assistantByGroup: [String: APIClient.AssistantState] = [:]

    /// Each group's snapshot: "dev" for the default group (what it always was), "dev.<group>" for the others.
    static func snapshotName(_ group: String) -> String { group.isEmpty ? "dev" : "dev." + group }

    private func restoreSnapshot() {
        var allMachines: [Machine] = [], allSessions: [Session] = [], allInteractions: [Interaction] = []
        for g in groupIDs {
            guard let snap = StateCache.load(Self.snapshotName(g), as: DevSnapshot.self) else { continue }
            allMachines += snap.machines.map { var m = $0; m.group = g; return m }
            allSessions += snap.sessions.map { var s = $0; s.group = g; return s }
            allInteractions += snap.interactions.map { var i = $0; i.group = g; return i }
            if let a = snap.assistant { assistantByGroup[g] = a }
        }
        machines = allMachines
        sessionsByMachine = Dictionary(grouping: allSessions, by: \.machineRef)
        interactions = Dictionary(allInteractions.map { ($0.uid, $0) }, uniquingKeysWith: { a, _ in a })
        assistantConfig = assistantByGroup[hub?.group ?? ""]
    }

    private func saveSnapshot() {
        snapshotWriter.run { [weak self] in
            guard let self, self.isPaired else { return }
            let hubGroup = self.hub?.group
            for g in self.groupIDs {
                let assistant = g == hubGroup ? self.assistantConfig : self.assistantByGroup[g]
                StateCache.save(Self.snapshotName(g), DevSnapshot(
                    machines: self.machines.filter { $0.group == g },
                    sessions: self.sessionsByMachine.filter { $0.key.group == g }.values.flatMap { $0 },
                    interactions: Array(self.interactions.values.filter { $0.group == g }), assistant: assistant))
            }
        }
    }

    /// What the app holds about groups that are no longer there.
    func forgetGroups(_ gone: Set<String>, keepingPushKeys: Bool = false) {
        guard !gone.isEmpty else { return }
        if !keepingPushKeys { LarkChatOutbox.forget(groups: gone) }
        machines = machines.filter { !gone.contains($0.group) }
        sessionsByMachine = sessionsByMachine.filter { !gone.contains($0.key.group) }
        interactions = interactions.filter { !gone.contains($0.value.group) }
        for key in chats.keys where gone.contains(key.group) { dropChat(key) }
        drafts = drafts.filter { !gone.contains($0.key.group) }
        if let k = openChat, gone.contains(k.group) { openChat = nil }
        if let k = assistantCurrent, gone.contains(k.group) { assistantCurrent = nil }
        for g in gone {
            StateCache.remove(Self.snapshotName(g))
            ChatCache.removeGroup(g)
            PinsStore.forget(suffix: g)
            if !keepingPushKeys { PushCrypto.removeKey(group: g) }
        }
        if links.contains(where: { $0.fleet != nil }) { LiveActivityController.shared.apply(mergedFleet()) }
        saveOutbox()
    }

    /// Another group was picked for the hub-level pages: their data starts over from that group's.
    func hubChanged() {
        if let g = hub?.group {
            assistantConfig = assistantByGroup[g]
        }
        if let a = assistantCurrent, a.group != hub?.group { assistantCurrent = assistantMain }
        InboxStore.shared.forget()
        TasksStore.shared.forget()
        InboxStore.shared.reload()
        TodoStore.shared.forget()
        TodoStore.shared.reload()
        Task {
            await refreshAssistant()
            await personas.refresh(api)
            if assistantCurrent == nil { assistantCurrent = assistantMain }
        }
        applyPersonaPalette()
    }

    // MARK: Badges

    /// The tab badges' numbers, counted here when what they count changes and published only when a number does
    /// (docs/ios-state.md「第 3 步」): the tabs read two numbers, not every pending question and its session.
    private(set) var badges = TabBadges()
    @ObservationIgnored private var watchingMutes = false

    private func countBadges() {
        var b = TabBadges()
        for ix in interactions.values {
            let s = sessionIndex[ix.sessionKey]
            if s?.assistant == true {
                b.assistant += 1
            } else if pinsStore(ix.sessionKey.group)?.isMuted(session: Self.pinKey(session: ix.sessionKey),
                                                              space: s?.spaceName.map { "\(ix.sessionKey.machineID)/\($0)" }) != true {
                b.agents += 1
            }
        }
        if b != badges { badges = b }
        if !watchingMutes { watchingMutes = true; watchMutes() }
    }

    /// Muting an agent or a space takes its questions off the Agents badge (each group's hub keeps its own mutes).
    private func watchMutes() {
        withObservationTracking { for l in links { _ = l.pins.mutes } } onChange: { [weak self] in
            Task { @MainActor in
                self?.countBadges()
                self?.watchMutes()
            }
        }
    }

    // MARK: Pins and mutes

    /// Pinned / muted spaces and agents: each group's hub keeps its own (`PinsStore`, one per group); the views ask
    /// through the methods below. Keys are the hub's ("machine/space name", "machine/session id"), without the group.
    func pinsStore(_ group: String) -> PinsStore? { link(group)?.pins }

    static func pinKey(space g: SpaceGroup) -> String { "\(g.machineID)/\(g.name)" }
    static func pinKey(session k: SessionKey) -> String { "\(k.machineID)/\(k.sessionID)" }

    func isPinned(_ g: SpaceGroup) -> Bool { pinsStore(g.group)?.isPinned(space: Self.pinKey(space: g)) == true }
    func isPinned(_ k: SessionKey) -> Bool { pinsStore(k.group)?.isPinned(session: Self.pinKey(session: k)) == true }
    func togglePin(_ g: SpaceGroup) { pinsStore(g.group)?.togglePin(space: Self.pinKey(space: g)) }
    func togglePin(_ k: SessionKey) { pinsStore(k.group)?.togglePin(session: Self.pinKey(session: k)) }
    /// Where a pinned agent sits among its group's pinned agents (the newest pin first).
    func pinOrder(_ k: SessionKey) -> Int { pinsStore(k.group)?.pins.sessions.firstIndex(of: Self.pinKey(session: k)) ?? 0 }
    func loadPins() async {
        for link in links { await link.pins.load() }
    }

    func isMuted(_ g: SpaceGroup) -> Bool { pinsStore(g.group)?.isMuted(space: Self.pinKey(space: g)) == true }
    /// Muted: the agent itself, or its space unless the agent was turned back on there (same rule as the hub).
    func isMuted(_ k: SessionKey) -> Bool {
        pinsStore(k.group)?.isMuted(session: Self.pinKey(session: k), space: spaceKey(k)) == true
    }
    func isMuted(_ s: Session) -> Bool {
        pinsStore(s.group)?.isMuted(session: Self.pinKey(session: s.key), space: s.spaceName.map { "\(s.machineID)/\($0)" }) == true
    }
    private func spaceKey(_ k: SessionKey) -> String? { session(k)?.spaceName.map { "\(k.machineID)/\($0)" } }
    func toggleMute(_ g: SpaceGroup) { pinsStore(g.group)?.toggleMute(space: Self.pinKey(space: g)) }
    func toggleMute(_ k: SessionKey) { pinsStore(k.group)?.toggleMute(session: Self.pinKey(session: k), space: spaceKey(k)) }


    // MARK: Attachments

    /// Images handed to a chat's composer from elsewhere (an image edited in the viewer); the composer takes them.
    var composerInbox: [SessionKey: [UIImage]] = [:]
    /// Page excerpts picked as context for a chat's next message (`<jarvis-context>`).
    var composerContexts: [SessionKey: [ContextRef]] = [:]

    // MARK: Renames

    /// Renames a space (herdr workspace) on its machine; shown at once, the hub keeps pins / mutes with it.
    func renameSpace(_ g: SpaceGroup, to name: String) async {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let api = api(g.group), !name.isEmpty, name != g.name else { return }
        let oldKey = Self.pinKey(space: g), newKey = "\(g.machineID)/\(name)"
        edit(machine: g.machineRef) { s in if s.spaceID == g.spaceID { s.spaceName = name } }
        pinsStore(g.group)?.renameSpace(oldKey, to: newKey)
        do { try await api.renameSpace(machine: g.machineID, workspaceID: g.spaceID, name: name) } catch { show(error) }
        await refresh()
    }

    /// Renames an agent's herdr tab and / or its Claude session name (nil leaves that one alone).
    func renameSession(_ k: SessionKey, tab: String?, title: String?) async {
        guard session(k)?.canTerminal == true, let api = api(k.group) else { return }
        let tab = tab?.trimmingCharacters(in: .whitespacesAndNewlines), title = title?.trimmingCharacters(in: .whitespacesAndNewlines)
        edit(machine: k.machineRef) { s in
            guard s.sessionID == k.sessionID else { return }
            if let tab, !tab.isEmpty { s.slotName = tab }
            if let title, !title.isEmpty { s.title = title }
        }
        do {
            try await api.renameSession(k, tabName: tab?.isEmpty == false ? tab : nil, title: title?.isEmpty == false ? title : nil)
            toast = Toast("已重命名", style: .success)
        } catch { show(error) }
        await refresh()
    }

    /// Archives an assistant conversation or brings it back: shown at once, the hub's push confirms it.
    func archiveConversation(_ k: SessionKey, _ archived: Bool) async {
        guard let api = api(k.group) else { return }
        guard !archived || k != assistantMain else { return } // the hub refuses the main one too
        edit(machine: k.machineRef) { s in if s.sessionID == k.sessionID { s.archived = archived } }
        Haptics.tap()
        do {
            try await api.archiveConversation(k.sessionID, archived: archived)
            toast = Toast(archived ? "已归档" : "已取消归档", style: .success)
        } catch {
            edit(machine: k.machineRef) { s in if s.sessionID == k.sessionID { s.archived = !archived } }
            show(error)
        }
    }

    private func edit(machine: MachineRef, _ change: (inout Session) -> Void) {
        guard var list = sessionsByMachine[machine] else { return }
        for i in list.indices { change(&list[i]) }
        sessionsByMachine[machine] = list
    }

    // MARK: Loops

    /// Asks the agent to end its /loop: nothing outside claude can cancel a scheduled wakeup or its cron job,
    /// so the agent is told to (it wakes up for the message and stops scheduling).
    func stopLoop(_ key: SessionKey) {
        sendNow(key, text: "停止当前的 /loop：调用 ScheduleWakeup(stop: true) 结束循环（如果用 CronCreate 建过定时任务，用 CronDelete 删掉），"
            + "不要再安排下一次唤醒，回复一句「循环已停止」即可。")
    }

    /// Everything again from every group's hub (pull to refresh, coming back to the app).
    func refresh() async {
        guard let api else { return }
        Task { await loadPins() }
        Task {
            let writes = muteWrites
            if let p = try? await api.notifyPrefs(device: PushRegistrar.deviceID) {
                if writes == muteWrites { muteWhileDesktop = p.muteWhileDesktop } // not over a switch made meanwhile
                #if DEBUG
                if UserDefaults.standard.bool(forKey: "showDesktopSample") { muteWhileDesktop = true } // the demo hub's prefs say off
                #endif
                Translator.shared.syncPrefs(hubHas: p.translate)
                LiveActivityController.shared.settings(p)
            }
            var any = false
            for link in links {
                if let fleet = try? await link.api.fleet() { link.fleet = fleet; any = true }
            }
            if any { LiveActivityController.shared.apply(mergedFleet()) }
        }
        await withTaskGroup(of: Void.self) { tasks in
            for link in links { tasks.addTask { await self.refresh(link) } }
        }
    }

    /// One group's machines, sessions and interactions.
    private func refresh(_ link: HubLink) async {
        let api = link.api, group = link.group
        do {
            async let m = api.machines()
            async let s = api.sessions()
            async let i = api.interactions()
            let (machines, sessions, ixs) = try await (m, s, i)
            guard self.link(group) === link else { return } // removed meanwhile
            replaceMachines(group, machines)
            let rest = Dictionary(grouping: sessions, by: \.machineRef)
            var lists = sessionsByMachine.filter { $0.key.group != group }
            if link.live.state == .connected && link.features.contains(SessionPatch.feature) {
                // What the socket pushed is newer than this answer: only the Codex history comes from it.
                for (ref, list) in rest {
                    lists[ref] = SessionPatch.refreshed(list, over: sessionsByMachine[ref] ?? [], connectedToDeltaHub: true)
                    if let p = pendingSessions[ref] { pendingSessions[ref] = SessionPatch.refreshed(list, over: p, connectedToDeltaHub: true) }
                }
            } else {
                for (ref, list) in rest { lists[ref] = list }
            }
            if lists != sessionsByMachine { sessionsByMachine = lists }
            replaceInteractions(group, ixs)
            link.live.ensureConnected()
        } catch {
            // The title says 等待网络 / 连接中…; with several groups one unreachable is not an error to show.
            if !error.isOffline, !isMultiHub { show(error) }
        }
    }


    func show(_ error: Error) {
        if error.isOffline { notDone(); return }
        if (error as? APIError)?.code == DemoText.code { toast = Toast(DemoText.notSent); return } // 体验模式: not an error
        guard !error.isQuiet else { return } // leaving a screen cancels its loads, a timeout gets retried: not shown
        toast = Toast(error.localizedDescription, style: .error)
        Haptics.error()
    }

    @ObservationIgnored private var lastNotDone = Date.distantPast

    /// Something the user did needed the hub while it is out of reach: one calm line, at most every few seconds (the
    /// title already says why), instead of a red error per tap.
    func notDone() {
        guard Date.now.timeIntervalSince(lastNotDone) > 4 else { return }
        lastNotDone = .now
        toast = Toast(connection.networkUp ? "连不上 hub，刚才的操作没有生效" : "没有网络，刚才的操作没有生效", style: .info)
        Haptics.attention()
    }

    func watchSummary() -> WatchSummary {
        let sessions = allSessions.filter { $0.status.isLive }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
        // The watch does not draw connectors' items (GitHub…) yet.
        let inbox = InboxStore.shared.items.filter { !$0.isDone && $0.triage == .action && $0.connector == nil }
        return WatchSummary(sessions: Array(sessions.prefix(20)), interactions: pendingInteractions, machines: machines,
                            inbox: Array(inbox.prefix(20)))
    }
}

enum AppTab: String, Hashable {
    case assistant, chats, mail, settings
    #if os(iOS)
    case feishu
    #endif
}

struct PhoneConfirmation: Identifiable {
    let id = UUID()
    let summary: String
    let respond: (Bool) -> Void
}

struct Toast: Identifiable, Equatable {
    enum Style { case info, success, error }
    let id = UUID()
    let message: String
    let style: Style

    init(_ message: String, style: Style = .info) {
        self.message = message
        self.style = style
    }

    static func == (a: Toast, b: Toast) -> Bool { a.id == b.id }
}

enum Haptics {
    static func tap() { UIImpactFeedbackGenerator(style: .light).impactOccurred() }
    static func success() { UINotificationFeedbackGenerator().notificationOccurred(.success) }
    static func error() { UINotificationFeedbackGenerator().notificationOccurred(.error) }
    static func attention() { UINotificationFeedbackGenerator().notificationOccurred(.warning) }
}

/// One session's latest state, observed on its own (`AppModel.session(_:)`). Its `rev`, `updatedAt` and last line are
/// as of its last other change: a push of only those (most of a working agent's) draws nothing that reads the cell
/// (`Session.sameForItsPage`); what shows them reads the lists (`allSessions`, `AgentsFeed.Row`), what sends them
/// `AppModel.latest(_:)`.
@Observable final class SessionCell {
    var value: Session?
    init(_ value: Session?) { self.value = value }
}

/// Questions and approvals waiting, as the tab bar counts them: the Persona tab's (its conversations') and the Agents
/// tab's (the agents', muted ones left out).
nonisolated struct TabBadges: Equatable, Sendable {
    var assistant = 0
    var agents = 0
}

/// Who shows the app's tabs, told of each new one as it is set (`AppModel.tabRouter`): the iPhone's shell.
@MainActor protocol TabRouter: AnyObject {
    func route(_ tab: AppTab, in model: AppModel)
}

/// Who shows the app's toasts instead of `ToastOverlay` (`AppModel.toastSink`): the iPhone's shell.
@MainActor protocol ToastSink: AnyObject {
    func take(_ toast: Toast)
}
