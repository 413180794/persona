import Foundation
import Observation
import SwiftUI
import UIKit
import UserNotifications

/// App-wide state: server pairing, live connection, machines, sessions, interactions and per-session chats.
@Observable
final class AppModel {
    static let shared = AppModel()

    /// Set by pair / unpair and when the phone gets its own credential (DeviceCredentials.swift).
    var config: ServerConfig?
    let live: LiveConnection
    /// What the titles say about the link (等待网络 / 连接中… / 更新中…).
    let connection: ConnectionStatus
    private(set) var machines: [Machine] = [] { didSet { saveSnapshot() } }
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
            saveSnapshot()
        }
    }
    private(set) var allSessions: [Session] = []
    private var sessionIndex: [SessionKey: Session] = [:]
    private(set) var interactions: [String: Interaction] = [:] { didSet { saveSnapshot() } }
    var toast: Toast?
    var openChat: SessionKey?
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

    init() {
        let live = LiveConnection()
        self.live = live
        connection = ConnectionStatus(live: live)
        config = Keychain.serverConfig
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
            case .delta(_, let text): _ = MarkdownMemo.content(text + " ▍")
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
        live.onConnected = { [weak self] in self?.didConnect() }
        live.onChange = { [weak self] in self?.connection.update() }
        connection.onCaughtUp = { [weak self] in self?.flushOutbox() }
    }

    #if DEBUG
    func adopt(_ cfg: ServerConfig) {
        config = cfg
        if machines.isEmpty { restoreSnapshot() }
    }
    #endif

    func start() {
        guard let config else { return }
        live.connect(config)
        WatchSync.shared.push(config: Keychain.watchConfig ?? config)
        Task { await upgradeCredentials() }
    }

    func pair(_ new: ServerConfig) async throws {
        let client = APIClient(config: new)
        _ = try await client.machines()
        config = new
        Keychain.serverConfig = new
        forgetDeviceCredentials()
        machines = []
        sessionsByMachine = [:]
        interactions = [:]
        live.disconnect()
        live.connect(new)
        WatchSync.shared.push(config: new)
        PushRegistrar.shared.registerIfPossible()
        Task { await upgradeCredentials() }
    }

    func unpair() {
        live.disconnect()
        LiveActivityController.shared.endAll() // its buttons would talk to a hub we no longer know
        config = nil
        Keychain.serverConfig = nil
        forgetDeviceCredentials()
        machines = []
        sessionsByMachine = [:]
        interactions = [:]
        chats = [:]
        subscribers = [:]
        shown = []
        saveOutbox()
    }

    // MARK: Derived

    func session(_ key: SessionKey) -> Session? {
        sessionIndex[key]
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
        allSessions.filter(\.assistant).sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
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
            // Routines and the assistant's sense of "now" follow the phone's time zone.
            let tz = TimeZone.current.identifier
            if st.timezone != tz, let updated = try? await api.setAssistant(timezone: tz) { assistantConfig = updated }
        }
    }

    /// The main conversation (the Jarvis tab's long-running one), when the hub has one.
    var assistantMain: SessionKey? {
        guard let st = assistantConfig, let id = st.main, !id.isEmpty else { return nil }
        return SessionKey(machineID: st.machineID, sessionID: id)
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
            toast = Toast("新对话将使用 \(launcher)", style: .success)
        } catch { show(error) }
    }

    /// Routes a notification / deep link to the right tab.
    func open(_ key: SessionKey) {
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
        var msg: [String: Any] = ["type": "terminal_input", "machine_id": key.machineID, "session_id": key.sessionID]
        if !text.isEmpty { msg["text"] = text }
        if let grid { msg["control"] = ["cols": grid.cols, "rows": grid.rows] }
        live.send(msg)
    }

    /// The mouse wheel over a terminal the app drives, in lines (> 0 back in history), over a cell of its screen: herdr
    /// gives it to a program that takes the mouse (Claude Code, Codex scroll their conversation) and scrolls its own
    /// scrollback for one that does not — what a herdr client does with the wheel, as in Moshi.
    func terminalScroll(_ key: SessionKey, lines: Int, at: (col: Int, row: Int)?) {
        guard lines != 0 else { return }
        var msg: [String: Any] = ["type": "terminal_input", "machine_id": key.machineID, "session_id": key.sessionID, "scroll": lines]
        if let at { msg["at"] = [at.col, at.row] }
        live.send(msg)
    }

    /// A tap on a terminal the app drives: a left click at that cell of its screen (0-based).
    func terminalClick(_ key: SessionKey, column: Int, row: Int) {
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
        AgentSync.shared.schedule()
        LarkSync.shared.reconnected()
        InboxStore.shared.reload()
        // 更新中… until the open chats' backlog and the pending interactions are in (ConnectionStatus).
        connection.startCatchUp(["interactions"] + subscribed.map(Self.catchUpPart))
        for key in subscribed {
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
        live.send(["type": "register_phone", "device_name": UIDevice.current.name, "tools": arr, "simulator": simulator])
    }

    private func handle(_ ev: ServerEvent) {
        switch ev {
        case .hello(let features):
            hubFeatures = Set(features)
            for handler in terminalHandlers.values { handler(.reconnected) }
        case .terminal(let key, let event):
            terminalHandlers[key]?(event)
        case .machines(let list):
            // A machine that just came back: re-subscribe its open chats (their backlog failed while it was down).
            let wasOffline = Set(machines.filter { !$0.online }.map(\.id))
            machines = list
            for m in list where m.online && wasOffline.contains(m.id) {
                for key in subscribed where key.machineID == m.id {
                    chat(key).error = nil
                    live.send(subscribeMessage(key))
                }
            }
        case .sessions(let machineID, let list):
            let old = sessionsByMachine[machineID] ?? []
            if old != list { sessionsByMachine[machineID] = list }
            WatchSync.shared.push(summary: watchSummary())
            notifyTransitions(old: old, new: list)
            AgentSync.shared.sessions(list)
        case .revs(let machineID, let revs):
            AgentSync.shared.revs(machine: machineID, revs)
        case .lark(let feedIDs):
            LarkSync.shared.received(feedIDs)
        case .inbox:
            InboxStore.shared.reload()
        case .fleet(let state):
            LiveActivityController.shared.apply(state)
        case .items(let key, let items, let backlog, let hasMore, let span, let moreAfter):
            if backlog { connection.caughtUp(Self.catchUpPart(key)) }
            let store = chat(key)
            settle(key, store.apply(items, span: span, backlog: backlog, hasMore: hasMore, moreAfter: moreAfter))
            // The streamed message landed in the transcript: stop drawing its live copy.
            if !store.liveText.isEmpty, items.contains(where: { $0.role == "assistant" && $0.kind == .text && ($0.text ?? "").hasPrefix(store.liveText) }) {
                store.liveText = ""
            }
        case .screen(let key, let text, let status):
            let c = chat(key)
            c.screen = text
            c.screenStatus = status
            c.screenUpdatedAt = .now
        case .delta(let key, let text):
            chat(key).liveText = text
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
        return await withCheckedContinuation { cont in
            pendingConfirmation = PhoneConfirmation(summary: summary) { cont.resume(returning: $0) }
        }
    }

    // MARK: Actions

    func resolve(_ ix: Interaction, _ r: Resolution) async {
        guard let api else { return }
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
        guard let api, let queue = session(key)?.queued, queue.indices.contains(index) else { return }
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

    /// Puts the message on screen immediately and sends it in the background; a failure marks it for retry.
    func sendNow(_ key: SessionKey, text: String, images: [ImagePayload] = []) {
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
        guard let i = chat(key).pending.firstIndex(where: { $0.id == msg.id }) else { return }
        chat(key).pending[i].state = .sending
        deliver(key, msg, clearDraft: clearDraft)
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
        guard let api else { return false }
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
        do { try await api?.interrupt(key); Haptics.tap() } catch { show(error) }
    }

    /// Changes a session's permission mode / effort level (nil leaves one). Shown right away; the hub's next session
    /// list confirms it. Returns whether it worked.
    @discardableResult
    func setModes(_ key: SessionKey, permissionMode: String? = nil, effort: String? = nil) async -> Bool {
        guard let api else { return false }
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
        do { try await api?.keys(key, keys: keys, text: text) } catch { show(error) }
    }

    func close(_ key: SessionKey) async {
        do { try await api?.close(key); toast = Toast("已关闭会话", style: .success) } catch { show(error) }
    }

    func launchers(_ machine: String, refresh: Bool = false) async throws -> [Launcher] {
        if !refresh, let cached = launcherCache[machine] { return cached }
        guard let api else { return [] }
        let list = try await api.launchers(machine: machine)
        launcherCache[machine] = list
        return list
    }

    // MARK: Style

    /// The app's look (设置 › 界面风格); RootView rebuilds everything when it changes.
    var style: AppStyle = AppStyle.current {
        didSet {
            AppStyle.current = style
            UserDefaults.standard.set(style.rawValue, forKey: "ui.style")
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

    // MARK: Pins

    /// Pinned spaces / agents (saved on the hub; cached here so the list orders right before it answers).
    var pins: Pins = {
        guard let data = UserDefaults.standard.data(forKey: "dev.pins"), let p = try? JSON.decoder.decode(Pins.self, from: data) else { return Pins() }
        return p
    }() {
        didSet { if let data = try? JSON.encoder.encode(pins) { UserDefaults.standard.set(data, forKey: "dev.pins") } }
    }

    static func pinKey(space g: SpaceGroup) -> String { "\(g.machineID)/\(g.name)" }
    static func pinKey(session k: SessionKey) -> String { "\(k.machineID)/\(k.sessionID)" }

    func isPinned(_ g: SpaceGroup) -> Bool { pins.spaces.contains(Self.pinKey(space: g)) }
    func isPinned(_ k: SessionKey) -> Bool { pins.sessions.contains(Self.pinKey(session: k)) }

    func togglePin(_ g: SpaceGroup) { updatePins { p in Self.toggle(&p.spaces, Self.pinKey(space: g)) } }
    func togglePin(_ k: SessionKey) { updatePins { p in Self.toggle(&p.sessions, Self.pinKey(session: k)) } }

    private static func toggle(_ list: inout [String], _ key: String) {
        if let i = list.firstIndex(of: key) { list.remove(at: i) } else { list.insert(key, at: 0) }
    }

    private func updatePins(_ change: (inout Pins) -> Void) {
        Haptics.tap()
        var p = pins
        change(&p)
        withAnimation(.snappy) { pins = p } // optimistic; the hub keeps the truth
        Task { do { try await api?.setPins(p) } catch { show(error) } }
    }

    func loadPins() async {
        if let p = try? await api?.pins() { pins = p }
        if let m = try? await api?.mutes() { mutes = m }
    }

    // MARK: Mutes

    /// Muted spaces / agents: the hub sends no notifications about them and the list shows them quietly.
    var mutes: Pins = {
        guard let data = UserDefaults.standard.data(forKey: "dev.mutes"), let p = try? JSON.decoder.decode(Pins.self, from: data) else { return Pins() }
        return p
    }() {
        didSet { if let data = try? JSON.encoder.encode(mutes) { UserDefaults.standard.set(data, forKey: "dev.mutes") } }
    }

    func isMuted(_ g: SpaceGroup) -> Bool { mutes.spaces.contains(Self.pinKey(space: g)) }
    /// Muted: the agent itself, or its space unless the agent was turned back on there (same rule as the hub).
    func isMuted(_ k: SessionKey) -> Bool { mutes.mutes(session: Self.pinKey(session: k), space: spaceKey(k)) }

    func isMuted(_ s: Session) -> Bool {
        mutes.mutes(session: Self.pinKey(session: s.key), space: s.spaceName.map { "\(s.machineID)/\($0)" })
    }

    private func spaceKey(_ k: SessionKey) -> String? { session(k)?.spaceName.map { "\(k.machineID)/\($0)" } }

    func toggleMute(_ g: SpaceGroup) { updateMutes { p in Self.toggle(&p.spaces, Self.pinKey(space: g)) } }
    /// One agent's own switch, on top of its space's: muting adds it to the muted agents; turning it on inside a
    /// muted space adds it to the allowed ones.
    func toggleMute(_ k: SessionKey) {
        let key = Self.pinKey(session: k), space = spaceKey(k)
        updateMutes { p in p = p.togglingMute(session: key, space: space) }
    }

    private func updateMutes(_ change: (inout Pins) -> Void) {
        Haptics.tap()
        var p = mutes
        change(&p)
        withAnimation(.snappy) { mutes = p }
        Task { do { try await api?.setMutes(p) } catch { show(error) } }
    }

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
        pins.spaces = pins.spaces.map { $0 == oldKey ? newKey : $0 }
        mutes.spaces = mutes.spaces.map { $0 == oldKey ? newKey : $0 }
        do { try await api.renameSpace(machine: g.machineID, workspaceID: g.spaceID, name: name) } catch { show(error) }
        await refresh()
    }

    /// Renames an agent's herdr tab and / or its Claude session name (nil leaves that one alone).
    func renameSession(_ k: SessionKey, tab: String?, title: String?) async {
        guard let api else { return }
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
            if let p = try? await api.notifyPrefs(device: PushRegistrar.deviceID) {
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
            sessionsByMachine = Dictionary(grouping: sessions, by: \.machineID)
            interactions = Dictionary(ixs.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
            live.ensureConnected()
        } catch {
            if !error.isOffline { show(error) } // the title says 等待网络 / 连接中…
        }
    }

    func show(_ error: Error) {
        if error.isOffline { notDone(); return }
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
