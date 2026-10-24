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
            let msg = (try? JSON.decoder.decode([String: String].self, from: data))?["error"]
            throw APIError(status: status, message: msg ?? HTTPURLResponse.localizedString(forStatusCode: status))
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
        enum CodingKeys: String, CodingKey {
            case cwd, launcher, prompt
            case machineID = "machine_id", resumeSessionID = "resume_session_id"
        }
    }

    func start(_ req: StartRequest) async throws -> Session {
        try await post("/api/v1/sessions", body: req, timeout: 110, as: SessionResponse.self).session
    }

    private func sp(_ k: SessionKey) -> String { "/api/v1/sessions/\(k.machineID)/\(k.sessionID)" }

    func items(_ k: SessionKey, beforeSeq: Int?, limit: Int = 80) async throws -> ItemsResponse {
        try await get(sp(k) + "/items", query: ["before_seq": beforeSeq.map(String.init) ?? "", "limit": String(limit)])
    }

    struct SendBody: Encodable { var text: String; var images: [ImagePayload]? }
    func send(_ k: SessionKey, text: String, images: [ImagePayload] = []) async throws {
        try await post(sp(k) + "/messages", body: SendBody(text: text, images: images.isEmpty ? nil : images), timeout: 100)
    }
    func interrupt(_ k: SessionKey) async throws { try await post(sp(k) + "/interrupt") }

    struct KeysBody: Encodable { var keys: [String]; var text: String }
    func keys(_ k: SessionKey, keys: [String], text: String = "") async throws {
        try await post(sp(k) + "/keys", body: KeysBody(keys: keys, text: text))
    }
    func screen(_ k: SessionKey, lines: Int = 60) async throws -> ScreenResponse {
        try await get(sp(k) + "/screen", query: ["lines": String(lines)])
    }
    func close(_ k: SessionKey) async throws { try await post(sp(k) + "/close") }

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
