import Foundation
import Observation

nonisolated enum ServerEvent: Sendable {
    case hello
    case machines([Machine])
    case sessions(machineID: String, [Session])
    case items(SessionKey, [Item], backlog: Bool, hasMore: Bool)
    case screen(SessionKey, text: String, status: SessionStatus)
    case interaction(Interaction)
    case interactionResolved(String)
    case toolCall(callID: String, name: String, args: JSONValue)
    case hookEvent(SessionKey, event: String, message: String?, lastText: String?)
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

        enum CodingKeys: String, CodingKey {
            case type, machines, sessions, items, backlog, text, status, interaction, id, name, args, event, message, error
            case machineID = "machine_id", sessionID = "session_id", hasMore = "has_more", callID = "call_id",
                 lastAssistantMessage = "last_assistant_message"
        }
    }

    static func decode(_ data: Data) -> ServerEvent? {
        guard let e = try? JSON.decoder.decode(Envelope.self, from: data) else { return nil }
        let key = e.machineID.flatMap { m in e.sessionID.map { SessionKey(machineID: m, sessionID: $0) } }
        switch e.type {
        case "hello": return .hello
        case "machines": return e.machines.map { .machines($0) }
        case "sessions":
            guard let m = e.machineID else { return nil }
            return .sessions(machineID: m, (e.sessions ?? []).map { var s = $0; s.machineID = m; return s })
        case "items":
            guard let key else { return nil }
            return .items(key, e.items ?? [], backlog: e.backlog ?? false, hasMore: e.hasMore ?? false)
        case "screen":
            guard let key else { return nil }
            return .screen(key, text: e.text ?? "", status: e.status ?? .unknown)
        case "interaction": return e.interaction.map { .interaction($0) }
        case "interaction_resolved": return e.id.map { .interactionResolved($0) }
        case "tool_call":
            guard let id = e.callID, let name = e.name else { return nil }
            return .toolCall(callID: id, name: name, args: e.args ?? .object([:]))
        case "hook_event":
            guard let key else { return nil }
            return .hookEvent(key, event: e.event ?? "", message: e.message, lastText: e.lastAssistantMessage)
        case "error": return .error(key, e.error ?? "未知错误")
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

    private(set) var state: State = .idle
    @ObservationIgnored var onEvent: ((ServerEvent) -> Void)?
    @ObservationIgnored var onConnected: (() -> Void)?

    @ObservationIgnored private var config: ServerConfig?
    @ObservationIgnored private var task: URLSessionWebSocketTask?
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var backoff: TimeInterval = 1
    @ObservationIgnored private let session = URLSession(configuration: .default)

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
    }

    /// Reconnect now if the socket is down (e.g. app came to foreground).
    func ensureConnected() {
        guard config != nil else { return }
        if state != .connected && state != .connecting { backoff = 1; open() }
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
        t.resume()
        receive(t, gen)
    }

    private func receive(_ t: URLSessionWebSocketTask, _ gen: Int) {
        t.receive { [weak self] result in
            Task { @MainActor in
                guard let self, gen == self.generation else { return }
                switch result {
                case .success(let msg):
                    if self.state != .connected {
                        self.state = .connected
                        self.backoff = 1
                        self.onConnected?()
                    }
                    let data: Data? = switch msg {
                    case .string(let s): s.data(using: .utf8)
                    case .data(let d): d
                    @unknown default: nil
                    }
                    if let data, let ev = ServerEvent.decode(data) { self.onEvent?(ev) }
                    self.receive(t, gen)
                case .failure(let err):
                    self.state = .failed(err.localizedDescription)
                    self.scheduleReconnect(gen)
                }
            }
        }
    }

    private func scheduleReconnect(_ gen: Int) {
        let delay = backoff
        backoff = min(backoff * 2, 20)
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
