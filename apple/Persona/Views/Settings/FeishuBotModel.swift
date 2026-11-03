import Foundation
import Observation

/// GET / PUT /api/v1/bots/feishu (docs/bots.md): the Feishu bot the user talks to Jarvis through.
nonisolated struct FeishuBotStatus: Codable, Hashable, Sendable {
    var enabled: Bool
    var appID: String
    var domain: String
    var hasSecret: Bool
    var fromConfig: Bool
    var features: Features
    var connected: Bool
    var error: String?
    var lastEventAt: Int64?
    var botName: String?
    var owner: Owner?
    var pairing: Pairing?
    var target: String?
    var agents: [BoundAgent]?

    nonisolated struct Features: Codable, Hashable, Sendable {
        var main: Bool
        var cards: Bool
        var allCards: Bool
        var pushes: Bool
        var sync: Bool
        var agents: Bool

        enum CodingKeys: String, CodingKey {
            case main, cards, pushes, sync, agents
            case allCards = "all_cards"
        }
    }

    nonisolated struct Owner: Codable, Hashable, Sendable {
        var name: String
        var pairedAt: Int64
        enum CodingKeys: String, CodingKey { case name; case pairedAt = "paired_at" }
    }

    nonisolated struct Pairing: Codable, Hashable, Sendable {
        var code: String
        var expiresAt: Int64
        enum CodingKeys: String, CodingKey { case code; case expiresAt = "expires_at" }
    }

    nonisolated struct BoundAgent: Codable, Hashable, Sendable {
        var title: String
        var machineID: String
        var sessionID: String
        enum CodingKeys: String, CodingKey { case title; case machineID = "machine_id", sessionID = "session_id" }
    }

    enum CodingKeys: String, CodingKey {
        case enabled, domain, features, connected, error, owner, pairing, target, agents
        case appID = "app_id", hasSecret = "has_secret", fromConfig = "from_config", lastEventAt = "last_event_at", botName = "bot_name"
    }
}

/// The edit sent to PUT /api/v1/bots/feishu: only what is set changes.
nonisolated struct FeishuBotUpdate: Encodable, Sendable {
    var enabled: Bool?
    var appID: String?
    var appSecret: String?
    var domain: String?
    var features: FeishuBotStatus.Features?

    enum CodingKeys: String, CodingKey {
        case enabled, domain, features
        case appID = "app_id", appSecret = "app_secret"
    }
}

extension APIClient {
    func feishuBot() async throws -> FeishuBotStatus { try await get("/api/v1/bots/feishu") }
    func updateFeishuBot(_ u: FeishuBotUpdate) async throws -> FeishuBotStatus {
        try JSON.decoder.decode(FeishuBotStatus.self, from: await request("PUT", "/api/v1/bots/feishu", body: u, timeout: 40))
    }
    func pairFeishuBot() async throws -> FeishuBotStatus { try await post("/api/v1/bots/feishu/pair") }
    func unpairFeishuBot() async throws -> FeishuBotStatus {
        try JSON.decoder.decode(FeishuBotStatus.self, from: await request("DELETE", "/api/v1/bots/feishu/owner"))
    }
    func testFeishuBot() async throws { try await post("/api/v1/bots/feishu/test") }
}

/// 设置 › 连接器 › 飞书机器人: load the bot's state, edit the credentials and switches, pair. Each change goes to the hub
/// at once and the page shows what the hub answered; a failed change leaves the page as it was.
@Observable final class FeishuBotModel {
    struct Deps {
        var api: () -> APIClient?
        var toast: (Toast) -> Void

        static var live: Deps {
            let model = AppModel.shared
            return Deps(api: { model.api }, toast: { model.toast = $0 })
        }
    }

    private(set) var status: FeishuBotStatus?
    private(set) var loading = false
    private(set) var saving = false
    private(set) var error: String?
    /// The credentials being typed (the secret is never read back from the hub).
    var appID = ""
    var appSecret = ""
    var domain = "feishu"
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    var features: FeishuBotStatus.Features {
        status?.features ?? .init(main: true, cards: true, allCards: false, pushes: false, sync: false, agents: false)
    }

