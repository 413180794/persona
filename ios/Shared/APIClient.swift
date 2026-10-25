import Foundation

nonisolated struct ServerConfig: Codable, Hashable, Sendable {
    var url: URL
    var token: String

    /// Parses `jarvis://pair?url=…&token=…`.
    init?(pairURL: String) {
        guard let comps = URLComponents(string: pairURL.trimmingCharacters(in: .whitespacesAndNewlines)),
              comps.scheme == "jarvis",
              let u = comps.queryItems?.first(where: { $0.name == "url" })?.value, let url = URL(string: u),
              let token = comps.queryItems?.first(where: { $0.name == "token" })?.value, !token.isEmpty
        else { return nil }
        self.url = url
        self.token = token
    }

    init(url: URL, token: String) {
        self.url = url
        self.token = token
    }
}

nonisolated struct APIError: LocalizedError, Sendable {
    var status: Int
    var message: String
    var errorDescription: String? { message }

    /// The hub's `{"error":"…"}`, or lark-connector's `{"error":{"code","message"}}` passed through the read-only proxy.
    static func message(in data: Data) -> String? {
        if let flat = (try? JSON.decoder.decode([String: String].self, from: data))?["error"] { return flat }
        guard let obj = try? JSON.decoder.decode(JSONValue.self, from: data), let err = obj["error"] else { return nil }
        let message = err["message"]?.string
        let code = err["code"]?.string
        switch (message, code) {
        case let (m?, c?): return "\(m)（\(c)）"
        case let (m?, nil): return m
        case let (nil, c?): return c
        default: return nil
        }
    }
}

