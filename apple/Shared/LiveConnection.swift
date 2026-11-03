import Foundation
import Observation

/// One message of a terminal stream (docs/protocol.md「终端流」).
nonisolated enum TerminalEvent: Sendable {
    /// VT bytes for a cols × rows grid; a full one is the whole screen.
    case frame(epoch: String, seq: Int, full: Bool, cols: Int, rows: Int, data: Data)
    /// The hub dropped the stream; unsupported: read the screen instead of subscribing again.
    case ended(String, unsupported: Bool)
    /// The WebSocket connected again: subscribe again (continuing from the last frame).
    case reconnected
    /// How driving the terminal at the phone's size went (docs/terminal.md): active (cols × rows), busy (another
    /// program holds it: ask before taking over), taken / lost (back to watching), unsupported (only watching).
    case control(state: String, cols: Int?, rows: Int?, why: String?)
}

nonisolated enum ServerEvent: Sendable {
    /// features: what the hub supports beyond the first protocol (proto.Features); which hub it is and its group
    /// (docs/plan-hub-mesh.md 2.6); its build and protocol (docs/hub-update.md).
    case hello(HubHello)
    case machines([Machine])
    case sessions(machineID: String, [Session])
    /// What changed in a machine's list since the last (a hub that has `sessions_delta`, asked for on connect): the
    /// sessions new or different, whole, and the ids gone (docs/protocol.md「会话增量」).
    case sessionsDelta(machineID: String, upsert: [Session], remove: [String])
    /// hasMore is nil when the event cannot know (a catch-up after since_seq): only pages from the end tell. moreAfter:
    /// the hub capped a catch-up, more came after these.
    case items(SessionKey, [Item], backlog: Bool, hasMore: Bool?, span: ItemSpan, moreAfter: Bool)
    case screen(SessionKey, text: String, status: SessionStatus)
    case delta(SessionKey, text: String)
    case interaction(Interaction)
    case interactionResolved(String)
    case toolCall(callID: String, name: String, args: JSONValue)
    case hookEvent(SessionKey, event: String, message: String?, lastText: String?)
    /// Transcripts that grew on a machine (session id → rev): pull them before they are opened.
    case revs(machineID: String, [String: Int64])
    /// Feishu conversations with new or changed messages (lark-connector's realtime events, relayed by the hub).
    case lark(feedIDs: [String])
    /// Personal agent sessions (lark-connector agent-bridge, hub/agents.go) that changed: `<source>:<sessionKey>` ids, empty
    /// for "something changed, re-pull the list".
    case agents(sessionIDs: [String])
    /// The unified inbox changed (hub/inbox.go): pull it.
    case inbox
    /// The Fleet Live Activity's content changed (hub/liveactivity.go).
    case fleet(FleetState)
    /// The hub updating itself: checking, downloading, waiting for idle, restarting… (docs/hub-update.md).
    case hubUpdate(HubUpdateStatus)
    /// The desktop apps connected to the hub now (sent on connect and whenever one comes or goes).
    case desktops([DesktopDevice])
    /// Tasks from an assistant conversation (docs/unified-entry.md): the todos changed (pull them), a conversation's
    /// feed grew, a plan changed.
    case todos
    case assistantFeed(FeedItem)
    case taskPlan(TaskPlan)
    case terminal(SessionKey, TerminalEvent)
    case error(SessionKey?, String)

    private struct Envelope: Decodable {
        var type: String
        var machineID: String?
        var sessionID: String?
        var machines: [Machine]?
        var sessions: [Session]?
        var upsert: [Session]?
        var remove: [String]?
        var items: [Item]?
        var backlog: Bool?
        var hasMore: Bool?
        var moreAfter: Bool?
        var history: String?
        var fromSeq: Int?
        var toSeq: Int?
        var text: String?
        var status: SessionStatus?
        var interaction: Interaction?
        var id: String?
        var callID: String?
        var name: String?
        var args: JSONValue?
        var event: String?
        var message: String?
        var lastAssistantMessage: String?
        var error: String?
        var revs: [String: Int64]?
        var feedIDs: [String]?
        var sessionIDs: [String]?
        var fleet: FleetState?
        var desktops: [DesktopDevice]?
        var features: [String]?
        var channel: String?
        var build: Int64?
        var protocolVersion: Int?
        var minAppBuild: Int?
        var update: HubUpdateStatus?
        var epoch: String?
        var seq: Int?
        var full: Bool?
        var cols: Int?
        var rows: Int?
        var data: String?
        var unsupported: Bool?
        var state: String?
        var hubID: String?
        var hubName: String?
        var mesh: MeshField?
        var item: FeedItem?
        var plan: TaskPlan?

        enum CodingKeys: String, CodingKey {
            case type, machines, sessions, upsert, remove, items, backlog, text, status, interaction, id, name, args, event, message, error, revs, fleet, history
            case features, epoch, seq, full, cols, rows, data, unsupported, state, desktops, mesh, channel, build, update, item, plan
            case hubID = "hub_id", hubName = "hub_name", protocolVersion = "protocol", minAppBuild = "min_app_build"
            case machineID = "machine_id", sessionID = "session_id", hasMore = "has_more", callID = "call_id",
                 moreAfter = "more_after", fromSeq = "from_seq", toSeq = "to_seq",
                 lastAssistantMessage = "last_assistant_message", feedIDs = "feed_ids", sessionIDs = "session_ids"
        }
    }

    /// The hello's `mesh`; anything else there is no mesh rather than a lost hello.
    private struct MeshField: Decodable {
        var hubs: [MeshHub]?
        enum CodingKeys: String, CodingKey { case hubs }
        init(from decoder: Decoder) throws {
            hubs = (try? decoder.container(keyedBy: CodingKeys.self)).flatMap { try? $0.decodeIfPresent([MeshHub].self, forKey: .hubs) }
        }
    }

    static func decode(_ data: Data) -> ServerEvent? {
        guard let e = try? JSON.decoder.decode(Envelope.self, from: data) else { return nil }
        let key = e.machineID.flatMap { m in e.sessionID.map { SessionKey(machineID: m, sessionID: $0) } }
        switch e.type {
        case "hello": return .hello(HubHello(features: e.features ?? [], hubID: e.hubID, hubName: e.hubName, mesh: e.mesh?.hubs,
                                             channel: e.channel, build: e.build, protocolVersion: e.protocolVersion,
                                             minAppBuild: e.minAppBuild))
        case "hub_update": return e.update.map { .hubUpdate($0) }
        case "machines": return e.machines.map { .machines($0) }
        case "sessions":
            guard let m = e.machineID else { return nil }
            return .sessions(machineID: m, (e.sessions ?? []).map { var s = $0; s.machineID = m; return s })
        case "sessions_delta":
            guard let m = e.machineID else { return nil }
            return .sessionsDelta(machineID: m, upsert: (e.upsert ?? []).map { var s = $0; s.machineID = m; return s }, remove: e.remove ?? [])
        case "items":
            guard let key else { return nil }
            return .items(key, e.items ?? [], backlog: e.backlog ?? false, hasMore: e.hasMore,
                          span: ItemSpan(history: e.history, from: e.fromSeq, to: e.toSeq), moreAfter: e.moreAfter ?? false)
        case "screen":
            guard let key else { return nil }
            return .screen(key, text: e.text ?? "", status: e.status ?? .unknown)
        case "delta":
            guard let key else { return nil }
            return .delta(key, text: e.text ?? "")
        case "interaction": return e.interaction.map { .interaction($0) }
        case "interaction_resolved": return e.id.map { .interactionResolved($0) }
        case "tool_call":
            guard let id = e.callID, let name = e.name else { return nil }
            return .toolCall(callID: id, name: name, args: e.args ?? .object([:]))
        case "hook_event":
            guard let key else { return nil }
            return .hookEvent(key, event: e.event ?? "", message: e.message, lastText: e.lastAssistantMessage)
        case "error": return .error(key, e.error ?? "未知错误")
        case "revs":
            guard let m = e.machineID, let revs = e.revs else { return nil }
            return .revs(machineID: m, revs)
        case "lark": return .lark(feedIDs: e.feedIDs ?? [])
        case "agents": return .agents(sessionIDs: e.sessionIDs ?? [])
        // the list prefs changed on another device: a non-session id wakes only the lists, which re-pull the prefs
        case "agents.prefs": return .agents(sessionIDs: ["agents.prefs"])
        case "inbox": return .inbox
        case "todos": return .todos
        case "assistant_feed": return e.item.map { .assistantFeed($0) }
        case "task_plan": return e.plan.map { .taskPlan($0) }
        case "fleet": return e.fleet.map { .fleet($0) }
        case "desktops": return .desktops(e.desktops ?? [])
        case "terminal":
            guard let key else { return nil }
            if let error = e.error { return .terminal(key, .ended(error, unsupported: e.unsupported ?? false)) }
            guard let epoch = e.epoch, let seq = e.seq, let data = e.data.flatMap({ Data(base64Encoded: $0) }) else { return nil }
            return .terminal(key, .frame(epoch: epoch, seq: seq, full: e.full ?? false, cols: e.cols ?? 0, rows: e.rows ?? 0, data: data))
        case "terminal_control":
            guard let key, let state = e.state else { return nil }
            return .terminal(key, .control(state: state, cols: e.cols, rows: e.rows, why: e.error))
        default: return nil
        }
    }
}

