import Foundation
import Observation
import SwiftUI
import UIKit
import UserNotifications

/// App-wide state: server pairing, live connection, machines, sessions, interactions and per-session chats.
@Observable
final class AppModel {
    static let shared = AppModel()

    private(set) var config: ServerConfig?
    let live = LiveConnection()
    private(set) var machines: [Machine] = [] { didSet { saveSnapshot() } }
    private(set) var sessionsByMachine: [String: [Session]] = [:] { didSet { saveSnapshot() } }
    private(set) var interactions: [String: Interaction] = [:] { didSet { saveSnapshot() } }
    var toast: Toast?
    var openChat: SessionKey?
    var pendingConfirmation: PhoneConfirmation?
    /// Remembered across launches together with the open dev chat (restoreChat), so a relaunch lands where you were.
    var selectedTab: AppTab = .assistant {
        didSet { UserDefaults.standard.set(selectedTab.rawValue, forKey: "app.tab") }
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

    @ObservationIgnored private var chats: [SessionKey: ChatStore] = [:]
    @ObservationIgnored private var subscribed = Set<SessionKey>()
    @ObservationIgnored private(set) var launcherCache: [String: [Launcher]] = [:]

    var api: APIClient? { config.map { APIClient(config: $0) } }
    var isPaired: Bool { config != nil }

    init() {
        config = Keychain.serverConfig
        // Last known machines, agents and pending interactions: the lists are there offline and on a cold start.
        if config != nil { restoreSnapshot() }
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
        WatchSync.shared.configProvider = { Keychain.serverConfig }
        WatchSync.shared.onActivated = { [weak self] in
            guard let self, let cfg = self.config else { return }
            WatchSync.shared.push(config: cfg)
            WatchSync.shared.push(summary: self.watchSummary())
        }
        live.onConnected = { [weak self] in self?.didConnect() }
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
        WatchSync.shared.push(config: config)
    }

    func pair(_ new: ServerConfig) async throws {
        let client = APIClient(config: new)
        _ = try await client.machines()
        config = new
        Keychain.serverConfig = new
        machines = []
        sessionsByMachine = [:]
        interactions = [:]
        live.disconnect()
        live.connect(new)
        WatchSync.shared.push(config: new)
        PushRegistrar.shared.registerIfPossible()
    }

    func unpair() {
        live.disconnect()
        config = nil
        Keychain.serverConfig = nil
        machines = []
        sessionsByMachine = [:]
        interactions = [:]
        chats = [:]
        subscribed = []
    }

    // MARK: Derived

    var allSessions: [Session] { sessionsByMachine.values.flatMap { $0 } }

    func session(_ key: SessionKey) -> Session? {
        sessionsByMachine[key.machineID]?.first { $0.sessionID == key.sessionID }
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
            if assistantCurrent == nil, neverChosen, let newest = st.conversations.max(by: { ($0.updatedAt ?? .distantPast) < ($1.updatedAt ?? .distantPast) }) {
                assistantCurrent = SessionKey(machineID: st.machineID, sessionID: newest.sessionID)
            }
            // Routines and the assistant's sense of "now" follow the phone's time zone.
            let tz = TimeZone.current.identifier
            if st.timezone != tz, let updated = try? await api.setAssistant(timezone: tz) { assistantConfig = updated }
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
        if let c = chats[key] { return c }
        let c = ChatStore(key: key)
        chats[key] = c
        return c
    }

    // MARK: Subscriptions

    func subscribe(_ key: SessionKey) {
        subscribed.insert(key)
        let store = chat(key)
        var msg: [String: Any] = ["type": "subscribe", "machine_id": key.machineID, "session_id": key.sessionID, "lazy_images": true]
        if store.lastSeq > 0 { msg["since_seq"] = store.lastSeq }
        live.send(msg)
        if live.state != .connected && !store.loadedOnce { Task { await loadInitial(key) } }
    }

    func isSubscribed(_ key: SessionKey) -> Bool { subscribed.contains(key) }
    /// The chat's store if one is in memory (never creates one: background sync writes to the disk cache instead).
    func existingChat(_ key: SessionKey) -> ChatStore? { chats[key] }

    func unsubscribe(_ key: SessionKey) {
        subscribed.remove(key)
        live.send(["type": "unsubscribe", "machine_id": key.machineID, "session_id": key.sessionID])
    }

    private func loadInitial(_ key: SessionKey) async {
        guard let api else { return }
        do {
            let res = try await api.items(key, beforeSeq: nil)
            chat(key).merge(res.items, backlog: true, hasMore: res.hasMore)
        } catch {
            chat(key).error = error.shown
        }
    }

    func loadOlder(_ key: SessionKey) async {
        let store = chat(key)
        guard let api, store.hasMore, !store.isLoadingOlder, let first = store.items.first else { return }
        store.isLoadingOlder = true
        defer { store.isLoadingOlder = false }
        do {
            let res = try await api.items(key, beforeSeq: first.seq)
            store.prepend(res.items, hasMore: res.hasMore)
        } catch {
            show(error)
        }
    }

    // MARK: Events

    private func didConnect() {
        registerPhone()
        AgentSync.shared.schedule()
        LarkSync.shared.reconnected()
        InboxStore.shared.reload()
        for key in subscribed {
            var msg: [String: Any] = ["type": "subscribe", "machine_id": key.machineID, "session_id": key.sessionID, "lazy_images": true]
            let seq = chat(key).lastSeq
            if seq > 0 { msg["since_seq"] = seq }
            live.send(msg)
        }
    }

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
        case .hello:
            break
        case .machines(let list):
            // A machine that just came back: re-subscribe its open chats (their backlog failed while it was down).
            let wasOffline = Set(machines.filter { !$0.online }.map(\.id))
            machines = list
            for m in list where m.online && wasOffline.contains(m.id) {
                for key in subscribed where key.machineID == m.id {
                    chat(key).error = nil
                    var msg: [String: Any] = ["type": "subscribe", "machine_id": key.machineID, "session_id": key.sessionID, "lazy_images": true]
                    if chat(key).lastSeq > 0 { msg["since_seq"] = chat(key).lastSeq }
                    live.send(msg)
                }
            }
        case .sessions(let machineID, let list):
            let old = sessionsByMachine[machineID] ?? []
            sessionsByMachine[machineID] = list
            WatchSync.shared.push(summary: watchSummary())
            notifyTransitions(old: old, new: list)
            AgentSync.shared.sessions(list)
        case .revs(let machineID, let revs):
            AgentSync.shared.revs(machine: machineID, revs)
        case .lark(let feedIDs):
            LarkSync.shared.received(feedIDs)
        case .inbox:
            InboxStore.shared.reload()
        case .items(let key, let items, let backlog, let hasMore):
            chat(key).merge(items, backlog: backlog, hasMore: hasMore)
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
        interactions[ix.id] = nil
        do {
            try await api.resolve(ix.id, r)
            Haptics.success()
        } catch {
            if (error as? APIError)?.status != 410 { interactions[ix.id] = ix }
            show(error)
        }
    }