/// REST client for the hub. All endpoints return JSON objects; errors are `{"error": "..."}`.
nonisolated struct APIClient: Sendable {
    var config: ServerConfig
    var session: URLSession = .shared

    func url(_ path: String, query: [String: String] = [:]) -> URL {
        var comps = URLComponents(url: config.url.appending(path: path), resolvingAgainstBaseURL: false)!
        let items = query.filter { !$0.value.isEmpty }.map { URLQueryItem(name: $0.key, value: $0.value) }
        if !items.isEmpty { comps.queryItems = items.sorted { $0.name < $1.name } }
        return comps.url!
    }

    func request(_ method: String, _ path: String, query: [String: String] = [:], body: (any Encodable)? = nil,
                 timeout: TimeInterval = 30) async throws -> Data {
        var req = URLRequest(url: url(path, query: query), timeoutInterval: timeout)
        req.httpMethod = method
        req.setValue("Bearer \(config.token)", forHTTPHeaderField: "Authorization")
        if let body {
            req.httpBody = try JSON.encoder.encode(AnyEncodable(body))
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, resp) = try await session.data(for: req)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw APIError(status: status, message: APIError.message(in: data) ?? HTTPURLResponse.localizedString(forStatusCode: status))
        }
        return data
    }

    func get<T: Decodable>(_ path: String, query: [String: String] = [:], as: T.Type = T.self) async throws -> T {
        try JSON.decoder.decode(T.self, from: try await request("GET", path, query: query))
    }

    func post<T: Decodable>(_ path: String, body: (any Encodable)? = nil, timeout: TimeInterval = 30, as: T.Type = T.self) async throws -> T {
        try JSON.decoder.decode(T.self, from: try await request("POST", path, body: body ?? [String: String](), timeout: timeout))
    }

    @discardableResult
    func post(_ path: String, body: (any Encodable)? = nil, timeout: TimeInterval = 30) async throws -> Data {
        try await request("POST", path, body: body ?? [String: String](), timeout: timeout)
    }

    func delete(_ path: String) async throws {
        _ = try await request("DELETE", path)
    }

    // MARK: Endpoints

    struct MachinesResponse: Decodable { var machines: [Machine] }
    struct LaunchersResponse: Decodable { var launchers: [Launcher] }
    struct SessionsResponse: Decodable { var sessions: [Session] }
    struct SessionResponse: Decodable { var session: Session }
    struct ItemsResponse: Decodable {
        var items: [Item]
        var hasMore: Bool
        enum CodingKeys: String, CodingKey { case items; case hasMore = "has_more" }
    }
    struct ScreenResponse: Decodable { var text: String; var status: SessionStatus? }
    struct InteractionsResponse: Decodable { var interactions: [Interaction] }

    func health() async throws -> Bool {
        struct H: Decodable { var ok: Bool }
        return try await get("/api/v1/health", as: H.self).ok
    }

    func machines() async throws -> [Machine] { try await get("/api/v1/machines", as: MachinesResponse.self).machines }
    func launchers(machine: String) async throws -> [Launcher] {
        try await get("/api/v1/machines/\(machine)/launchers", as: LaunchersResponse.self).launchers
    }
    func dirs(machine: String, path: String) async throws -> DirList {
        try await get("/api/v1/machines/\(machine)/dirs", query: ["path": path])
    }
    func reconnect(machine: String) async throws { try await post("/api/v1/machines/\(machine)/reconnect") }
    func sessions() async throws -> [Session] { try await get("/api/v1/sessions", as: SessionsResponse.self).sessions }

    struct StartRequest: Encodable {
        var machineID: String
        var cwd: String
        var launcher: String
        var prompt: String?
        var resumeSessionID: String?
        var workspaceID: String?
        enum CodingKeys: String, CodingKey {
            case cwd, launcher, prompt
            case machineID = "machine_id", resumeSessionID = "resume_session_id", workspaceID = "workspace_id"
        }
    }

    func start(_ req: StartRequest) async throws -> Session {
        try await post("/api/v1/sessions", body: req, timeout: 110, as: SessionResponse.self).session
    }

    private func sp(_ k: SessionKey) -> String { "/api/v1/sessions/\(k.machineID)/\(k.sessionID)" }

    /// A page of history going back `turns` whole exchanges (user prompt + answer) before beforeSeq.
    func items(_ k: SessionKey, beforeSeq: Int?, turns: Int = 2) async throws -> ItemsResponse {
        try await get(sp(k) + "/items", query: ["before_seq": beforeSeq.map(String.init) ?? "", "turns": String(turns), "limit": "80"])
    }

    struct SendBody: Encodable { var text: String; var images: [ImagePayload]?; var launcher: String? }
    /// launcher switches the model of a headless (assistant) conversation from this message on.
    func send(_ k: SessionKey, text: String, images: [ImagePayload] = [], launcher: String? = nil) async throws {
        try await post(sp(k) + "/messages", body: SendBody(text: text, images: images.isEmpty ? nil : images, launcher: launcher), timeout: 100)
    }
    func interrupt(_ k: SessionKey) async throws { try await post(sp(k) + "/interrupt") }

    struct KeysBody: Encodable { var keys: [String]; var text: String }
    func keys(_ k: SessionKey, keys: [String], text: String = "") async throws {
        try await post(sp(k) + "/keys", body: KeysBody(keys: keys, text: text))
    }
    /// `recent` also reaches back into the pane's scrollback (page by asking for more lines).
    func screen(_ k: SessionKey, lines: Int = 60, recent: Bool = false) async throws -> ScreenResponse {
        try await get(sp(k) + "/screen", query: ["lines": String(lines), "source": recent ? "recent" : "visible"])
    }
    func close(_ k: SessionKey) async throws { try await post(sp(k) + "/close") }

    struct ScreenBefore: Decodable { var text: String; var hasMore: Bool; var atLimit: Bool
        enum CodingKeys: String, CodingKey { case text; case hasMore = "has_more", atLimit = "at_limit" } }
    private struct ScreenBeforeBody: Encodable { var anchor: [String]; var lines: Int }
    /// The pane's scrollback just before `anchor` (the first lines already shown), at most `lines` lines.
    func screen(_ k: SessionKey, before anchor: [String], lines: Int = 200) async throws -> ScreenBefore {
        try await post(sp(k) + "/screen/before", body: ScreenBeforeBody(anchor: anchor, lines: lines), as: ScreenBefore.self)
    }

    struct SessionInfo: Decodable, Sendable {
        var sessionID: String
        var transcriptPath: String?
        var cwd: String?
        var model: String?
        var launcher: String?
        var gitBranch: String?
        var version: String?
        var permissionMode: String?
        var contextTokens: Int
        var outputTokens: Int
        var compactions: Int
        var turns: Int
        var startedAt: Date?
        var lastActivity: Date?
        enum CodingKeys: String, CodingKey {
            case cwd, model, launcher, version, compactions, turns
            case sessionID = "session_id", transcriptPath = "transcript_path", gitBranch = "git_branch",
                 permissionMode = "permission_mode", contextTokens = "context_tokens", outputTokens = "output_tokens",
                 startedAt = "started_at", lastActivity = "last_activity"
        }
    }
    func sessionInfo(_ k: SessionKey) async throws -> SessionInfo { try await get(sp(k) + "/info") }

    struct QueueEdit: Encodable { var keep: [Int] }
    struct QueueResponse: Decodable { var queue: [String] }
    /// Pulls the queued input back out of claude and re-queues the indexes in `keep`; returns what was queued.
    func editQueue(_ k: SessionKey, keep: [Int]) async throws -> [String] {
        try await post(sp(k) + "/queue", body: QueueEdit(keep: keep), as: QueueResponse.self).queue
    }

    struct AssistantState: Codable, Sendable {
        var machineID: String
        var launcher: String
        var cwd: String
        var conversations: [Session]
        enum CodingKeys: String, CodingKey { case launcher, cwd, conversations; case machineID = "machine_id" }
    }
    func assistant() async throws -> AssistantState { try await get("/api/v1/assistant") }
    struct AssistantConfigBody: Encodable {
        var machineID: String?
        var launcher: String?
        enum CodingKeys: String, CodingKey { case launcher; case machineID = "machine_id" }
    }
    func setAssistant(machine: String? = nil, launcher: String? = nil) async throws -> AssistantState {
        try JSON.decoder.decode(AssistantState.self, from: try await request("PUT", "/api/v1/assistant",
            body: AssistantConfigBody(machineID: machine, launcher: launcher)))
    }
    struct NewConversation: Encodable { var prompt: String; var images: [ImagePayload]?; var launcher: String? }
    func startConversation(prompt: String, images: [ImagePayload] = [], launcher: String? = nil) async throws -> Session {
        try await post("/api/v1/assistant/conversations", body: NewConversation(prompt: prompt, images: images.isEmpty ? nil : images, launcher: launcher),
                       timeout: 110, as: SessionResponse.self).session
    }

    struct ClaudeSetting: Codable, Identifiable, Sendable {
        var key: String
        var title: String
        var detail: String
        var type: String
        var options: [String]?
        var `default`: JSONValue?
        var value: JSONValue?
        var id: String { key }
    }
    struct ClaudeSettingsResponse: Decodable { var settings: [ClaudeSetting] }
    func claudeSettings(machine: String) async throws -> [ClaudeSetting] {
        try await get("/api/v1/machines/\(machine)/claude-settings", as: ClaudeSettingsResponse.self).settings
    }
    struct SetSetting: Encodable { var key: String; var value: JSONValue }
    func setClaudeSetting(machine: String, key: String, value: JSONValue) async throws -> [ClaudeSetting] {
        try JSON.decoder.decode(ClaudeSettingsResponse.self, from: try await request("PUT", "/api/v1/machines/\(machine)/claude-settings",
            body: SetSetting(key: key, value: value))).settings
    }

    func interactions() async throws -> [Interaction] {
        try await get("/api/v1/interactions", as: InteractionsResponse.self).interactions
    }
    func resolve(_ id: String, _ r: Resolution) async throws { try await post("/api/v1/interactions/\(id)/resolve", body: r) }

    struct DeviceBody: Encodable {
        var deviceID: String
        var name: String
        var kind: String
        var apnsToken: String?
        var apnsEnv: String?
        var topic: String?
        enum CodingKeys: String, CodingKey {
            case name, kind, topic
            case deviceID = "device_id", apnsToken = "apns_token", apnsEnv = "apns_env"
        }
    }
    func register(device: DeviceBody) async throws { try await post("/api/v1/devices", body: device) }
}