nonisolated extension ServerEvent {
    /// The event with the group of the connection it came on (docs/multi-hub.md) on every key, session, machine and
    /// interaction in it. The default group's events are left as they are.
    func stamped(_ group: String) -> ServerEvent {
        guard !group.isEmpty else { return self }
        func k(_ key: SessionKey) -> SessionKey { var key = key; key.group = group; return key }
        switch self {
        case .machines(let list): return .machines(list.map { var m = $0; m.group = group; return m })
        case .sessions(let machine, let list): return .sessions(machineID: machine, list.map { var s = $0; s.group = group; return s })
        case .items(let key, let items, let backlog, let hasMore, let span, let moreAfter):
            return .items(k(key), items, backlog: backlog, hasMore: hasMore, span: span, moreAfter: moreAfter)
        case .screen(let key, let text, let status): return .screen(k(key), text: text, status: status)
        case .delta(let key, let text): return .delta(k(key), text: text)
        case .interaction(let ix): var ix = ix; ix.group = group; return .interaction(ix)
        case .hookEvent(let key, let event, let message, let lastText):
            return .hookEvent(k(key), event: event, message: message, lastText: lastText)
        case .terminal(let key, let event): return .terminal(k(key), event)
        case .error(let key, let message): return .error(key.map(k), message)
        case .sessionsDelta(let machine, let upsert, let remove):
            return .sessionsDelta(machineID: machine, upsert: upsert.map { var s = $0; s.group = group; return s }, remove: remove)
        case .hello, .interactionResolved, .toolCall, .revs, .lark, .agents, .inbox, .fleet, .hubUpdate, .desktops, .todos,
             .assistantFeed, .taskPlan: return self
        }
    }
}