    /// Unsent composer text per chat (also where an edited queued message lands).
    var drafts: [SessionKey: String] = [:]

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
        Haptics.tap()
        deliver(key, msg)
    }

    func retry(_ key: SessionKey, _ msg: PendingMessage) {
        guard let i = chat(key).pending.firstIndex(where: { $0.id == msg.id }) else { return }
        chat(key).pending[i].state = .sending
        deliver(key, msg)
    }

    func discard(_ key: SessionKey, _ msg: PendingMessage) {
        chat(key).pending.removeAll { $0.id == msg.id }
    }

    private func deliver(_ key: SessionKey, _ msg: PendingMessage) {
        Task {
            let ok = await send(key, text: msg.text, images: msg.images, haptic: false)
            let store = chat(key)
            guard let i = store.pending.firstIndex(where: { $0.id == msg.id }) else { return }
            store.pending[i].state = ok ? .sent : .failed
            guard ok else { return }
            // Normally the transcript shows it within a second or two (or it lands in the queue); never linger.
            try? await Task.sleep(for: .seconds(60))
            store.pending.removeAll { $0.id == msg.id }
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

    /// Other live agents in the same space as this session (herdr tabs of one workspace), slot order.
    func siblings(of key: SessionKey) -> [Session] {
        guard let s = session(key), let space = s.spaceKey else { return [] }
        return allSessions.filter { $0.spaceKey == space && $0.state.isLive }
            .sorted { ($0.slotIndex ?? 0, $0.sessionID) < ($1.slotIndex ?? 0, $1.sessionID) }
    }

    func interrupt(_ key: SessionKey) async {
        do { try await api?.interrupt(key); Haptics.tap() } catch { show(error) }
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
            show(error)
        }
    }

    func show(_ error: Error) {
        guard !error.isCancellation else { return } // leaving a screen cancels its loads: not an error
        toast = Toast(error.localizedDescription, style: .error)
        Haptics.error()
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
