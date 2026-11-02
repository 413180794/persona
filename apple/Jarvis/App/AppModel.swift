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

    /// Set by pair / unpair and when the phone gets its own credential (DeviceCredentials.swift).
    var config: ServerConfig? { didSet { if config?.isDemo != oldValue?.isDemo { applyPersonaPalette() } } }
    /// Jarvis's looks (docs/persona.md): the chosen one, what the hub offers, the packages on the phone.
    let personas = PersonaStore()
    /// Which paths conversations mention are there on their machines (only those are linked).
    let filePresence = FilePresence()
    /// Which machine ports links point at are listening (docs/ports.md).
    let portPresence = PortPresence()
    let live: LiveConnection
    /// What the titles say about the link (等待网络 / 连接中… / 更新中…).
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
    private(set) var sessionsByMachine: [String: [Session]] = [:] {
        didSet {
            allSessions = sessionsByMachine.values.flatMap { $0 }
            var index: [SessionKey: Session] = [:]
            for (machine, sessions) in sessionsByMachine {
                for session in sessions {
                    let key = SessionKey(machineID: machine, sessionID: session.sessionID)
                    if index[key] == nil { index[key] = session }
                }
            }
            sessionIndex = index
            for (key, cell) in sessionCells where cell.value != index[key] { cell.value = index[key] }
            saveSnapshot()
        }
    }
    private(set) var allSessions: [Session] = []
    @ObservationIgnored private var sessionIndex: [SessionKey: Session] = [:]
    /// One observable cell per session a view has looked up (`session(_:)`): a view showing one session is drawn again
    /// when that session changes, not on every push about any of them (the open chat and the tab badges were redrawn
    /// for each machine's list, 2026-10-31). `allSessions` is still there for views about all of them.
    @ObservationIgnored private var sessionCells: [SessionKey: SessionCell] = [:]
    private(set) var interactions: [String: Interaction] = [:] { didSet { saveSnapshot() } }
    var pairingEngineMachineID: String?
    var toast: Toast?
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
            #if PERF_SIGNPOST
            PerfTrace.event("Tab selection", selectedTab.rawValue)
            #endif
        }
    }

    /// The dev chat that was open; saved when a chat opens, cleared when going back to the list.
    static func rememberChat(_ key: SessionKey?) {
        if let key { UserDefaults.standard.set("\(key.machineID)/\(key.sessionID)", forKey: "dev.openChat") }
        else { UserDefaults.standard.removeObject(forKey: "dev.openChat") }
    }
    /// Session whose chat is on screen; its completion pings are suppressed.
    var visibleSession: SessionKey?
    private(set) var assistantConfig: APIClient.AssistantState? { didSet { saveSnapshot() } }
    /// The assistant conversation shown in the 对话 tab (persisted across launches).
    var assistantCurrent: SessionKey? {
        didSet {
            let v = assistantCurrent.map { "\($0.machineID)/\($0.sessionID)" } ?? ""
            UserDefaults.standard.set(v, forKey: "assistant.current")
        }
    }
    var debugShowNewChat = false
    /// Bumped whenever another page hands the user to a Jarvis conversation (`askJarvis`): the Jarvis tab goes back
    /// to its conversation even when it is the one already shown (a pushed 知识库 page would otherwise stay on top).
    var assistantReveal = 0

    @ObservationIgnored private(set) var chats: [SessionKey: ChatStore] = [:]
    @ObservationIgnored var flushingOutbox = false
    @ObservationIgnored let outboxWriter = Debouncer(.milliseconds(300))
    @ObservationIgnored private(set) var launcherCache: [String: [Launcher]] = [:]

    var api: APIClient? { config.map { APIClient(config: $0) } }
    var isPaired: Bool { config != nil }
    /// 体验模式: the made-up hub inside the app (DemoHub), no server at all. Remembered across launches until the
    /// user leaves it or pairs.
    var isDemo: Bool { config?.isDemo == true }
    private static let demoKey = "demo.on"

    init() {
        let live = LiveConnection()
        self.live = live
        connection = ConnectionStatus(live: live)
        DemoHub.install()
        #if DEBUG
        // `-openWelcome YES`: start unpaired and out of 体验模式, on the pairing page (DemoModeTests).
        if UserDefaults.standard.bool(forKey: "openWelcome") {
            Keychain.serverConfig = nil
            UserDefaults.standard.removeObject(forKey: Self.demoKey)
        }
        #endif
        config = Keychain.serverConfig ?? (UserDefaults.standard.bool(forKey: Self.demoKey) ? .demo : nil)
        // Last known machines, agents and pending interactions: the lists are there offline and on a cold start.
        if config != nil { restoreSnapshot(); restoreOutbox() }
        if let raw = UserDefaults.standard.string(forKey: "app.tab"), let tab = AppTab(rawValue: raw) {
            selectedTab = tab
        }
        if selectedTab == .chats, let v = UserDefaults.standard.string(forKey: "dev.openChat"), let slash = v.firstIndex(of: "/") {
            openChat = SessionKey(machineID: String(v[..<slash]), sessionID: String(v[v.index(after: slash)...]))
        }
        if let v = UserDefaults.standard.string(forKey: "assistant.current"), let slash = v.firstIndex(of: "/") {
            assistantCurrent = SessionKey(machineID: String(v[..<slash]), sessionID: String(v[v.index(after: slash)...]))
        }
        live.onEvent = { [weak self] in self?.handle($0) }
        live.prepareEvent = { event in
            switch event {
            // What `StreamingText` asks for: the block being written (the finished ones are memoized as they finish).
            case .delta(_, let text): _ = MarkdownMemo.content(StreamChunks.split(text).tail + " ▍")
            case .interaction(let interaction):
                if let plan = interaction.plan { _ = MarkdownMemo.content(plan) }
            default: break
            }
        }
        WatchSync.shared.configProvider = { Keychain.watchConfig ?? Keychain.serverConfig }
        WatchSync.shared.onActivated = { [weak self] in
            guard let self, let cfg = self.config else { return }
            WatchSync.shared.push(config: Keychain.watchConfig ?? cfg)
            WatchSync.shared.push(summary: self.watchSummary())
        }
        personas.onChange = { [weak self] in self?.applyPersonaPalette() }
        applyPersonaPalette()
        live.onConnected = { [weak self] in self?.didConnect() }
        live.onChange = { [weak self] in self?.connection.update() }
        connection.onCaughtUp = { [weak self] in self?.flushOutbox() }
        DemoHub.shared.onNotSent = { [weak self] in self?.toast = Toast(DemoText.notSent) }
    }

    #if DEBUG
    func adopt(_ cfg: ServerConfig) {
        config = cfg
        if machines.isEmpty { restoreSnapshot() }
    }
    #endif

    func start() {
        guard let config else { return }
        #if os(iOS)
        // Launched in the background (a push, a file from the watch): the socket waits for the app to open.
        live.suspended = UIApplication.shared.applicationState == .background
        #endif
        live.connect(config)
        if config.isDemo { return } // no watch, no device credentials: nothing to talk to
        WatchSync.shared.push(config: Keychain.watchConfig ?? config)
        Task { await upgradeCredentials() }
    }

    func pair(_ new: ServerConfig) async throws {
        let client = APIClient(config: new)
        let pairedMachines = try await client.machines()
        if isDemo { leaveDemo() } // a real hub ends 体验模式
        config = new
        Keychain.serverConfig = new
        forgetDeviceCredentials()
        machines = pairedMachines
        // The engine step only when the hub reports engines (an older hub cannot save one; the step must not trap).
        pairingEngineMachineID = EngineSelectionModel.pairingMachine(pairedMachines).flatMap { $0.engines.isEmpty ? nil : $0.id }
        sessionsByMachine = [:]
        pendingSessions = [:] // a burst from the hub left before this
        interactions = [:]
        desktops = []
        muteWhileDesktop = false
        live.disconnect()
        live.connect(new)
        WatchSync.shared.push(config: new)
        PushRegistrar.shared.registerIfPossible()
        Task { await upgradeCredentials() }
    }

    /// 体验模式 from the pairing page: every tab runs on DemoHub's made-up data, nothing is sent anywhere.
    func enterDemo() {
        guard config == nil else { return }
        DemoHub.shared.reset()
        InboxStore.shared.forget()
        forgetDemoChats()
        chats = [:]
        assistantCurrent = DemoHub.shared.mainConversation // Jarvis opens on a conversation, not a blank new one
        UserDefaults.standard.set(true, forKey: Self.demoKey)
        config = .demo
        machines = []
        sessionsByMachine = [:]
        pendingSessions = [:] // a burst from the hub left before this
        interactions = [:]
        desktops = []
        muteWhileDesktop = false
        selectedTab = .chats
        live.connect(.demo)
        toast = Toast("已进入体验模式，数据都是编造的")
    }

    /// Out of 体验模式 (退出 or pairing): what it showed goes with it.
    private func leaveDemo() {
        UserDefaults.standard.set(false, forKey: Self.demoKey)
        live.disconnect()
        InboxStore.shared.forget()
        forgetDemoChats()
        chats = [:]
        subscribers = [:]
        shown = []
    }

    /// What the demo's conversations left behind: drafts, and cached chats (a new demo starts over).
    private func forgetDemoChats() {
        let demo = DemoHub.shared.sessionKeys
        drafts = drafts.filter { !demo.contains($0.key) }
        if let k = assistantCurrent, demo.contains(k) { assistantCurrent = nil }
        if let k = openChat, demo.contains(k) { openChat = nil }
        for key in demo { ChatCache.remove(key) }
    }

    func unpair() {
        pairingEngineMachineID = nil
        if isDemo { leaveDemo() }
        live.disconnect()
        LiveActivityController.shared.endAll() // its buttons would talk to a hub we no longer know
        config = nil
        Keychain.serverConfig = nil
        forgetDeviceCredentials()
        machines = []
        sessionsByMachine = [:]
        pendingSessions = [:] // a burst from the hub left before this
        interactions = [:]
        desktops = []
        muteWhileDesktop = false
        chats = [:]
        subscribers = [:]
        shown = []
        saveOutbox()
    }

    /// The PUT response updates immediately; subsequent machines pushes remain authoritative.
    func applyMachine(_ machine: Machine) {
        guard let index = machines.firstIndex(where: { $0.id == machine.id }) else { return }
        machines[index] = machine
    }

    // MARK: Derived

    func session(_ key: SessionKey) -> Session? {
        if let cell = sessionCells[key] { return cell.value }
        let cell = SessionCell(sessionIndex[key])
        sessionCells[key] = cell
        return cell.value
    }

    /// A session this app just started, until the hub's next list for its machine (which replaces it) has it: its
    /// chat opened with no title (「对话 · 对话」) and no machine until then.
    func adopt(_ s: Session) {
        guard session(s.key) == nil else { return }
        sessionsByMachine[s.machineID, default: []].append(s)
    }

    func machine(_ id: String) -> Machine? { machines.first { $0.id == id } }
    /// The machine the hub runs on (it reports itself as "local"): where the assistant lives by default.
    var hubMachineID: String { machines.first { $0.kind == "local" }?.id ?? machines.first?.id ?? "" }

    var pendingInteractions: [Interaction] {
        interactions.values.sorted { ($0.createdAt ?? .distantPast) < ($1.createdAt ?? .distantPast) }
    }

    var assistantConversations: [Session] {
        allSessions.filter { $0.assistant && $0.archived != true }.sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
    }

    /// The archived ones (已归档), newest first.
    var archivedConversations: [Session] {
        allSessions.filter { $0.assistant && $0.archived == true }.sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
    }

    /// Loads the assistant config; with no remembered conversation, picks up the newest one.
    func refreshAssistant() async {
        guard let api else { return }
        if let st = try? await api.assistant() {
            assistantConfig = st
            // Only on the very first launch: after "新对话" the key exists but is empty, and the home page stays.
            let neverChosen = UserDefaults.standard.object(forKey: "assistant.current") == nil
            if assistantCurrent == nil, neverChosen {
                if let main = assistantMain {
                    assistantCurrent = main
                } else if let newest = st.conversations.max(by: { ($0.updatedAt ?? .distantPast) < ($1.updatedAt ?? .distantPast) }) {
                    assistantCurrent = SessionKey(machineID: st.machineID, sessionID: newest.sessionID)
                }
            }
            // Routines and the assistant's sense of "now" follow this device's time zone (the Mac app writes its
            // own too; the hub keeps whichever came last).
            let tz = TimeZone.current.identifier
            if st.timezone != tz, let updated = try? await api.setAssistant(timezone: tz) { assistantConfig = updated }
        }
    }

    /// The main conversation (the Jarvis tab's long-running one), when the hub has one.
    var assistantMain: SessionKey? {
        guard let st = assistantConfig, let id = st.main, !id.isEmpty else { return nil }
        return SessionKey(machineID: st.machineID, sessionID: id)
    }

    /// Shows a Jarvis conversation from another page (定时任务's 立即运行 / its own conversation): the Jarvis tab or the
    /// shell's 助手, back at the conversation even from a page pushed on it or the shell's 设置 sheet.
    func openAssistantConversation(_ key: SessionKey) {
        shellSettings = false
        selectedTab = .assistant
        assistantCurrent = key
        assistantReveal += 1
    }

    /// Sends text to Jarvis' main conversation (starting it when there is none) and opens the Jarvis tab: how other
    /// pages hand something to Jarvis with its context.
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
            toast = Toast("Jarvis 下一条消息起使用 \(launcher)", style: .success)
        } catch { show(error) }
    }

    /// Routes a notification / deep link to the right tab; `latestAnswer` lands on the start of the agent's last answer.
    func open(_ key: SessionKey, latestAnswer: Bool = false) {
        if latestAnswer { latestAnswerFor = key }
        if session(key)?.assistant == true {
            selectedTab = .assistant
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
        live.send(subscribeMessage(key))
        if live.state != .connected && !store.loadedOnce { Task { await loadInitial(key) } }
    }

    func unsubscribe(_ key: SessionKey, by owner: UUID) {
        guard var owners = subscribers[key], owners.remove(owner) != nil else { return }
        guard owners.isEmpty else { subscribers[key] = owners; return }
        subscribers[key] = nil
        live.send(["type": "unsubscribe", "machine_id": key.machineID, "session_id": key.sessionID])
    }

    func isSubscribed(_ key: SessionKey) -> Bool { subscribers[key] != nil }

    // MARK: Terminal streams

    /// What the hub listed in its hello: `terminal_stream` means the terminal tab can subscribe to live frames.
    private(set) var hubFeatures: Set<String> = []
    /// The desktop apps (the Mac app) connected to the hub: the iPhone's 「Mac 上的 Jarvis 已登录」 (DesktopPresence.swift).
    private(set) var desktops: [DesktopDevice] = []
    /// 手机静音: this device's NotifyPrefs.muteWhileDesktop, shown on that banner.
    private(set) var muteWhileDesktop = false
    @ObservationIgnored private var muteWrites = 0
    /// The terminal views streaming each session (one at a time; a newer one replaces the handler).
    @ObservationIgnored private var terminalHandlers: [SessionKey: (TerminalEvent) -> Void] = [:]

    var canStreamTerminal: Bool { hubFeatures.contains("terminal_stream") }
    /// The hub can let the phone drive a terminal at its own size (`terminal_control`).
    var canControlTerminal: Bool { hubFeatures.contains("terminal_control") }
    /// The hub takes the wheel over a terminal the app drives (terminal_input scroll) to herdr.
    var canScrollTerminal: Bool { hubFeatures.contains("terminal_scroll") }
    /// The hub takes a tap on a terminal the app drives (terminal_input click) to herdr as a click.
    var canClickTerminal: Bool { hubFeatures.contains("terminal_click") }
    var isLive: Bool { live.state == .connected }

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
        live.send(msg)
    }

    /// Typed into a terminal the phone drives: the bytes go straight to it (in order, over the WebSocket). grid: the
    /// terminal's new size (the keyboard came up, the font changed).
    func terminalInput(_ key: SessionKey, _ text: String, grid: TerminalGridSize? = nil) {
        guard session(key)?.canTerminal == true else { return }
        var msg: [String: Any] = ["type": "terminal_input", "machine_id": key.machineID, "session_id": key.sessionID]
        if !text.isEmpty { msg["text"] = text }
        if let grid { msg["control"] = ["cols": grid.cols, "rows": grid.rows] }
        live.send(msg)
    }

    /// The mouse wheel over a terminal the app drives, in lines (> 0 back in history), over a cell of its screen: herdr
    /// gives it to a program that takes the mouse (Claude Code, Codex scroll their conversation) and scrolls its own
    /// scrollback for one that does not — what a herdr client does with the wheel, as in Moshi.
    func terminalScroll(_ key: SessionKey, lines: Int, at: (col: Int, row: Int)?) {
        guard session(key)?.canTerminal == true, lines != 0 else { return }
        var msg: [String: Any] = ["type": "terminal_input", "machine_id": key.machineID, "session_id": key.sessionID, "scroll": lines]
        if let at { msg["at"] = [at.col, at.row] }
        live.send(msg)
    }

    /// A tap on a terminal the app drives: a left click at that cell of its screen (0-based).
    func terminalClick(_ key: SessionKey, column: Int, row: Int) {
        guard session(key)?.canTerminal == true else { return }
        live.send(["type": "terminal_input", "machine_id": key.machineID, "session_id": key.sessionID, "click": [column, row]])
    }

    func terminalUnsubscribe(_ key: SessionKey) {
        guard terminalHandlers.removeValue(forKey: key) != nil else { return }
        live.send(["type": "terminal_unsubscribe", "machine_id": key.machineID, "session_id": key.sessionID])
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
        guard let api else { return }
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
        guard let api, !filling.contains(key) else { return }
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
        guard let api, let page = try? await api.items(key, beforeSeq: nil) else { return }
        chat(key).replace(with: page.items, hasMore: page.hasMore, span: page.span)
    }

    /// Fetches the work a fold stands for into the chat, once at a time per fold (several rows of one open work line
    /// may ask together).
    func unfold(_ key: SessionKey, _ fold: Item) async throws {
        guard let api else { return }
        let id = "\(key.machineID)/\(key.sessionID)/\(fold.id)"
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

    func loadOlder(_ key: SessionKey) async {
        let store = chat(key)
        guard let api, store.hasMore, !store.isLoadingOlder, let first = store.items.first else { return }
        store.isLoadingOlder = true
        defer { store.isLoadingOlder = false }
        do {
            let res = try await api.items(key, beforeSeq: first.seq)
            store.prepend(res.items, hasMore: res.hasMore)
        } catch {
            if !error.isOffline { show(error) } // scrolling up while offline: the title says why nothing comes
        }
    }

    // MARK: Events

    private func didConnect() {
        registerPhone()
        Task { await personas.refresh(api) }
        AgentSync.shared.schedule()
        LarkSync.shared.reconnected()
        InboxStore.shared.reload()
        // 更新中… until the open chats' backlog and the pending interactions are in (ConnectionStatus).
        connection.startCatchUp(["interactions"] + subscribed.map(Self.catchUpPart))
        for key in subscribed {
            deltas.drop(key)
            chat(key).liveText = "" // a delta that cleared it may have been lost with the old socket
            live.send(subscribeMessage(key))
        }
        // Questions answered on the computer while the socket was down are gone from the hub: drop their cards.
        Task {
            defer { connection.caughtUp("interactions") }
            guard let ixs = try? await api?.interactions() else { return }
            interactions = Dictionary(ixs.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
            WatchSync.shared.push(summary: watchSummary())
        }
    }

    private static func catchUpPart(_ key: SessionKey) -> String { "chat:\(key.machineID)/\(key.sessionID)" }

    func registerPhone() {
        if isDemo { return } // nobody would call the phone's tools
        #if DEBUG
        // Simulator automation (scripts/sim.sh) must not take the real iPhone's phone_* tool calls.
        if UserDefaults.standard.string(forKey: "pairURL") != nil { return }
        #endif
        let tools = PhoneToolRegistry.enabledSpecs()
        guard let data = try? JSON.encoder.encode(tools),
              let arr = try? JSONSerialization.jsonObject(with: data) else { return }
        #if targetEnvironment(simulator)
        let simulator = true
        #else
        let simulator = false
        #endif
        live.send(Self.registration(deviceName: UIDevice.current.name, tools: arr, simulator: simulator, deviceID: PushRegistrar.deviceID))
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
        ["type": "register_desktop", "device_name": deviceName]
        #else
        ["type": "register_phone", "device_name": deviceName, "tools": tools, "simulator": simulator, "device_id": deviceID]
        #endif
    }

    /// Streamed replies drawn at most 5 times a second (`DeltaPacer`).
    @ObservationIgnored private var deltas = DeltaPacer()
    /// Session lists received and not applied yet (`applyPendingSessions`), by machine.
    @ObservationIgnored private var pendingSessions: [String: [Session]] = [:]
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
    func applySessions(_ byMachine: [String: [Session]]) {
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

    private func handle(_ ev: ServerEvent) {
        #if DEBUG || PERF_SIGNPOST
        PerfTrace.push(Self.perfName(ev))
        #endif
        switch ev {
        case .hello(let features):
            hubFeatures = Set(features)
            #if DEBUG
            // `-showDesktopSample YES`: a Mac signed in (and 手机静音 on), for screenshots of the banner (the demo hub sends no desktops).
            if UserDefaults.standard.bool(forKey: "showDesktopSample") {
                desktops = [DesktopDevice(device: "dev_sample", name: "MacBook Pro", since: Date().addingTimeInterval(-3600))]
                muteWhileDesktop = true
            }
            #endif
            for handler in terminalHandlers.values { handler(.reconnected) }
        case .terminal(let key, let event):
            terminalHandlers[key]?(event)
        case .machines(let list):
            // A machine that just came back: re-subscribe its open chats (their backlog failed while it was down).
            let wasOffline = Set(machines.filter { !$0.online }.map(\.id))
            // Every view that names a machine reads the list: the same list again is no change.
            if machines != list { machines = list }
            for m in list where m.online && wasOffline.contains(m.id) {
                for key in subscribed where key.machineID == m.id {
                    chat(key).error = nil
                    live.send(subscribeMessage(key))
                }
            }
        case .sessions(let machineID, let list):
            // A busy hub sends each machine's list on its own, one after another: they are applied together a moment
            // later, so the pages that read the sessions update once for the lot (one SwiftUI pass, not one a machine).
            let old = pendingSessions[machineID] ?? sessionsByMachine[machineID] ?? []
            pendingSessions[machineID] = SessionPatch.snapshot(list, over: old, deltaHub: hubFeatures.contains(SessionPatch.feature))
            scheduleSessionsFlush()
        case .sessionsDelta(let machineID, let upsert, let remove):
            // Over the newest the app has: a list still waiting for the flush, or what is shown.
            let base = pendingSessions[machineID] ?? sessionsByMachine[machineID] ?? []
            pendingSessions[machineID] = SessionPatch.apply(base, upsert: upsert, remove: remove)
            scheduleSessionsFlush()
        case .revs(let machineID, let revs):
            AgentSync.shared.revs(machine: machineID, revs)
        case .lark(let feedIDs):
            LarkSync.shared.received(feedIDs)
        case .agents(let ids):
            PersonalAgentSync.shared.received(ids)
        case .inbox:
            InboxStore.shared.reload()
        case .fleet(let state):
            LiveActivityController.shared.apply(state)
        case .desktops(let list):
            desktops = list
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
            let isNew = interactions[ix.id] == nil
            interactions[ix.id] = ix
            if isNew { Haptics.attention() }
            WatchSync.shared.push(summary: watchSummary())
        case .interactionResolved(let id):
            interactions[id] = nil
            WatchSync.shared.push(summary: watchSummary())
        case .toolCall(let callID, let name, let args):
            Task { await runPhoneTool(callID: callID, name: name, args: args) }
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
        case .fleet: "fleet"
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

    private func runPhoneTool(callID: String, name: String, args: JSONValue) async {
        let (content, isError) = await PhoneToolRegistry.call(name: name, args: args) { [weak self] summary in
            await self?.confirm(summary) ?? false
        }
        guard let data = try? JSON.encoder.encode(content), let arr = try? JSONSerialization.jsonObject(with: data) else { return }
        live.send(["type": "tool_result", "call_id": callID, "content": arr, "is_error": isError])
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
        guard session(ix.sessionKey)?.canApprove == true, let api else { return }
        // No network: the card stays as it is (taking it away and bringing it back a moment later read as "done").
        guard connection.networkUp else { notDone(); return }
        interactions[ix.id] = nil
        do {
            try await api.resolve(ix.id, r)
            Haptics.success()
        } catch let e as APIError where e.code == "prompt_changed" {
            // Nothing pressed: the terminal's dialog changed. The hub sends the card for what is there now.
            Haptics.attention()
            toast = Toast("终端上的对话框已经变了，这次没有按任何键：看看新的再答", style: .error)
        } catch {
            if (error as? APIError)?.status != 410 { interactions[ix.id] = ix }
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
        guard allowSend(key), let api, let queue = session(key)?.queued, queue.indices.contains(index) else { return }
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
            if let s = sessionsByMachine[key.machineID]?.first(where: { $0.paneID == pane && $0.sessionID != key.sessionID && $0.state.isLive }) {
                return SessionKey(machineID: key.machineID, sessionID: s.sessionID)
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
            let outcome: SendOutcome = isLive && !live.stalled ? await sendPending(key, msg, clearDraft: clearDraft) : .waiting
            settleSend(key, msg.id, outcome)
        }
    }
    func send(_ key: SessionKey, text: String, images: [ImagePayload] = [], haptic: Bool = true) async -> Bool {
        guard allowSend(key), let api else { return false }
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
        if live.state == .connected {
            live.send(["type": "seen", "machine_id": key.machineID, "session_id": key.sessionID])
        } else if let api {
            Task { try? await api.post("/api/v1/sessions/\(key.machineID)/\(key.sessionID)/seen") }
        }
        // Optimistic: flip the local copy right away.
        if var list = sessionsByMachine[key.machineID], let i = list.firstIndex(where: { $0.sessionID == key.sessionID }),
           list[i].state == .unread {
            list[i].state = .idle
            sessionsByMachine[key.machineID] = list
        }
    }

    /// 全部标为已读: every finished-unread one of these, in one update of the list.
    func markSeen(_ keys: [SessionKey]) {
        let unread = keys.filter { session($0)?.state == .unread }
        guard !unread.isEmpty else { return }
        for key in unread {
            if live.state == .connected {
                live.send(["type": "seen", "machine_id": key.machineID, "session_id": key.sessionID])
            } else if let api {
                Task { try? await api.post("/api/v1/sessions/\(key.machineID)/\(key.sessionID)/seen") }
            }
        }
        var lists = sessionsByMachine
        for key in unread {
            if let i = lists[key.machineID]?.firstIndex(where: { $0.sessionID == key.sessionID }) { lists[key.machineID]![i].state = .idle }
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
        do { try await api?.interrupt(key); Haptics.tap() } catch { show(error) }
    }

    func attach(_ key: SessionKey) async {
        guard session(key)?.canAttach == true, let api else { return }
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
        guard allowSend(key), let api else { return false }
        do {
            try await api.setModes(key, permissionMode: permissionMode, effort: effort)
            if var list = sessionsByMachine[key.machineID], let i = list.firstIndex(where: { $0.sessionID == key.sessionID }) {
                if let permissionMode { list[i].permissionMode = permissionMode }
                if let effort { list[i].effort = effort }
                sessionsByMachine[key.machineID] = list
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
        do { try await api?.keys(key, keys: keys, text: text) } catch { show(error) }
    }

    func close(_ key: SessionKey) async {
        guard session(key)?.canTerminal == true else { return }
        do { try await api?.close(key); toast = Toast("已关闭会话", style: .success) } catch { show(error) }
    }

    /// After a change in 设置 › 模型: the pickers read the machines' lists again.
    func forgetLaunchers() { launcherCache = [:] }

    func launchers(_ machine: String, refresh: Bool = false) async throws -> [Launcher] {
        if !refresh, let cached = launcherCache[machine] { return cached }
        guard let api else { return [] }
        let list = try await api.launchers(machine: machine)
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
    var agentsWaiting: Int {
        pendingInteractions.filter { session($0.sessionKey)?.assistant != true && !isMuted($0.sessionKey) }.count
    }

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

    // MARK: Snapshot

    nonisolated private struct DevSnapshot: Codable, Sendable {
        var machines: [Machine]
        var sessions: [Session]
        var interactions: [Interaction]
        var assistant: APIClient.AssistantState?
    }

    @ObservationIgnored private let snapshotWriter = Debouncer(.seconds(2))

    private func restoreSnapshot() {
        guard let snap = StateCache.load("dev", as: DevSnapshot.self) else { return }
        machines = snap.machines
        sessionsByMachine = Dictionary(grouping: snap.sessions, by: \.machineID)
        interactions = Dictionary(snap.interactions.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        assistantConfig = snap.assistant
    }

    private func saveSnapshot() {
        snapshotWriter.run { [weak self] in
            guard let self, self.config != nil else { return }
            StateCache.save("dev", DevSnapshot(machines: self.machines, sessions: self.sessionsByMachine.values.flatMap { $0 },
                                               interactions: Array(self.interactions.values), assistant: self.assistantConfig))
        }
    }

    // MARK: Pins and mutes

    /// Pinned / muted spaces and agents (`PinsStore`); the views ask through the methods below.
    @ObservationIgnored private(set) lazy var pinsStore = PinsStore(deps: .init(api: { [weak self] in self?.api },
                                                                              report: { [weak self] in self?.show($0) }))
    var pins: Pins {
        get { pinsStore.pins }
        set { pinsStore.pins = newValue }
    }
    var mutes: Pins {
        get { pinsStore.mutes }
        set { pinsStore.mutes = newValue }
    }

    static func pinKey(space g: SpaceGroup) -> String { "\(g.machineID)/\(g.name)" }
    static func pinKey(session k: SessionKey) -> String { "\(k.machineID)/\(k.sessionID)" }

    func isPinned(_ g: SpaceGroup) -> Bool { pinsStore.isPinned(space: Self.pinKey(space: g)) }
    func isPinned(_ k: SessionKey) -> Bool { pinsStore.isPinned(session: Self.pinKey(session: k)) }
    func togglePin(_ g: SpaceGroup) { pinsStore.togglePin(space: Self.pinKey(space: g)) }
    func togglePin(_ k: SessionKey) { pinsStore.togglePin(session: Self.pinKey(session: k)) }
    func loadPins() async { await pinsStore.load() }

    func isMuted(_ g: SpaceGroup) -> Bool { pinsStore.isMuted(space: Self.pinKey(space: g)) }
    /// Muted: the agent itself, or its space unless the agent was turned back on there (same rule as the hub).
    func isMuted(_ k: SessionKey) -> Bool { pinsStore.isMuted(session: Self.pinKey(session: k), space: spaceKey(k)) }
    func isMuted(_ s: Session) -> Bool {
        pinsStore.isMuted(session: Self.pinKey(session: s.key), space: s.spaceName.map { "\(s.machineID)/\($0)" })
    }
    private func spaceKey(_ k: SessionKey) -> String? { session(k)?.spaceName.map { "\(k.machineID)/\($0)" } }
    func toggleMute(_ g: SpaceGroup) { pinsStore.toggleMute(space: Self.pinKey(space: g)) }
    func toggleMute(_ k: SessionKey) { pinsStore.toggleMute(session: Self.pinKey(session: k), space: spaceKey(k)) }

    // MARK: Attachments

    /// Images handed to a chat's composer from elsewhere (an image edited in the viewer); the composer takes them.
    var composerInbox: [SessionKey: [UIImage]] = [:]
    /// Page excerpts picked as context for a chat's next message (`<jarvis-context>`).
    var composerContexts: [SessionKey: [ContextRef]] = [:]

    // MARK: Renames

    /// Renames a space (herdr workspace) on its machine; shown at once, the hub keeps pins / mutes with it.
    func renameSpace(_ g: SpaceGroup, to name: String) async {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let api, !name.isEmpty, name != g.name else { return }
        let oldKey = Self.pinKey(space: g), newKey = "\(g.machineID)/\(name)"
        edit(machine: g.machineID) { s in if s.spaceID == g.spaceID { s.spaceName = name } }
        pinsStore.renameSpace(oldKey, to: newKey)
        do { try await api.renameSpace(machine: g.machineID, workspaceID: g.spaceID, name: name) } catch { show(error) }
        await refresh()
    }

    /// Renames an agent's herdr tab and / or its Claude session name (nil leaves that one alone).
    func renameSession(_ k: SessionKey, tab: String?, title: String?) async {
        guard session(k)?.canTerminal == true, let api else { return }
        let tab = tab?.trimmingCharacters(in: .whitespacesAndNewlines), title = title?.trimmingCharacters(in: .whitespacesAndNewlines)
        edit(machine: k.machineID) { s in
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
        guard let api else { return }
        guard !archived || k != assistantMain else { return } // the hub refuses the main one too
        edit(machine: k.machineID) { s in if s.sessionID == k.sessionID { s.archived = archived } }
        Haptics.tap()
        do {
            try await api.archiveConversation(k.sessionID, archived: archived)
            toast = Toast(archived ? "已归档" : "已取消归档", style: .success)
        } catch {
            edit(machine: k.machineID) { s in if s.sessionID == k.sessionID { s.archived = !archived } }
            show(error)
        }
    }

    private func edit(machine: String, _ change: (inout Session) -> Void) {
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
            if let fleet = try? await api.fleet() { LiveActivityController.shared.apply(fleet) }
        }
        do {
            async let m = api.machines()
            async let s = api.sessions()
            async let i = api.interactions()
            let (machines, sessions, ixs) = try await (m, s, i)
            self.machines = machines
            let rest = Dictionary(grouping: sessions, by: \.machineID)
            if live.state == .connected && hubFeatures.contains(SessionPatch.feature) {
                // What the socket pushed is newer than this answer: only the Codex history comes from it.
                var lists = sessionsByMachine
                for (m, list) in rest {
                    lists[m] = SessionPatch.refreshed(list, over: lists[m] ?? [], connectedToDeltaHub: true)
                    if let p = pendingSessions[m] { pendingSessions[m] = SessionPatch.refreshed(list, over: p, connectedToDeltaHub: true) }
                }
                if lists != sessionsByMachine { sessionsByMachine = lists }
            } else {
                sessionsByMachine = rest
            }
            interactions = Dictionary(ixs.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
            live.ensureConnected()
        } catch {
            if !error.isOffline { show(error) } // the title says 等待网络 / 连接中…
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
        let inbox = InboxStore.shared.items.filter { !$0.isDone && $0.triage == .action }
        return WatchSummary(sessions: Array(sessions.prefix(20)), interactions: pendingInteractions, machines: machines,
                            inbox: Array(inbox.prefix(20)))
    }
}

enum AppTab: String, Hashable {
    case assistant, chats, mail, settings
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

/// One session's latest state, observed on its own (`AppModel.session(_:)`).
@Observable final class SessionCell {
    var value: Session?
    init(_ value: Session?) { self.value = value }
}