/// The 体验模式 hub's end of the socket (Persona/App/DemoHub.swift): what the app sends goes to `receive`, and it
/// answers through `deliver` with the same JSON a hub sends.
protocol LocalSocket: AnyObject {
    func open(deliver: @escaping (Data) -> Void)
    func receive(_ message: [String: Any])
    func close()
}

/// The app's WebSocket to the hub: reconnects with backoff and replays subscriptions.
@Observable
final class LiveConnection {
    enum State: Equatable {
        case idle, connecting, connected
        case failed(String)
    }

    private(set) var state: State = .idle { didSet { if state != oldValue { onChange?() } } }
    /// Connected, but a ping has gone unanswered for `slowAfter`, or its pong came that late: a weak or stalled
    /// network. Only a prompt pong clears it (a late one alone made the title flip every ping), and pings go every
    /// `slowPingEvery` meanwhile. The socket is only declared dead after `deadAfter`; until then the app says
    /// 连接中… (ConnectionStatus).
    private(set) var stalled = false { didSet { if stalled != oldValue { onChange?() } } }
    /// Round trip of the last answered ping.
    private(set) var rtt: TimeInterval?
    /// When the next attempt starts while the hub is out of reach.
    private(set) var nextTry: Date?
    @ObservationIgnored var onEvent: ((ServerEvent) -> Void)?
    /// The state or `stalled` changed.
    @ObservationIgnored var onChange: (() -> Void)?
    @ObservationIgnored var prepareEvent: (@Sendable (ServerEvent) -> Void)?
    @ObservationIgnored var onConnected: (() -> Void)?
    /// Picks where each attempt goes when a group has several hubs (HubRoute, docs/multi-hub.md「选路」): asked before
    /// every attempt; nil keeps the config given to `connect`.
    @ObservationIgnored var route: (@MainActor () async -> ServerConfig?)?