nonisolated struct AnyEncodable: Encodable {
    let value: any Encodable
    init(_ value: any Encodable) { self.value = value }
    func encode(to encoder: Encoder) throws { try value.encode(to: encoder) }
}

extension Error {
    /// A task or request that was cancelled (the screen went away, a newer load replaced it). iOS words it "已取消";
    /// it is never something to show the user.
    nonisolated var isCancellation: Bool {
        if self is CancellationError { return true }
        if let u = self as? URLError, u.code == .cancelled { return true }
        let ns = self as NSError
        return ns.domain == NSURLErrorDomain && ns.code == NSURLErrorCancelled
    }

    /// The message to show, or nil for a cancellation.
    nonisolated var shown: String? { isCancellation ? nil : localizedDescription }
}

/// Spaces ("machine/space name") and agents ("machine/session") kept at the top of 开发.
nonisolated struct Pins: Codable, Hashable, Sendable {
    var spaces: [String] = []
    var sessions: [String] = []
    /// Mutes only: agents that keep notifications although their space is muted.
    var allowed: [String] = []

    init() {}

    /// Mute rule (the hub's too): the agent is muted itself, or its space is unless the agent was turned back on.
    func mutes(session: String, space: String?) -> Bool {
        if sessions.contains(session) { return true }
        if allowed.contains(session) { return false }
        return space.map(spaces.contains) ?? false
    }

    /// Flips one agent's notifications on top of its space's setting.
    func togglingMute(session: String, space: String?) -> Pins {
        let wasMuted = mutes(session: session, space: space)
        let spaceMuted = space.map(spaces.contains) ?? false
        var p = self
        p.sessions.removeAll { $0 == session }
        p.allowed.removeAll { $0 == session }
        if !wasMuted { p.sessions.insert(session, at: 0) } else if spaceMuted { p.allowed.insert(session, at: 0) }
        return p
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        spaces = try c.decodeIfPresent([String].self, forKey: .spaces) ?? []
        sessions = try c.decodeIfPresent([String].self, forKey: .sessions) ?? []
        allowed = try c.decodeIfPresent([String].self, forKey: .allowed) ?? []
    }
}

