import Foundation

nonisolated struct ServerConfig: Codable, Hashable, Sendable {
    var url: URL
    var token: String

    /// Parses both `persona://pair?url=…&token=…` and the legacy `jarvis://` link.
    init?(pairURL: String) {
        guard let comps = URLComponents(string: pairURL.trimmingCharacters(in: .whitespacesAndNewlines)),
              ["persona", "jarvis"].contains(comps.scheme?.lowercased() ?? ""),
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

    /// 体验模式: the made-up hub inside the app (Persona/App/DemoHub.swift). Its host can never resolve, so nothing
    /// that slipped past `APIClient.local` reaches a network.
    static let demo = ServerConfig(url: URL(string: "https://demo.invalid")!, token: "demo")
    var isDemo: Bool { url.host() == "demo.invalid" }
}

/// One request the app's own made-up hub answers instead of the network (体验模式).
nonisolated struct LocalRequest: Sendable {
    var method: String
    var path: String
    var query: [String: String]
    var body: Data?
}

nonisolated struct APIError: LocalizedError, Sendable {
    var status: Int
    var message: String
    /// The response's other string fields: a refused send (409) has code (dialog_open / input_draft), delivered
    /// (none / unknown), draft and clearable (docs/protocol.md「发消息」).
    var fields: [String: String] = [:]
    /// The hub logs failed requests under this id (X-Request-Id): a server error shows it, to find that line.
    var requestID: String?
    var errorDescription: String? {
        guard status >= 500, let requestID else { return message }
        return "\(message)（请求 \(requestID)）"
    }
    var code: String? { fields["code"] }

    /// The hub's words for a call that ran out of time (Go's context deadline, a node call's timeout).
    static func isTimeoutText(_ message: String) -> Bool {
        let m = message.lowercased()
        return m.contains("deadline exceeded") || m.contains("timed out") || m.contains("timeout") || m.contains("超时")
    }

    static func fields(in data: Data) -> [String: String] {
        guard case .object(let obj)? = try? JSON.decoder.decode(JSONValue.self, from: data) else { return [:] }
        return obj.compactMapValues { $0.string }
    }

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

nonisolated enum DemoText {
    /// What a 体验模式 action that would reach a computer, Feishu or mail says instead.
    static let notSent = "体验模式下不会真的发送"
    /// `APIError.code` of such a refusal.
    static let code = "demo"
}

/// REST client for the hub. All endpoints return JSON objects; errors are `{"error": "..."}`.
nonisolated struct APIClient: Sendable {
    typealias Transport = @Sendable (LocalRequest) async throws -> Data

    var config: ServerConfig
    var session: URLSession = .shared
    /// Answers every request in place of the network: unit tests' `FakeHub`. The demo config uses `local`.
    var transport: Transport?
    /// The group of paired hubs this client talks to (docs/multi-hub.md): put on the sessions, machines,
    /// interactions and keys it returns. "" = the default group.
    var group: String = ""
    /// Answers the demo config's requests (DemoHub registers it at launch); every endpoint goes through `request`.
    nonisolated(unsafe) static var local: (@Sendable (LocalRequest) async throws -> Data)?

    /// For bytes the app caches itself (chat images, Feishu media): through `.shared` the system's URLCache kept a
    /// second copy of every one of them.
    static let uncachedSession: URLSession = {
        let c = URLSessionConfiguration.default
        c.urlCache = nil
        c.requestCachePolicy = .reloadIgnoringLocalCacheData
        return URLSession(configuration: c)
    }()

    func url(_ path: String, query: [String: String] = [:]) -> URL {
        var comps = URLComponents(url: config.url, resolvingAgainstBaseURL: false)!
        var base = comps.percentEncodedPath
        if base.hasSuffix("/") { base.removeLast() }
        comps.percentEncodedPath = base + Self.encodedPath(path)
        let items = query.filter { !$0.value.isEmpty }.map { URLQueryItem(name: $0.key, value: $0.value) }
        if !items.isEmpty { comps.queryItems = items.sorted { $0.name < $1.name } }
        return comps.url!
    }

    /// A path for the request line. Callers escape ids themselves (`kbDetail`: a slug with Chinese in it, an id with
    /// a slash); `URL.appending(path:)` escaped their `%` once more and the hub got another id — 404 (2026-10-30, 知识库).
    /// So: a path that already has escapes is used as is, anything else is escaped like appending(path:) did.
    nonisolated static func encodedPath(_ path: String) -> String {
        let p = path.hasPrefix("/") ? path : "/" + path
        if p.range(of: "%[0-9A-Fa-f]{2}", options: .regularExpression) != nil { return p }
        return p.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? p
    }

    func request(_ method: String, _ path: String, query: [String: String] = [:], body: (any Encodable)? = nil,
                 timeout: TimeInterval = 30) async throws -> Data {
        if let answer = transport ?? (config.isDemo ? Self.local : nil) {
            return try await answer(LocalRequest(method: method, path: path, query: query.filter { !$0.value.isEmpty },
                                                 body: try body.map { try JSON.encoder.encode(AnyEncodable($0)) }))
        }
        if config.isDemo { throw APIError(status: 503, message: "体验模式不可用") }
        var req = URLRequest(url: url(path, query: query), timeoutInterval: timeout)
        req.httpMethod = method
        req.setValue("Bearer \(config.token)", forHTTPHeaderField: "Authorization")
        let requestID = String(UUID().uuidString.prefix(8)).lowercased()
        req.setValue(requestID, forHTTPHeaderField: "X-Client-Request-Id")
        if let body {
            req.httpBody = try JSON.encoder.encode(AnyEncodable(body))
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, resp) = try await session.data(for: req)
        return try checked(data, resp, requestID: requestID)
    }

    private func checked(_ data: Data, _ resp: URLResponse, requestID: String) throws -> Data {
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw APIError(status: status, message: APIError.message(in: data) ?? HTTPURLResponse.localizedString(forStatusCode: status),
                           fields: APIError.fields(in: data),
                           requestID: (resp as? HTTPURLResponse)?.value(forHTTPHeaderField: "X-Request-Id") ?? requestID)
        }
        return data
    }

    nonisolated struct UploadedFile: Codable, Sendable {
        var path: String
        var name: String
        var size: Int64
    }

    /// Sends a file to the machine's temp directory (docs/protocol.md「发送文件」), streamed from disk; progress gets
    /// the bytes sent so far. Cancelling the task cancels the upload and the machine removes what it wrote.
    func upload(file: URL, name: String, machine: String,
                progress: @escaping @Sendable (Int64) -> Void) async throws -> UploadedFile {
        if config.isDemo { throw APIError(status: 403, message: DemoText.notSent) }
        var req = URLRequest(url: url("/api/v1/machines/\(machine)/files", query: ["name": name]), timeoutInterval: 120)
        req.httpMethod = "POST"
        req.setValue("Bearer \(config.token)", forHTTPHeaderField: "Authorization")
        let requestID = String(UUID().uuidString.prefix(8)).lowercased()
        req.setValue(requestID, forHTTPHeaderField: "X-Client-Request-Id")
        req.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
        let (data, resp) = try await Self.uncachedSession.upload(for: req, fromFile: file, delegate: UploadProgress(progress))
        return try JSON.decoder.decode(UploadedFile.self, from: try checked(data, resp, requestID: requestID))
    }

    func download(_ path: String, query: [String: String], to destination: URL,
                  progress: @escaping @Sendable (Int64, Int64) -> Void) async throws {
        if transport != nil || config.isDemo {
            let data = try await request("GET", path, query: query)
            try Task.checkCancellation()
            try data.write(to: destination, options: .atomic)
            progress(Int64(data.count), Int64(data.count))
            return
        }
        var req = URLRequest(url: url(path, query: query), timeoutInterval: 120)
        req.setValue("Bearer \(config.token)", forHTTPHeaderField: "Authorization")
        let requestID = String(UUID().uuidString.prefix(8)).lowercased()
        req.setValue(requestID, forHTTPHeaderField: "X-Client-Request-Id")
        let (temp, response) = try await Self.uncachedSession.download(for: req, delegate: DownloadProgress(progress))
        defer { try? FileManager.default.removeItem(at: temp) }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if !(200..<300).contains(status) {
            let handle = try FileHandle(forReadingFrom: temp)
            defer { try? handle.close() }
            _ = try checked(handle.read(upToCount: 64 << 10) ?? Data(), response, requestID: requestID)
        }
        try Task.checkCancellation()
        let size = (try FileManager.default.attributesOfItem(atPath: temp.path)[.size] as? NSNumber)?.int64Value ?? 0
        if response.expectedContentLength >= 0, size != response.expectedContentLength {
            throw APIError(status: 0, message: "文件没有下载完整，请重试")
        }
        try FileManager.default.moveItem(at: temp, to: destination)
        progress(size, size)
    }

    func get<T: Decodable>(_ path: String, query: [String: String] = [:], as: T.Type = T.self) async throws -> T {
        try JSON.decoder.decode(T.self, from: try await request("GET", path, query: query))
    }

    /// `get` for answers big enough to show in a frame: `get` decodes on the caller's actor (the main one, for a view),
    /// this one off it.
    func getDecodedOffMain<T: Decodable & Sendable>(_ path: String, query: [String: String] = [:], as: T.Type = T.self) async throws -> T {
        let data = try await request("GET", path, query: query)
        return try await Task.detached(priority: .userInitiated) { try JSON.decoder.decode(T.self, from: data) }.value
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
    nonisolated struct SessionsResponse: Decodable, Sendable { var sessions: [Session] }
    struct SessionResponse: Decodable { var session: Session }
    struct ItemsResponse: Decodable, Sendable {
        var items: [Item]
        var hasMore: Bool
        var history: String?
        var toSeq: Int?
        /// A page counted back from the end (no from: it does not continue anything).
        var span: ItemSpan { ItemSpan(history: history, from: nil, to: toSeq) }
        enum CodingKeys: String, CodingKey { case items, history; case hasMore = "has_more", toSeq = "to_seq" }
    }
    struct ScreenResponse: Decodable { var text: String; var status: SessionStatus? }
    struct InteractionsResponse: Decodable { var interactions: [Interaction] }

    func health() async throws -> Bool {
        struct H: Decodable { var ok: Bool }
        return try await get("/api/v1/health", as: H.self).ok
    }

    /// Which hub this is (`GET /api/v1/health`: `hub_id`, `hub_name`; docs/plan-hub-mesh.md 2.6).
    struct Identity: Decodable, Sendable {
        var hubID: String?
        var hubName: String?
        enum CodingKeys: String, CodingKey { case hubID = "hub_id", hubName = "hub_name" }
    }
    func identity() async throws -> Identity { try await get("/api/v1/health", as: Identity.self) }


    func machines() async throws -> [Machine] {
        stamp(try await get("/api/v1/machines", as: MachinesResponse.self).machines)
    }
    func setEngine(machine: String, engine: MachineEngine) async throws -> Machine {
        struct Body: Encodable { var engine: String }
        var m = try JSON.decoder.decode(Machine.self, from: await request("PUT", "/api/v1/machines/\(machine)/engine", body: Body(engine: engine.rawValue)))
        m.group = group
        return m
    }

    /// The group on what the hub returned (docs/multi-hub.md).
    func stamp(_ list: [Machine]) -> [Machine] { group.isEmpty ? list : list.map { var m = $0; m.group = group; return m } }
    func stamp(_ list: [Session]) -> [Session] { group.isEmpty ? list : list.map { var s = $0; s.group = group; return s } }
    func stamp(_ list: [Interaction]) -> [Interaction] { group.isEmpty ? list : list.map { var i = $0; i.group = group; return i } }
    func stamp(_ s: Session) -> Session { var s = s; s.group = group; return s }
    func stamp(_ k: SessionKey) -> SessionKey { var k = k; k.group = group; return k }
    func launchers(machine: String) async throws -> [Launcher] {
        try await get("/api/v1/machines/\(machine)/launchers", as: LaunchersResponse.self).launchers
    }
    func dirs(machine: String, path: String) async throws -> DirList {
        try await get("/api/v1/machines/\(machine)/dirs", query: ["path": path])
    }
    func reconnect(machine: String) async throws { try await post("/api/v1/machines/\(machine)/reconnect") }
    /// The hub installs its own jarvisd build on the machine and reconnects; progress comes with the machine list.
    func deploy(machine: String) async throws { try await post("/api/v1/machines/\(machine)/deploy") }

    /// Every machine's latest sample and its last hour by minute (docs/machines.md).
    func metricsOverview() async throws -> [MachineMetrics] {
        struct R: Decodable { var machines: [MachineMetrics] }
        return try await get("/api/v1/metrics", as: R.self).machines
    }
    func machineMetrics(_ machine: String, range: MetricRange) async throws -> MachineMetrics {
        struct R: Decodable { var metrics: MachineMetrics }
        return try await get("/api/v1/machines/\(machine)/metrics", query: ["range": range.rawValue], as: R.self).metrics
    }
    func sessions() async throws -> [Session] {
        let data = try await request("GET", "/api/v1/sessions")
        return stamp(try await Task.detached(priority: .userInitiated) {
            try JSON.decoder.decode(SessionsResponse.self, from: data).sessions
        }.value)
    }

    struct StartRequest: Encodable {
        var machineID: String
        var cwd: String
        var launcher: String
        var prompt: String?
        var resumeSessionID: String?
        var workspaceID: String?
        var permissionMode: String? = nil
        var effort: String? = nil
        /// The herdr tab's name; nil: the hub makes one from the first message (else agent-<n>).
        var tabName: String? = nil
        /// Answer at once with the session it is going to be and start it in the background (docs/protocol.md「新建会话」).
        var async: Bool? = nil
        /// What the tab name is made from when the first message is sent as a message of its own (not `prompt`).
        var nameHint: String? = nil
        enum CodingKeys: String, CodingKey {
            case cwd, launcher, prompt, effort, async
            case machineID = "machine_id", resumeSessionID = "resume_session_id", workspaceID = "workspace_id",
                 permissionMode = "permission_mode", tabName = "tab_name", nameHint = "name_hint"
        }
    }

    func start(_ req: StartRequest) async throws -> Session {
        stamp(try await post("/api/v1/sessions", body: req, timeout: req.async == true ? 20 : 110, as: SessionResponse.self).session)
    }

    private func sp(_ k: SessionKey) -> String { "/api/v1/sessions/\(k.machineID)/\(k.sessionID)" }

    /// A page of history going back `turns` whole exchanges (user prompt + answer) before beforeSeq.
    func items(_ k: SessionKey, beforeSeq: Int?, turns: Int = 2) async throws -> ItemsResponse {
        let data = try await request("GET", sp(k) + "/items",
                                     query: ["before_seq": beforeSeq.map(String.init) ?? "", "turns": String(turns), "limit": "80",
                                            "lazy_images": "1", "fold_work": "1"])
        // Images come without their bytes (`image(ref:)` fetches one when it is shown); a page is still worth
        // decoding off the main thread, which called us.
        return try await Task.detached(priority: .userInitiated) { try JSON.decoder.decode(ItemsResponse.self, from: data) }.value
    }

    /// The work a fold stands for (`Item.fold`): its lines, a page (about 1000 items) at a time.
    func foldedWork(_ k: SessionKey, _ fold: Item) async throws -> [Item] {
        struct Range: Decodable, Sendable { var items: [Item]; var more: Bool? }
        guard let through = fold.fold?.throughSeq else { return [] }
        var out: [Item] = []
        var from = fold.seq
        for _ in 0..<100 {
            let data = try await request("GET", sp(k) + "/items",
                                         query: ["from_seq": String(from), "through_seq": String(through), "lazy_images": "1"])
            let page = try await Task.detached(priority: .userInitiated) { try JSON.decoder.decode(Range.self, from: data) }.value
            out += page.items
            guard page.more == true, let last = page.items.last else { break }
            from = last.seq + 1
        }
        return out
    }

    /// The bytes of a lazy image (`ImagePayload.ref`, "<machine>/<session>/<item id>").
    func image(ref: String) async throws -> Data {
        let parts = ref.split(separator: "/", maxSplits: 2).map(String.init)
        guard parts.count == 3 else { throw APIError(status: 0, message: "bad image ref") }
        if config.isDemo { return try await request("GET", "/api/v1/sessions/\(parts[0])/\(parts[1])/items/\(parts[2])/image") }
        var req = URLRequest(url: itemURL(machine: parts[0], session: parts[1], item: parts[2], "image"), timeoutInterval: 60)
        req.setValue("Bearer \(config.token)", forHTTPHeaderField: "Authorization")
        let (data, resp) = try await Self.uncachedSession.data(for: req) // ChatImageLoader keeps it on disk
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw APIError(status: status, message: APIError.message(in: data) ?? HTTPURLResponse.localizedString(forStatusCode: status))
        }
        return data
    }

    /// An item whole: the text a page cut (`Item.fullSize`).
    func fullItem(_ k: SessionKey, id: String) async throws -> Item {
        if config.isDemo { return try JSON.decoder.decode(Item.self, from: try await request("GET", sp(k) + "/items/\(id)/full")) }
        var req = URLRequest(url: itemURL(machine: k.machineID, session: k.sessionID, item: id, "full"), timeoutInterval: 60)
        req.setValue("Bearer \(config.token)", forHTTPHeaderField: "Authorization")
        let (data, resp) = try await Self.uncachedSession.data(for: req)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw APIError(status: status, message: APIError.message(in: data) ?? HTTPURLResponse.localizedString(forStatusCode: status))
        }
        return try await Task.detached(priority: .userInitiated) { try JSON.decoder.decode(Item.self, from: data) }.value
    }

    /// …/items/<item id>/<what>. Item ids carry '#' and ':'; appending(path:) would leave '#' to start a fragment.
    private func itemURL(machine: String, session: String, item: String, _ what: String) -> URL {
        let id = item.addingPercentEncoding(withAllowedCharacters: .alphanumerics.union(CharacterSet(charactersIn: "-._~"))) ?? item
        var comps = URLComponents(url: config.url, resolvingAgainstBaseURL: false)!
        var base = comps.percentEncodedPath
        if base.hasSuffix("/") { base.removeLast() }
        comps.percentEncodedPath = base + "/api/v1/sessions/\(machine)/\(session)/items/\(id)/\(what)"
        return comps.url!
    }

    struct NewerResponse: Decodable, Sendable {
        var items: [Item]
        /// The hub stopped at its cap: more came after these (ask again after `toSeq`, or take the latest page).
        var moreAfter: Bool?
        var history: String?
        var fromSeq: Int?
        var toSeq: Int?
        var span: ItemSpan { ItemSpan(history: history, from: fromSeq, to: toSeq) }
        enum CodingKeys: String, CodingKey {
            case items, history
            case moreAfter = "more_after", fromSeq = "from_seq", toSeq = "to_seq"
        }
    }

    /// Everything after `afterSeq` (background sync of a session the app has not opened).
    func items(_ k: SessionKey, afterSeq: Int) async throws -> NewerResponse {
        let data = try await request("GET", sp(k) + "/items", query: ["after_seq": String(afterSeq), "lazy_images": "1"])
        return try await Task.detached(priority: .utility) { try JSON.decoder.decode(NewerResponse.self, from: data) }.value
    }

    struct SendBody: Encodable {
        var text: String
        var images: [ImagePayload]?
        var launcher: String?
        var clientMsgID: String?
        var clearDraft: String?
        enum CodingKeys: String, CodingKey {
            case text, images, launcher, clientMsgID = "client_msg_id", clearDraft = "clear_draft"
        }
    }
    /// launcher switches the model of a headless (assistant) conversation from this message on. clientMsgID stays the
    /// same on a retry (the node types it once); clearDraft is the computer's draft the user agreed to clear first.
    func send(_ k: SessionKey, text: String, images: [ImagePayload] = [], launcher: String? = nil,
              clientMsgID: String? = nil, clearDraft: String? = nil) async throws {
        try await post(sp(k) + "/messages", body: SendBody(text: text, images: images.isEmpty ? nil : images, launcher: launcher,
                                                           clientMsgID: clientMsgID, clearDraft: clearDraft), timeout: 100)
    }
    func interrupt(_ k: SessionKey) async throws { try await post(sp(k) + "/interrupt") }
    func attach(_ k: SessionKey) async throws { try await post(sp(k) + "/attach") }

    // MARK: Device credentials (DeviceCredentials.swift)

    struct NewToken: Decodable, Sendable { var id: String; var token: String; var role: String }
    struct DeviceToken: Decodable, Identifiable, Hashable, Sendable {
        var id: String
        var name: String
        var kind: String
        var role: String
        var createdAt: Date?
        var lastSeen: Date?
        var revokedAt: Date?
        enum CodingKeys: String, CodingKey {
            case id, name, kind, role, createdAt = "created_at", lastSeen = "last_seen", revokedAt = "revoked_at"
        }
    }
    struct DeviceTokens: Decodable, Sendable { var tokens: [DeviceToken]; var current: String? }

    /// Trades this credential for a device's own (kind "watch": answer-only).
    func createToken(name: String, kind: String) async throws -> NewToken {
        try JSON.decoder.decode(NewToken.self, from: try await request("POST", "/api/v1/auth/tokens", body: ["name": name, "kind": kind]))
    }
    func deviceTokens() async throws -> DeviceTokens {
        try JSON.decoder.decode(DeviceTokens.self, from: try await request("GET", "/api/v1/auth/tokens"))
    }
    func revokeToken(_ id: String) async throws { _ = try await request("DELETE", "/api/v1/auth/tokens/\(id)") }

    struct KeysBody: Encodable { var keys: [String]; var text: String }
    func keys(_ k: SessionKey, keys: [String], text: String = "") async throws {
        try await post(sp(k) + "/keys", body: KeysBody(keys: keys, text: text))
    }
    /// `recent` also reaches back into the pane's scrollback (page by asking for more lines); `ansi` keeps the colors
    /// (SGR escapes; an older node sends plain text either way).
    func screen(_ k: SessionKey, lines: Int = 60, recent: Bool = false, ansi: Bool = false) async throws -> ScreenResponse {
        var q = ["lines": String(lines), "source": recent ? "recent" : "visible"]
        if ansi { q["format"] = "ansi" }
        return try await get(sp(k) + "/screen", query: q)
    }
    func close(_ k: SessionKey) async throws { try await post(sp(k) + "/close") }

    struct ScreenBefore: Decodable { var text: String; var hasMore: Bool; var atLimit: Bool
        enum CodingKeys: String, CodingKey { case text; case hasMore = "has_more", atLimit = "at_limit" } }
    private struct ScreenBeforeBody: Encodable { var anchor: [String]; var lines: Int; var loaded: Int; var format: String? }
    /// The pane's scrollback just before `anchor` (the first lines already shown, `loaded` lines in all), at most
    /// `lines` lines; `ansi`: with its colors.
    func screen(_ k: SessionKey, before anchor: [String], loaded: Int, lines: Int = 200, ansi: Bool = false) async throws -> ScreenBefore {
        try await post(sp(k) + "/screen/before", body: ScreenBeforeBody(anchor: anchor, lines: lines, loaded: loaded, format: ansi ? "ansi" : nil),
                       as: ScreenBefore.self)
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
        var effort: String?
        var contextTokens: Int
        var outputTokens: Int
        var compactions: Int
        var turns: Int
        var startedAt: Date?
        var lastActivity: Date?
        enum CodingKeys: String, CodingKey {
            case cwd, model, launcher, version, compactions, turns, effort
            case sessionID = "session_id", transcriptPath = "transcript_path", gitBranch = "git_branch",
                 permissionMode = "permission_mode", contextTokens = "context_tokens", outputTokens = "output_tokens",
                 startedAt = "started_at", lastActivity = "last_activity"
        }
    }
    func sessionInfo(_ k: SessionKey) async throws -> SessionInfo { try await get(sp(k) + "/info") }

    struct ModesBody: Encodable {
        var permissionMode: String?
        var effort: String?
        enum CodingKeys: String, CodingKey { case effort; case permissionMode = "permission_mode" }
    }
    /// Changes a session's permission mode and / or effort level (nil leaves it); a TUI agent takes a second or two.
    func setModes(_ k: SessionKey, permissionMode: String? = nil, effort: String? = nil) async throws {
        try await post(sp(k) + "/modes", body: ModesBody(permissionMode: permissionMode, effort: effort), timeout: 30)
    }

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
        /// What the assistant is told about the user (设置 › 助手 › 关于我).
        var notes: String?
        var timezone: String?
        /// The main conversation's session id (the Persona tab opens it); nil until there is one.
        var main: String?
        enum CodingKeys: String, CodingKey {
            case launcher, cwd, conversations, timezone, main; case machineID = "machine_id", notes = "notes_effective"
        }
    }
    func assistant() async throws -> AssistantState { try await get("/api/v1/assistant") }
    struct AssistantConfigBody: Encodable {
        var machineID: String?
        var launcher: String?
        var timezone: String?
        var notes: String?
        enum CodingKeys: String, CodingKey { case launcher, timezone, notes; case machineID = "machine_id" }
    }
    func setAssistant(machine: String? = nil, launcher: String? = nil, timezone: String? = nil, notes: String? = nil) async throws -> AssistantState {
        try JSON.decoder.decode(AssistantState.self, from: try await request("PUT", "/api/v1/assistant",
            body: AssistantConfigBody(machineID: machine, launcher: launcher, timezone: timezone, notes: notes)))
    }
    /// The 对话 tab is open: the hub gets a claude process ready so the first message starts at once.
    func warmAssistant(launcher: String?) async throws {
        _ = try await request("POST", "/api/v1/assistant/warm", body: ["launcher": launcher ?? ""])
    }

    // MARK: Assistant schedule, memory (docs/assistant.md)

    /// Something the assistant does on its own (docs/assistant.md「定时任务」): once (a follow-up it scheduled), every
    /// day at a time (早报), or every N minutes (a patrol). Runs go into the conversation it came from, else into the
    /// task's own one (the first run starts it, the next ones continue it), with its own launcher / permission mode /
    /// effort (nil = the assistant's defaults); pushed when done.
    struct ScheduledTask: Codable, Identifiable, Hashable, Sendable {
        enum Repeat: String, Codable, Sendable, CaseIterable { case once, daily, interval }
        var id: String
        var title: String
        var prompt: String
        var repeats: Repeat
        var at: Date?             // once: when; interval: the next run
        var time: String          // daily: "HH:MM" in the phone's time zone
        var weekdays: [Int]       // daily: 1 = Monday … 7 = Sunday; empty = every day
        var everyMinutes: Int     // interval
        var enabled: Bool
        var machineID: String?
        var sessionID: String?    // the conversation it came from, runs go into it; nil = the task's own one
        var runMachineID: String? // read-only: the task's own conversation; nil = the next run starts one
        var runSessionID: String?
        var launcher: String?
        var permissionMode: String?
        var effort: String?
        var includesModelSettings = true
        var createdBy: String?    // "assistant": Persona set it up from a conversation
        var lastRun: Date?        // read-only, set by the hub
        var lastSessionID: String?
        var lastStatus: String?   // of the last run: ok / failed / skipped
        var lastError: String?
        var nextRun: Date?
        enum CodingKeys: String, CodingKey {
            case id, title, prompt, at, time, weekdays, enabled, launcher, effort
            case repeats = "repeat", everyMinutes = "every_minutes", machineID = "machine_id", sessionID = "session_id"
            case permissionMode = "permission_mode", createdBy = "created_by"
            case lastRun = "last_run", lastSessionID = "last_session_id", nextRun = "next_run"
            case lastStatus = "last_status", lastError = "last_error"
            case runMachineID = "run_machine_id", runSessionID = "run_session_id"
        }
        /// Where the next run goes when the task isn't continuing the conversation it came from.
        var ownConversation: SessionKey? {
            guard let m = runMachineID, let s = runSessionID else { return nil }
            return SessionKey(machineID: m, sessionID: s)
        }
        init(id: String = "", title: String, prompt: String, repeats: Repeat = .daily, at: Date? = nil, time: String = "09:00",
             weekdays: [Int] = [], everyMinutes: Int = 30, enabled: Bool = true) {
            self.id = id; self.title = title; self.prompt = prompt; self.repeats = repeats; self.at = at; self.time = time
            self.weekdays = weekdays; self.everyMinutes = everyMinutes; self.enabled = enabled
        }
        init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            func text(_ k: CodingKeys) -> String? { (try? c.decodeIfPresent(String.self, forKey: k)).flatMap { $0?.isEmpty == false ? $0 : nil } }
            func date(_ k: CodingKeys) -> Date? { (try? c.decodeIfPresent(Date.self, forKey: k)).flatMap { $0?.realDate } }
            id = try c.decode(String.self, forKey: .id)
            title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
            prompt = try c.decode(String.self, forKey: .prompt)
            repeats = text(.repeats).flatMap(Repeat.init(rawValue:)) ?? .once
            at = date(.at)
            time = text(.time) ?? "09:00"
            weekdays = try c.decodeIfPresent([Int].self, forKey: .weekdays) ?? []
            everyMinutes = try c.decodeIfPresent(Int.self, forKey: .everyMinutes) ?? 0
            enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
            machineID = text(.machineID); sessionID = text(.sessionID)
            launcher = text(.launcher); permissionMode = text(.permissionMode); effort = text(.effort)
            createdBy = text(.createdBy)
            lastRun = date(.lastRun); lastSessionID = text(.lastSessionID); lastStatus = text(.lastStatus); lastError = text(.lastError)
            nextRun = date(.nextRun)
            runMachineID = text(.runMachineID); runSessionID = text(.runSessionID)
        }
        func encode(to e: Encoder) throws { // only what the user edits; "" = the default
            var c = e.container(keyedBy: CodingKeys.self)
            try c.encode(id, forKey: .id); try c.encode(title, forKey: .title); try c.encode(prompt, forKey: .prompt)
            try c.encode(repeats, forKey: .repeats); try c.encodeIfPresent(at, forKey: .at)
            try c.encode(time, forKey: .time); try c.encode(weekdays, forKey: .weekdays); try c.encode(everyMinutes, forKey: .everyMinutes)
            try c.encode(enabled, forKey: .enabled); try c.encode(sessionID ?? "", forKey: .sessionID)
            try c.encode(permissionMode ?? "", forKey: .permissionMode)
            if includesModelSettings {
                try c.encode(launcher ?? "", forKey: .launcher)
                try c.encode(effort ?? "", forKey: .effort)
            }
        }
    }
    struct TasksBody: Decodable { var tasks: [ScheduledTask] }
    struct TaskBody: Codable { var task: ScheduledTask }
    func scheduledTasks() async throws -> [ScheduledTask] { try await get("/api/v1/assistant/tasks", as: TasksBody.self).tasks }
    /// Creates `t` (no id yet) or saves the user's changes to it; returns it as the hub has it.
    func saveTask(_ t: ScheduledTask, includesModelSettings: Bool = true) async throws -> ScheduledTask {
        var t = t
        t.includesModelSettings = includesModelSettings
        let data = t.id.isEmpty ? try await request("POST", "/api/v1/assistant/tasks", body: t)
                                : try await request("PUT", "/api/v1/assistant/tasks/\(t.id)", body: t)
        return try JSON.decoder.decode(TaskBody.self, from: data).task
    }
    func deleteTask(_ id: String) async throws { try await delete("/api/v1/assistant/tasks/\(id)") }
    struct TaskRun: Decodable {
        var sessionID: String
        var machineID: String
        enum CodingKeys: String, CodingKey { case sessionID = "session_id", machineID = "machine_id" }
    }
    /// 下次开新对话: the task's next run starts a new conversation of its own (the old one stays in 对话).
    func freshTaskConversation(_ id: String) async throws -> ScheduledTask {
        try await post("/api/v1/assistant/tasks/\(id)/fresh", body: [String: String](), as: TaskBody.self).task
    }
    /// Runs a task now instead of at its time; returns the conversation it went to.
    func runTask(_ id: String) async throws -> SessionKey {
        let r = try await post("/api/v1/assistant/tasks/\(id)/run", body: [String: String](), timeout: 130, as: TaskRun.self)
        return SessionKey(group: group, machineID: r.machineID, sessionID: r.sessionID)
    }

    /// One entry of the schedule's history: a routine or follow-up that ran, failed to start, or was skipped.
    struct ScheduleRun: Decodable, Identifiable, Hashable, Sendable {
        var id: String
        var kind: String          // routine / followup
        var refID: String
        var title: String
        var due: Date
        var at: Date
        var status: String        // ok / failed / skipped
        var error: String?
        var manual: Bool?
        var machineID: String?
        var sessionID: String?
        enum CodingKeys: String, CodingKey {
            case id, kind, title, due, at, status, error, manual
            case refID = "ref_id", machineID = "machine_id", sessionID = "session_id"
        }
        var key: SessionKey? {
            guard let machineID, let sessionID, !sessionID.isEmpty else { return nil }
            return SessionKey(machineID: machineID, sessionID: sessionID)
        }
    }
    struct RunsBody: Decodable { var runs: [ScheduleRun] }
    /// Newest first; `ref` limits it to one routine or follow-up.
    func scheduleRuns(ref: String? = nil) async throws -> [ScheduleRun] {
        try await get("/api/v1/assistant/runs", query: ["ref": ref ?? ""], as: RunsBody.self).runs
    }

    /// One file of the assistant's memory (Claude Code auto-memory): frontmatter (name / description / type) + body.
    /// MEMORY.md is the index the assistant keeps itself.
    struct MemoryFile: Decodable, Identifiable, Hashable, Sendable {
        var name: String
        var content: String
        var updatedAt: Date?
        var id: String { name }
        enum CodingKeys: String, CodingKey { case name, content; case updatedAt = "updated_at" }
    }
    struct MemoryBody: Decodable { var files: [MemoryFile] }
    func assistantMemory() async throws -> [MemoryFile] { try await get("/api/v1/assistant/memory", as: MemoryBody.self).files }
    func saveMemory(name: String, content: String) async throws {
        _ = try await request("PUT", "/api/v1/assistant/memory/\(name)", body: ["content": content])
    }
    func deleteMemory(name: String) async throws { try await delete("/api/v1/assistant/memory/\(name)") }
    struct NewConversation: Encodable {
        var prompt: String
        var images: [ImagePayload]?
        var launcher: String?
        var permissionMode: String?
        var effort: String?
        var main: Bool?
        enum CodingKeys: String, CodingKey { case prompt, images, launcher, effort, main; case permissionMode = "permission_mode" }
    }
    /// `main`: the new conversation becomes the main one.
    func startConversation(prompt: String, images: [ImagePayload] = [], launcher: String? = nil, permissionMode: String? = nil,
                           effort: String? = nil, main: Bool = false) async throws -> Session {
        stamp(try await post("/api/v1/assistant/conversations", body: NewConversation(prompt: prompt, images: images.isEmpty ? nil : images,
                                                                                    launcher: launcher, permissionMode: permissionMode, effort: effort,
                                                                                    main: main ? true : nil),
                             timeout: 110, as: SessionResponse.self).session)
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
    /// Codex's options (~/.codex/config.toml), the same shape as Claude's.
    func codexSettings(machine: String) async throws -> [ClaudeSetting] {
        try await get("/api/v1/machines/\(machine)/codex-settings", as: ClaudeSettingsResponse.self).settings
    }
    func setCodexSetting(machine: String, key: String, value: JSONValue) async throws -> [ClaudeSetting] {
        try JSON.decoder.decode(ClaudeSettingsResponse.self, from: try await request("PUT", "/api/v1/machines/\(machine)/codex-settings",
            body: SetSetting(key: key, value: value))).settings
    }
    struct SetSetting: Encodable { var key: String; var value: JSONValue }
    func setClaudeSetting(machine: String, key: String, value: JSONValue) async throws -> [ClaudeSetting] {
        try JSON.decoder.decode(ClaudeSettingsResponse.self, from: try await request("PUT", "/api/v1/machines/\(machine)/claude-settings",
            body: SetSetting(key: key, value: value))).settings
    }

    /// One Claude Code config directory of a machine (CLAUDE_CONFIG_DIR), as 设置 › Claude Code › 屏蔽的配置目录 lists it.
    struct ClaudeDir: Codable, Identifiable, Sendable, Equatable {
        var path: String
        var isDefault: Bool?
        var blocked: Bool?
        /// The rule that blocks it, as written (a glob, or its own path).
        var blockedBy: String?
        /// "default", "config" (hub.json), "found" (a running claude used it).
        var source: String
        var running: Int
        var sessions: Int?
        var id: String { path }
        enum CodingKeys: String, CodingKey {
            case path, isDefault = "default", blocked, blockedBy = "blocked_by", source, running, sessions
        }
    }
    struct ClaudeDirsState: Codable, Sendable, Equatable {
        var dirs: [ClaudeDir]
        var blocked: [String]
        var home: String?
        /// false: the hub's own record (the machine is offline or its node is older).
        var live: Bool
    }
    struct SetBlockedClaudeDirs: Encodable { var blocked: [String] }
    func claudeDirs(machine: String) async throws -> ClaudeDirsState {
        try await get("/api/v1/machines/\(machine)/claude-dirs", as: ClaudeDirsState.self)
    }
    func setBlockedClaudeDirs(machine: String, blocked: [String]) async throws -> ClaudeDirsState {
        try JSON.decoder.decode(ClaudeDirsState.self, from: try await request("PUT", "/api/v1/machines/\(machine)/claude-dirs/blocked",
            body: SetBlockedClaudeDirs(blocked: blocked)))
    }

    func interactions() async throws -> [Interaction] {
        stamp(try await get("/api/v1/interactions", as: InteractionsResponse.self).interactions)
    }
    func resolve(_ id: String, _ r: Resolution) async throws { try await post("/api/v1/interactions/\(id)/resolve", body: r) }

    struct DeviceBody: Encodable {
        var deviceID: String
        var name: String
        var kind: String
        var apnsToken: String?
        var apnsEnv: String?
        var topic: String?
        /// Push relay (docs/push-relay.md): this phone's public key, the relay and the grant it gave.
        var pushKey: String?
        var relayURL: String?
        var relayGrant: String?
        enum CodingKeys: String, CodingKey {
            case name, kind, topic
            case deviceID = "device_id", apnsToken = "apns_token", apnsEnv = "apns_env"
            case pushKey = "push_key", relayURL = "relay_url", relayGrant = "relay_grant"
        }
    }
    func register(device: DeviceBody) async throws { try await post("/api/v1/devices", body: device) }

    /// The Fleet Live Activity's tokens, or that it ended (hub: liveactivity.go).
    struct LiveActivityReport: Encodable {
        var activityID: String?
        var pushToken: String?
        var pushToStartToken: String?
        var ended: Bool?
        enum CodingKeys: String, CodingKey {
            case ended
            case activityID = "activity_id", pushToken = "push_token", pushToStartToken = "push_to_start_token"
        }
    }
    func reportLiveActivity(device: String, _ body: LiveActivityReport) async throws {
        try await post("/api/v1/devices/\(device)/live-activity", body: body)
    }
    struct FleetResponse: Decodable { var fleet: FleetState }
    func fleet() async throws -> FleetState { try await get("/api/v1/fleet", as: FleetResponse.self).fleet }

    /// Which pushes this device wants (hub: notify.go).
    func notifyPrefs(device: String) async throws -> NotifyPrefs { try await get("/api/v1/devices/\(device)/prefs") }
    /// 手机静音 alone (the PUT merges): nothing else this device chose is written back with it.
    func setMuteWhileDesktop(device: String, _ on: Bool) async throws -> NotifyPrefs {
        try JSON.decoder.decode(NotifyPrefs.self, from: try await request("PUT", "/api/v1/devices/\(device)/prefs", body: MuteWhileDesktopPut(on: on)))
    }
    /// The persona the hub keeps for this device's Live Activity; nil when it has none (an older hub never has one).
    func fleetLook(device: String) async throws -> FleetLook? {
        struct Prefs: Decodable { var look: FleetLook? }
        return try await get("/api/v1/devices/\(device)/prefs", as: Prefs.self).look
    }
    /// The persona this device's Live Activity draws, alone (the PUT merges): the hub echoes it in the device's pushes.
    func setFleetLook(device: String, _ look: FleetLook) async throws {
        _ = try await request("PUT", "/api/v1/devices/\(device)/prefs", body: FleetLookPut(look: look))
    }
    func setNotifyPrefs(device: String, _ prefs: NotifyPrefs) async throws -> NotifyPrefs {
        try JSON.decoder.decode(NotifyPrefs.self, from: try await request("PUT", "/api/v1/devices/\(device)/prefs", body: prefs))
    }
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

    /// A request that ran out of time: on the phone (URLError.timedOut) or behind the hub (a gateway timeout, a node
    /// call past its deadline). Not shown (2026-10-29 user request): the next poll, push or retry takes over.
    nonisolated var isTimeout: Bool {
        if let u = self as? URLError, u.code == .timedOut { return true }
        let ns = self as NSError
        if ns.domain == NSURLErrorDomain && ns.code == NSURLErrorTimedOut { return true }
        guard let api = self as? APIError else { return false }
        return api.status == 504 || APIError.isTimeoutText(api.message)
    }

    /// The request never got an answer because the phone has no network or cannot reach the hub (it may still have
    /// arrived when the link broke midway: networkConnectionLost). The title already says 等待网络 / 连接中….
    nonisolated var isOffline: Bool {
        let ns = self as NSError
        guard ns.domain == NSURLErrorDomain else { return false }
        return Self.offlineCodes.contains(ns.code)
    }

    nonisolated private static var offlineCodes: Set<Int> {
        [NSURLErrorNotConnectedToInternet, NSURLErrorNetworkConnectionLost, NSURLErrorCannotConnectToHost,
         NSURLErrorCannotFindHost, NSURLErrorDNSLookupFailed, NSURLErrorDataNotAllowed, NSURLErrorInternationalRoamingOff,
         NSURLErrorCallIsActive, NSURLErrorSecureConnectionFailed]
    }

    /// Nothing for the user to act on in a message: a cancellation, a timeout, or no network (2026-10-29: the title
    /// says it, a toast or a red line per failed request was noise).
    nonisolated var isQuiet: Bool { isCancellation || isTimeout || isOffline }

    /// The message to show, or nil for a cancellation, a timeout or no network.
    nonisolated var shown: String? { isQuiet ? nil : localizedDescription }
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
    /// Archives an assistant conversation (out of the sidebar, under 已归档) or brings it back.
    func archiveConversation(_ id: String, archived: Bool) async throws {
        try await post("/api/v1/assistant/conversations/\(id)/" + (archived ? "archive" : "unarchive"), body: [String: String]())
    }

    /// Renames the session's herdr tab and / or the Claude session (`/rename`).
    func renameSession(_ k: SessionKey, tabName: String?, title: String?) async throws {
        try await post("/api/v1/sessions/\(k.machineID)/\(k.sessionID)/rename", body: RenameBody(tabName: tabName, title: title))
    }
}


extension Date {
    /// nil for Go's zero time and other placeholder dates the hub sends for "never".
    nonisolated var realDate: Date? { timeIntervalSince1970 > 946_684_800 ? self : nil }
}

private nonisolated final class DownloadProgress: NSObject, URLSessionDownloadDelegate, Sendable {
    let report: @Sendable (Int64, Int64) -> Void
    init(_ report: @escaping @Sendable (Int64, Int64) -> Void) { self.report = report }
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {}
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64,
                    totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        report(totalBytesWritten, totalBytesExpectedToWrite)
    }
}

private nonisolated final class UploadProgress: NSObject, URLSessionTaskDelegate, Sendable {
    let report: @Sendable (Int64) -> Void
    init(_ report: @escaping @Sendable (Int64) -> Void) { self.report = report }
    func urlSession(_ session: URLSession, task: URLSessionTask, didSendBodyData bytesSent: Int64, totalBytesSent: Int64,
                    totalBytesExpectedToSend: Int64) {
        report(totalBytesSent)
    }
}