    @ObservationIgnored private var config: ServerConfig?
    /// Where the socket goes (or went last): the config given to `connect`, or the one `route` picked.
    var currentConfig: ServerConfig? { config }
    @ObservationIgnored private var task: URLSessionWebSocketTask?
    /// The demo config's socket, made by `makeLocal` (DemoHub registers it at launch).
    @ObservationIgnored private var local: LocalSocket?
    @ObservationIgnored static var makeLocal: (() -> LocalSocket)?
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var backoff: TimeInterval = 1
    @ObservationIgnored private let session = URLSession(configuration: .default)
    /// When the hub last answered (any message or a pong): a socket that went quiet on a network change can sit
    /// "connected" forever, and the chat would silently stop updating.
    @ObservationIgnored private var lastHeard = Date.distantPast
    static let pingEvery: Duration = .seconds(20)
    static let deadAfter: TimeInterval = 45
    static let slowAfter: TimeInterval = 5
    static let slowPingEvery: Duration = .seconds(5)
    @ObservationIgnored private var lastPong = Date.distantPast
    /// The app is in the background: nothing opens the socket (a reconnect after a failure, the network coming back, a
    /// refresh finishing) but `wake()`, for a silent push. iOS suspends an app with its socket open, and the hub takes
    /// a connected phone for an open app: no wake push for its phone_* calls, no Live Activity pushes (it draws its
    /// own), until the pings time out.
    @ObservationIgnored var suspended = false

    func connect(_ config: ServerConfig) {
        self.config = config
        backoff = 1
        open()
    }

    func disconnect() {
        generation += 1
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
        local?.close()
        local = nil
        state = .idle
        stalled = false
        nextTry = nil
    }

    /// A silent push (`jarvis_wake`) in the background: connect for the hub's call, though suspended.
    func wake() {
        suspended = false
        ensureConnected()
    }

    /// Reconnect now if the socket is down (e.g. app came to foreground).
    func ensureConnected() {
        guard config != nil else { return }
        if state != .connected && state != .connecting { backoff = 1; open() }
    }

    /// The network came back or the user asked: try right away instead of after the backoff (up to 20 s), and also
    /// replace an attempt that hangs. A socket that works is left alone.
    func reconnectNow() {
        guard config != nil else { return }
        backoff = 1
        if state != .connected || stalled { open() }
    }

    /// The group's center answers again while the socket is on a member (AppModel.watchCenter): open a new socket,
    /// which asks `route` first and so goes to the center. Nothing while suspended or with no route to ask.
    func reroute() {
        guard config != nil, route != nil, !suspended else { return }
        backoff = 1
        open()
    }

    /// The network path changed (Wi-Fi to cellular): a socket on the old interface goes quiet without failing. Ping
    /// now, so a dead one shows as stalled in seconds instead of at the next heartbeat.
    func probe() {
        guard let task, state == .connected else { return }
        ping(task, generation)
    }

    private func open() {
        guard let config, !suspended else { return }
        generation += 1
        let gen = generation
        task?.cancel(with: .goingAway, reason: nil)
        local?.close()
        local = nil
        if config.isDemo {
            openLocal(gen)
            return
        }
        guard let route else { openSocket(config, gen); return }
        state = .connecting
        stalled = false
        nextTry = nil
        Task { @MainActor [weak self] in
            let picked = await route()
            guard let self, gen == self.generation, !self.suspended else { return }
            if let picked, picked != self.config {
                self.config = picked
                self.onRoute?(picked)
            }
            self.openSocket(self.config ?? config, gen)
        }
    }

    /// The hub `route` picked for the next attempt.
    @ObservationIgnored var onRoute: ((ServerConfig) -> Void)?

    private func openSocket(_ config: ServerConfig, _ gen: Int) {
        var comps = URLComponents(url: config.url.appending(path: "/api/v1/ws"), resolvingAgainstBaseURL: false)!
        comps.scheme = config.url.scheme == "https" ? "wss" : "ws"
        // A hub that has it sends session lists as deltas (an older one ignores the question and sends whole lists).
        comps.queryItems = [URLQueryItem(name: "features", value: "sessions_delta")]
        var req = URLRequest(url: HubTransport.resolve(comps.url!, hub: config.url))
        req.setValue("Bearer \(config.token)", forHTTPHeaderField: "Authorization")
        req.timeoutInterval = 15
        let t = session.webSocketTask(with: req)
        t.maximumMessageSize = 64 << 20
        task = t
        state = .connecting
        stalled = false
        nextTry = nil
        t.resume()
        receive(t, gen)
        heartbeat(t, gen)
    }

