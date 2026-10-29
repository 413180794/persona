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
    /// features: what the hub supports beyond the first protocol (proto.Features).
    case hello(features: [String])
    case machines([Machine])
    case sessions(machineID: String, [Session])
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
    /// The unified inbox changed (hub/inbox.go): pull it.
    case inbox
    /// The Fleet Live Activity's content changed (hub/liveactivity.go).
    case fleet(FleetState)
    case terminal(SessionKey, TerminalEvent)
    case error(SessionKey?, String)

    private struct Envelope: Decodable {
        var type: String
        var machineID: String?
        var sessionID: String?
        var machines: [Machine]?
        var sessions: [Session]?
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
        var fleet: FleetState?
        var features: [String]?
        var epoch: String?
        var seq: Int?
        var full: Bool?
        var cols: Int?
        var rows: Int?
        var data: String?
        var unsupported: Bool?
        var state: String?

        enum CodingKeys: String, CodingKey {
            case type, machines, sessions, items, backlog, text, status, interaction, id, name, args, event, message, error, revs, fleet, history
            case features, epoch, seq, full, cols, rows, data, unsupported, state
            case machineID = "machine_id", sessionID = "session_id", hasMore = "has_more", callID = "call_id",
                 moreAfter = "more_after", fromSeq = "from_seq", toSeq = "to_seq",
                 lastAssistantMessage = "last_assistant_message", feedIDs = "feed_ids"
        }
    }

    static func decode(_ data: Data) -> ServerEvent? {
        guard let e = try? JSON.decoder.decode(Envelope.self, from: data) else { return nil }
        let key = e.machineID.flatMap { m in e.sessionID.map { SessionKey(machineID: m, sessionID: $0) } }
        switch e.type {
        case "hello": return .hello(features: e.features ?? [])
        case "machines": return e.machines.map { .machines($0) }
        case "sessions":
            guard let m = e.machineID else { return nil }
            return .sessions(machineID: m, (e.sessions ?? []).map { var s = $0; s.machineID = m; return s })
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
        case "inbox": return .inbox
        case "fleet": return e.fleet.map { .fleet($0) }
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
    @ObservationIgnored var onConnected: (() -> Void)?

    @ObservationIgnored private var config: ServerConfig?
    @ObservationIgnored private var task: URLSessionWebSocketTask?
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

    func connect(_ config: ServerConfig) {
        self.config = config
        backoff = 1
        open()
    }

    func disconnect() {
        generation += 1
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
        state = .idle
        stalled = false
        nextTry = nil
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

    /// The network path changed (Wi-Fi to cellular): a socket on the old interface goes quiet without failing. Ping
    /// now, so a dead one shows as stalled in seconds instead of at the next heartbeat.
    func probe() {
        guard let task, state == .connected else { return }
        ping(task, generation)
    }

    private func open() {
        guard let config else { return }
        generation += 1
        let gen = generation
        task?.cancel(with: .goingAway, reason: nil)
        var comps = URLComponents(url: config.url.appending(path: "/api/v1/ws"), resolvingAgainstBaseURL: false)!
        comps.scheme = config.url.scheme == "https" ? "wss" : "ws"
        var req = URLRequest(url: comps.url!)
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
        t.receive { [weak self] result in
            // Decode here, off the main thread: session lists are large and arrive every second while agents
            // work (decoding them on the main thread showed up in scrolling profiles).
            let event: ServerEvent? = switch result {
            case .success(.string(let s)): s.data(using: .utf8).flatMap(ServerEvent.decode)
            case .success(.data(let d)): ServerEvent.decode(d)
            default: nil
            }
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
        guard let task, state == .connected,
              let data = try? JSONSerialization.data(withJSONObject: object),
              let s = String(data: data, encoding: .utf8) else { return }
        task.send(.string(s)) { _ in }
    }

    func send<T: Encodable>(encodable: T) {
        guard let task, state == .connected, let data = try? JSON.encoder.encode(encodable),
              let s = String(data: data, encoding: .utf8) else { return }
        task.send(.string(s)) { _ in }
    }
}
