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
    private(set) var machines: [Machine] = []
    private(set) var sessionsByMachine: [String: [Session]] = [:]
    private(set) var interactions: [String: Interaction] = [:]
    var toast: Toast?
    var openChat: SessionKey?
    var pendingConfirmation: PhoneConfirmation?
    var selectedTab: AppTab = .assistant
    /// Session whose chat is on screen; its completion pings are suppressed.
    var visibleSession: SessionKey?
    private(set) var assistantConfig: APIClient.AssistantState?
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
        if let v = UserDefaults.standard.string(forKey: "assistant.current"), let slash = v.firstIndex(of: "/") {
            assistantCurrent = SessionKey(machineID: String(v[..<slash]), sessionID: String(v[v.index(after: slash)...]))
        }
        live.onEvent = { [weak self] in self?.handle($0) }
        live.onConnected = { [weak self] in self?.didConnect() }
    }

    #if DEBUG
    func adopt(_ cfg: ServerConfig) { config = cfg }
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
        }
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
        var msg: [String: Any] = ["type": "subscribe", "machine_id": key.machineID, "session_id": key.sessionID]
        if store.lastSeq > 0 { msg["since_seq"] = store.lastSeq }
        live.send(msg)
        if live.state != .connected && !store.loadedOnce { Task { await loadInitial(key) } }
    }

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
            chat(key).error = error.localizedDescription
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
        for key in subscribed {
            var msg: [String: Any] = ["type": "subscribe", "machine_id": key.machineID, "session_id": key.sessionID]
            let seq = chat(key).lastSeq
            if seq > 0 { msg["since_seq"] = seq }
            live.send(msg)
        }
    }

    func registerPhone() {
        let tools = PhoneToolRegistry.enabledSpecs()
        guard let data = try? JSON.encoder.encode(tools),
              let arr = try? JSONSerialization.jsonObject(with: data) else { return }
        live.send(["type": "register_phone", "device_name": UIDevice.current.name, "tools": arr])
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
                    var msg: [String: Any] = ["type": "subscribe", "machine_id": key.machineID, "session_id": key.sessionID]
                    if chat(key).lastSeq > 0 { msg["since_seq"] = chat(key).lastSeq }
                    live.send(msg)
                }
            }
        case .sessions(let machineID, let list):
            let old = sessionsByMachine[machineID] ?? []
            sessionsByMachine[machineID] = list
            WatchSync.shared.push(summary: watchSummary())
            notifyTransitions(old: old, new: list)
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

    /// Launcher chosen in an assistant chat for its next messages (model switch).
    var assistantLaunchers: [SessionKey: String] = [:]

    func send(_ key: SessionKey, text: String, images: [ImagePayload] = []) async -> Bool {
        guard let api else { return false }
        do {
            try await api.send(key, text: text, images: images, launcher: assistantLaunchers[key])
            Haptics.tap()
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

    func refresh() async {
        guard let api else { return }
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
        toast = Toast(error.localizedDescription, style: .error)
        Haptics.error()
    }

    func watchSummary() -> WatchSummary {
        let sessions = allSessions.filter { $0.status.isLive }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
        return WatchSummary(sessions: Array(sessions.prefix(20)), interactions: pendingInteractions, machines: machines)
    }
}

enum AppTab: Hashable {
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