    private func heartbeat(_ t: URLSessionWebSocketTask, _ gen: Int) {
        Task { @MainActor [weak self] in
            while true {
                try? await Task.sleep(for: self?.stalled == true ? Self.slowPingEvery : Self.pingEvery)
                guard let self, gen == self.generation else { return }
                guard self.state == .connected else { continue }
                if Date.now.timeIntervalSince(self.lastHeard) > Self.deadAfter {
                    t.cancel(with: .goingAway, reason: nil) // its receive fails and schedules the reconnect
                    return
                }
                self.ping(t, gen)
            }
        }
    }

    /// One ping: its pong refreshes `lastHeard` and the round trip, and decides `stalled`; no answer within
    /// `slowAfter` marks the link stalled.
    private func ping(_ t: URLSessionWebSocketTask, _ gen: Int) {
        let sent = Date.now
        t.sendPing { [weak self] error in
            guard error == nil else { return }
            Task { @MainActor in
                guard let self, gen == self.generation else { return }
                let rtt = Date.now.timeIntervalSince(sent)
                self.lastHeard = .now
                self.lastPong = .now
                self.rtt = rtt
                self.stalled = rtt > Self.slowAfter
            }
        }
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: .seconds(Self.slowAfter))
            guard let self, gen == self.generation, self.state == .connected, self.lastPong < sent else { return }
            self.stalled = true
        }
    }

    private func receive(_ t: URLSessionWebSocketTask, _ gen: Int) {
        let prepare = prepareEvent
        t.receive { [weak self, prepare] result in
            // Decode here, off the main thread: session lists are large and arrive every second while agents
            // work (decoding them on the main thread showed up in scrolling profiles).
            let event: ServerEvent? = switch result {
            case .success(.string(let s)): s.data(using: .utf8).flatMap(ServerEvent.decode)
            case .success(.data(let d)): ServerEvent.decode(d)
            default: nil
            }
            if let event { prepare?(event) }
            Task { @MainActor in
                guard let self, gen == self.generation else { return }
                switch result {
                case .success:
                    self.lastHeard = .now
                    if self.state != .connected {
                        self.state = .connected
                        self.backoff = 1
                        self.onConnected?()
                    }
                    if let event { self.onEvent?(event) }
                    self.receive(t, gen)
                case .failure(let err):
                    self.state = .failed(err.localizedDescription)
                    self.stalled = false
                    self.scheduleReconnect(gen)
                }
            }
        }
    }

    /// 体验模式: no network, no heartbeat; the made-up hub's messages arrive like a socket's.
    private func openLocal(_ gen: Int) {
        guard let make = Self.makeLocal else {
            state = .failed("体验模式不可用")
            return
        }
        let socket = make()
        local = socket
        state = .connecting
        let prepare = prepareEvent
        socket.open { [weak self] data in
            guard let self, gen == self.generation, let event = ServerEvent.decode(data) else { return }
            prepare?(event)
            if self.state != .connected {
                self.state = .connected
                self.onConnected?()
            }
            self.lastHeard = .now
            self.onEvent?(event)
        }
    }

    private func scheduleReconnect(_ gen: Int) {
        let delay = backoff
        backoff = min(backoff * 2, 20)
        nextTry = .now + delay
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(delay))
            guard gen == self.generation else { return }
            self.open()
        }
    }

    func send(_ object: [String: Any]) {
        if let local {
            local.receive(object)
            return
        }
        guard let task, state == .connected,
              let data = try? JSONSerialization.data(withJSONObject: object),
              let s = String(data: data, encoding: .utf8) else { return }
        task.send(.string(s)) { _ in }
    }

    func send<T: Encodable>(encodable: T) {
        if let local {
            if let data = try? JSON.encoder.encode(encodable), let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                local.receive(object)
            }
            return
        }
        guard let task, state == .connected, let data = try? JSON.encoder.encode(encodable),
              let s = String(data: data, encoding: .utf8) else { return }
        task.send(.string(s)) { _ in }
    }
}