extension APIClient {
    func pins() async throws -> Pins { try await get("/api/v1/pins") }
    /// Muted spaces / agents: no push notifications, shown quietly (same shape as pins).
    func mutes() async throws -> Pins { try await get("/api/v1/mutes") }
    @discardableResult
    func setMutes(_ p: Pins) async throws -> Pins {
        try JSON.decoder.decode(Pins.self, from: try await request("PUT", "/api/v1/mutes", body: p))
    }
    @discardableResult
    func setPins(_ p: Pins) async throws -> Pins {
        try JSON.decoder.decode(Pins.self, from: try await request("PUT", "/api/v1/pins", body: p))
    }
}

extension APIClient {
    /// Renames a herdr workspace; the hub carries pins / mutes over to the new name.
    func renameSpace(machine: String, workspaceID: String, name: String) async throws {
        try await post("/api/v1/machines/\(machine)/spaces/\(workspaceID)/rename", body: ["name": name])
    }

    struct RenameBody: Encodable {
        var tabName: String?
        var title: String?
        enum CodingKeys: String, CodingKey { case title; case tabName = "tab_name" }
    }
    /// Renames the session's herdr tab and / or the Claude session (`/rename`).
    func renameSession(_ k: SessionKey, tabName: String?, title: String?) async throws {
        try await post("/api/v1/sessions/\(k.machineID)/\(k.sessionID)/rename", body: RenameBody(tabName: tabName, title: title))
    }
}