    /// The domain picker's choice for what the hub resolved.
    static func domainChoice(_ resolved: String) -> String {
        if resolved.contains("larksuite") { return "lark" }
        if resolved.isEmpty || resolved.contains("open.feishu.cn") { return "feishu" }
        return resolved
    }

    var credentialsChanged: Bool {
        guard let st = status else { return !appID.isEmpty }
        return appID.trimmingCharacters(in: .whitespaces) != st.appID || !appSecret.isEmpty || domain != Self.domainChoice(st.domain)
    }

    func load() async {
        guard let api = deps.api() else { return }
        loading = true
        defer { loading = false }
        do {
            adopt(try await api.feishuBot())
            error = nil
        } catch {
            self.error = error.shown.map { "读不到机器人的状态：\($0)" }
        }
    }

    private func adopt(_ st: FeishuBotStatus) {
        let firstOrSame = status == nil || !credentialsChanged
        status = st
        if firstOrSame { // don't overwrite what is being typed
            appID = st.appID
            domain = Self.domainChoice(st.domain)
            appSecret = ""
        }
    }

    @discardableResult
    private func run(_ what: String, _ call: (APIClient) async throws -> FeishuBotStatus) async -> Bool {
        guard let api = deps.api() else { return false }
        saving = true
        defer { saving = false }
        do {
            let st = try await call(api)
            status = nil // what the hub has now, credentials included
            adopt(st)
            error = nil
            return true
        } catch {
            self.error = error.shown.map { "\(what)：\($0)" }
            return false
        }
    }

    func saveCredentials() async {
        let id = appID.trimmingCharacters(in: .whitespaces)
        let secret = appSecret.trimmingCharacters(in: .whitespaces)
        if await run("保存失败", { try await $0.updateFeishuBot(.init(appID: id, appSecret: secret.isEmpty ? nil : secret, domain: domain)) }) {
            deps.toast(Toast("应用凭证已验证并保存", style: .success))
        }
    }

    func setEnabled(_ on: Bool) async {
        await run(on ? "打开失败" : "关闭失败") { try await $0.updateFeishuBot(.init(enabled: on)) }
    }

    func set(_ key: WritableKeyPath<FeishuBotStatus.Features, Bool>, _ on: Bool) async {
        var f = features
        f[keyPath: key] = on
        if key == \.cards && !on { f.allCards = false }
        if key == \.allCards && on { f.cards = true }
        await run("保存失败") { try await $0.updateFeishuBot(.init(features: f)) }
    }

    func pair() async { await run("拿不到配对码") { try await $0.pairFeishuBot() } }

    func unpair() async {
        if await run("解除失败", { try await $0.unpairFeishuBot() }) { deps.toast(Toast("已解除配对", style: .success)) }
    }

    func test() async {
        guard let api = deps.api() else { return }
        saving = true
        defer { saving = false }
        do {
            try await api.testFeishuBot()
            deps.toast(Toast("已发一条测试消息到飞书", style: .success))
            error = nil
        } catch {
            self.error = error.shown.map { "测试消息没发出去：\($0)" }
        }
    }

    /// What the connection row says.
    var stateText: String {
        guard let st = status else { return loading ? "正在检查…" : "读不到状态" }
        if !st.enabled { return "未打开" }
        if !st.hasSecret { return "还没填应用凭证" }
        if !st.connected { return st.error.map { "连不上：\($0)" } ?? "正在连接…" }
        return st.owner == nil ? "已连接，等你配对" : "已连接"
    }

    /// The settings list's one-word summary.
    static func summary(_ st: FeishuBotStatus?) -> String {
        guard let st else { return "" }
        if !st.enabled { return "未打开" }
        if !st.connected { return "连不上" }
        return st.owner == nil ? "待配对" : "已连接"
    }
}
