import Foundation

/// 设置 › 模型 (docs/launchers.md): every launcher, from each machine's ~/.zshrc or added in Persona.
nonisolated struct LauncherView: Codable, Identifiable, Hashable, Sendable {
    var name: String
    var source: String // zshrc | jarvis
    var baseURL: String?
    var model: String?
    var smallModel: String?
    /// Subagents (workflows, multi-agent runs): "" follows the model.
    var subagentModel: String?
    /// An extra /model entry at the gateway.
    var customModel: String?
    var customModelName: String?
    var customModelDescription: String?
    var args: [String]?
    var hasKey: Bool?
    var kind: String?
    var api: String?
    /// Managed: how the key goes to the gateway, `api_key` (x-api-key, the default) or `bearer`.
    var auth: String?
    var machines: [String]?
    var hidden: Bool?
    var definitions: [String: String]?
    /// What a ~/.zshrc launcher really sets, per machine (the node runs it with the agent stubbed out).
    var details: [String: LauncherDetail]?
    /// A managed provider as cindy models it (#2455): its models, the model per Claude Code tier and the gateway's needs.
    var models: [ProviderModel]?
    var opusModel: String?
    var sonnetModel: String?
    var haikuModel: String?
    var fableModel: String?
    var subagentForce: Bool?
    /// Custom request headers stay sealed at the hub, never sent back: only whether there are some.
    var hasHeaders: Bool?
    var proxy: String?
    var noProxy: String?
    var timeoutMS: Int?
    var compactWindow: Int?
    var maxContext: Int?
    var effort: String?
    /// Where a managed provider came from: "" by hand, "zshrc:<alias>@<machine>" imported, "preset:<id>".
    var origin: String?
    var id: String { name }
    var managed: Bool { source == "jarvis" }

    enum CodingKeys: String, CodingKey {
        case name, source, model, args, machines, hidden, definitions, details, kind, api, auth, models, proxy, effort, origin
        case baseURL = "base_url", smallModel = "small_model", hasKey = "has_key"
        case subagentModel = "subagent_model", customModel = "custom_model"
        case customModelName = "custom_model_name", customModelDescription = "custom_model_description"
        case opusModel = "opus_model", sonnetModel = "sonnet_model", haikuModel = "haiku_model", fableModel = "fable_model"
        case subagentForce = "subagent_force", hasHeaders = "has_headers", noProxy = "no_proxy", timeoutMS = "timeout_ms"
        case compactWindow = "compact_window", maxContext = "max_context"
    }

    var bearerAuth: Bool { auth == "bearer" }
}

/// One model a provider's gateway serves (proto.ProviderModel, cindy's ProviderRuntimeModelConfig).
nonisolated struct ProviderModel: Codable, Hashable, Sendable, Identifiable {
    var id: String
    var name: String?
    /// Tokens; nil or 0: unknown (Claude Code assumes 200K).
    var contextWindow: Int?
    /// The thinking levels it takes; nil or empty: unknown (all are offered).
    var efforts: [String]?
    var defaultEffort: String?

    enum CodingKeys: String, CodingKey {
        case id, name, efforts
        case contextWindow = "context_window", defaultEffort = "default_effort"
    }

    init(id: String, name: String? = nil, contextWindow: Int? = nil, efforts: [String]? = nil, defaultEffort: String? = nil) {
        self.id = id
        self.name = name
        self.contextWindow = contextWindow
        self.efforts = efforts
        self.defaultEffort = defaultEffort
    }

    var title: String { name?.isEmpty == false ? name! : id }
}

/// One model a launcher offers in its /model menu (the chat's model picker, node/launcher_models.go).
nonisolated struct AgentModelOption: Decodable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String?
    var resolved: String?
    var isDefault: Bool?
    enum CodingKeys: String, CodingKey { case id, name, resolved, isDefault = "default" }
    var title: String { name ?? id }
}

nonisolated struct LauncherDetail: Codable, Hashable, Sendable {
    struct ModelTier: Codable, Hashable, Sendable { var role: String; var env: String; var model: String }
    struct EnvVar: Codable, Hashable, Sendable { var key: String; var value: String }
    var agent: String?
    var baseURL: String?
    var auth: String?
    var models: [ModelTier]?
    var contextWindow: Int?
    var compactWindow: Int?
    var effort: String?
    var timeoutMS: Int?
    var args: [String]?
    var env: [EnvVar]?
    var error: String?
    enum CodingKeys: String, CodingKey {
        case agent, auth, models, effort, args, env, error
        case baseURL = "base_url", contextWindow = "context_window", compactWindow = "compact_window", timeoutMS = "timeout_ms"
    }

    var agentName: String {
        switch agent {
        case "codex": "Codex"
        case "traex": "TRAEX"
        default: "Claude Code"
        }
    }

    /// 786432 → "786K", 1000000 → "1M".
    static func tokens(_ n: Int) -> String {
        if n >= 1_000_000, n % 100_000 == 0 { return String(format: n % 1_000_000 == 0 ? "%.0fM" : "%.1fM", Double(n) / 1_000_000) }
        if n >= 1_000 { return "\(n / 1_000)K" }
        return "\(n)"
    }
}

/// One 测试可用性 run on a machine: a real one-word request through the launcher.
nonisolated struct LauncherCheck: Codable, Hashable, Sendable {
    var ok: Bool
    var agent: String?
    var model: String?
    var answer: String?
    var millis: Int
    var error: String?
}

/// A launcher added or changed in the app; an empty `apiKey` on a change keeps the key.
nonisolated struct LauncherInput: Encodable, Sendable {
    var name: String
    var baseURL: String
    var model: String
    var smallModel: String
    var subagentModel: String
    var customModel: String
    var customModelName: String
    var customModelDescription: String
    var args: [String]
    var apiKey: String?
    var kind: String? = nil
    var api: String? = nil
    /// A plugin's provider it is added from (`<plugin>/<provider>`): the hub fills in what is left empty.
    var preset: String? = nil
    var auth: String? = nil
    /// The provider's fields (#2455). nil is left out: a form that does not set them changes nothing at the hub.
    var models: [ProviderModel]? = nil
    var opusModel: String? = nil
    var sonnetModel: String? = nil
    var haikuModel: String? = nil
    var fableModel: String? = nil
    var subagentForce: Bool? = nil
    /// ANTHROPIC_CUSTOM_HEADERS: nil keeps what the hub has, "" clears it (a secret like the key).
    var headers: String? = nil
    var proxy: String? = nil
    var noProxy: String? = nil
    var timeoutMS: Int? = nil
    var compactWindow: Int? = nil
    var maxContext: Int? = nil
    var effort: String? = nil
    enum CodingKeys: String, CodingKey {
        case name, model, args, kind, api, preset, auth, models, headers, proxy, effort
        case baseURL = "base_url", smallModel = "small_model", apiKey = "api_key"
        case subagentModel = "subagent_model", customModel = "custom_model"
        case customModelName = "custom_model_name", customModelDescription = "custom_model_description"
        case opusModel = "opus_model", sonnetModel = "sonnet_model", haikuModel = "haiku_model", fableModel = "fable_model"
        case subagentForce = "subagent_force", noProxy = "no_proxy", timeoutMS = "timeout_ms"
        case compactWindow = "compact_window", maxContext = "max_context"
    }
}

/// What 测试连接 and 获取模型列表 send (POST /api/v1/launchers/test, …/discover): the form as typed; with `name` the hub
/// uses the stored key and headers where these are nil.
nonisolated struct LauncherProbe: Encodable, Hashable, Sendable {
    var name: String?
    var baseURL: String?
    var auth: String?
    var apiKey: String?
    var headers: String?
    var model: String?
    enum CodingKeys: String, CodingKey {
        case name, auth, headers, model
        case baseURL = "base_url", apiKey = "api_key"
    }
}

/// 测试连接's answer: one tiny request through the gateway. A gateway's failure is `ok: false` with `error`.
nonisolated struct LauncherProbeResult: Decodable, Hashable, Sendable {
    var ok: Bool
    var ms: Int?
    var model: String?
    var error: String?
}

/// 获取模型列表's answer: the gateway's /v1/models, filled in from the hub's catalog.
nonisolated struct LauncherDiscovery: Decodable, Hashable, Sendable {
    var models: [ProviderModel]?
    var error: String?
}

/// 从 ~/.zshrc 导入's preview (GET /api/v1/launchers/import): a machine's claude aliases merged into providers by gateway
/// and key. No key or header is in it.
nonisolated struct LauncherImportPreview: Decodable, Hashable, Sendable {
    var machine: String
    var providers: [Provider]

    nonisolated struct Provider: Decodable, Hashable, Sendable, Identifiable {
        var name: String
        var baseURL: String?
        var auth: String?
        var hasKey: Bool?
        var models: [ProviderModel]?
        var model: String?
        var subagentModel: String?
        var effort: String?
        var hasHeaders: Bool?
        var proxy: String?
        var aliases: [String]?
        /// The stored provider with the same gateway and key: importing merges the models into it.
        var existing: String?
        var id: String { name }

        enum CodingKeys: String, CodingKey {
            case name, auth, models, model, effort, proxy, aliases, existing
            case baseURL = "base_url", hasKey = "has_key", subagentModel = "subagent_model", hasHeaders = "has_headers"
        }
    }
}

nonisolated struct LauncherImportResult: Decodable, Hashable, Sendable {
    var created: [String]?
    var updated: [String]?
}

/// What a machine's launcher picker runs with no pick (`default` of GET /api/v1/machines/{id}/launchers): 模型配置's
/// effective value for the use.
nonisolated struct LauncherDefault: Codable, Hashable, Sendable {
    /// `launcher.<name>`, as in 模型配置.
    var provider: String?
    var model: String?
    var effort: String?
    var runtime: String?

    /// The launcher's own name (the provider id without `launcher.`).
    var launcher: String? {
        guard let p = provider, !p.isEmpty else { return nil }
        return p.hasPrefix("launcher.") ? String(p.dropFirst("launcher.".count)) : p
    }

    /// 「默认：jv_strong · glm-5」; nil when there is no default launcher.
    var label: String? {
        guard let l = launcher else { return nil }
        let m = model.flatMap { $0.isEmpty ? nil : (Fmt.model($0) ?? $0) }
        return "默认：" + [l, m].compactMap { $0 }.joined(separator: " · ")
    }
}

/// A model provider a plugin offers (GET /api/v1/launchers/presets, docs/extensions.md「模型供应商」): a launcher added from
/// it takes the address, kind and models from here; the user fills in only the name and the key.
nonisolated struct LauncherPreset: Decodable, Hashable, Sendable, Identifiable {
    nonisolated struct Model: Decodable, Hashable, Sendable, Identifiable {
        var id: String
        var name: String?
        var label: String { name?.isEmpty == false ? name! : id }
    }
    /// `<plugin>/<provider>`.
    var id: String
    var plugin: String
    var name: String
    var description: String?
    /// launcher | api
    var kind: String
    var api: String?
    var baseURL: String
    var model: String?
    var smallModel: String?
    var models: [Model]?
    /// What the key looks like or where to get one.
    var keyHint: String?
    /// Why the plugin's own list of models could not be had (the declared ones are still there).
    var modelsError: String?

    enum CodingKeys: String, CodingKey {
        case id, plugin, name, description, kind, api, model, models
        case baseURL = "base_url", smallModel = "small_model", keyHint = "key_hint", modelsError = "models_error"
    }

    /// The model a launcher gets when none is picked: the declared default, else the first listed.
    var defaultModel: String { model?.isEmpty == false ? model! : models?.first?.id ?? "" }
    /// What can be picked: the listed models, with the default first when it is not among them.
    var choices: [Model] {
        let listed = models ?? []
        let d = defaultModel
        if d.isEmpty || listed.contains(where: { $0.id == d }) { return listed }
        return [Model(id: d)] + listed
    }
}

extension APIClient {
    func allLaunchers() async throws -> [LauncherView] {
        struct R: Decodable { var launchers: [LauncherView] }
        let r: R = try await get("/api/v1/launchers")
        return r.launchers
    }
    func launcher(_ name: String) async throws -> LauncherView {
        struct R: Decodable { var launcher: LauncherView }
        let r: R = try await get("/api/v1/launchers/\(name)")
        return r.launcher
    }
    /// The models this launcher's agent offers in its /model menu on this machine (the chat's model picker).
    func launcherModels(machine: String, launcher: String) async throws -> [AgentModelOption] {
        struct R: Decodable { var models: [AgentModelOption]? }
        let r: R = try await get("/api/v1/machines/\(machine)/launchers/\(launcher)/models")
        return r.models ?? []
    }
    func saveLauncher(_ input: LauncherInput, isNew: Bool) async throws {
        _ = try await request(isNew ? "POST" : "PUT", isNew ? "/api/v1/launchers" : "/api/v1/launchers/\(input.name)", body: input)
    }
    /// A launcher added in Persona is deleted; a ~/.zshrc one is hidden in Persona (the file is not touched).
    /// The providers plugins offer; none from a hub without the plugin base (it answers 404).
    func launcherPresets() async throws -> [LauncherPreset] {
        struct R: Decodable { var presets: [LauncherPreset]? }
        do {
            let r: R = try await get("/api/v1/launchers/presets")
            return r.presets ?? []
        } catch let e as APIError where e.status == 404 {
            return []
        }
    }
    func deleteLauncher(_ name: String) async throws {
        _ = try await request("DELETE", "/api/v1/launchers/\(name)")
    }
    func checkLauncher(_ name: String, machine: String?) async throws -> LauncherCheck {
        struct R: Decodable { var check: LauncherCheck }
        let data = try await request("POST", "/api/v1/launchers/\(name)/check", query: machine.map { ["machine": $0] } ?? [:], timeout: 110)
        return try JSON.decoder.decode(R.self, from: data).check
    }
    func restoreLauncher(_ name: String) async throws {
        _ = try await request("POST", "/api/v1/launchers/\(name)/restore")
    }
    /// 测试连接: the hub sends one max_tokens 1 request through the gateway (30 s at most).
    func testLauncher(_ probe: LauncherProbe) async throws -> LauncherProbeResult {
        try await post("/api/v1/launchers/test", body: probe, timeout: 45)
    }
    /// 获取模型列表: the gateway's own list.
    func discoverLauncherModels(_ probe: LauncherProbe) async throws -> LauncherDiscovery {
        try await post("/api/v1/launchers/discover", body: probe, timeout: 45)
    }
    /// 从 ~/.zshrc 导入: what the machine's claude aliases come to (409 `node_outdated` from a node too old to say).
    func launcherImportPreview(machine: String) async throws -> LauncherImportPreview {
        try JSON.decoder.decode(LauncherImportPreview.self,
                                from: await request("GET", "/api/v1/launchers/import", query: ["machine": machine], timeout: 45))
    }
    func importLaunchers(machine: String, providers: [String]) async throws -> LauncherImportResult {
        struct Body: Encodable { var machine: String; var providers: [String] }
        return try await post("/api/v1/launchers/import", body: Body(machine: machine, providers: providers), timeout: 45)
    }
}

/// A launcher's quota at its gateway (GET /api/v1/launchers/{name}/quota, docs/launchers.md「额度」), as a quota plugin
/// read it; `error` when there is nothing to read (`unsupported`, no plugin knows the gateway) or the gateway said no.
nonisolated struct LauncherQuotaAnswer: Decodable, Hashable, Sendable {
    var machine: String?
    var checkedAt: Date?
    var quota: LauncherQuota?
    var error: String?
    var unsupported: Bool?

    enum CodingKeys: String, CodingKey { case machine, quota, error, unsupported; case checkedAt = "checked_at" }
}

nonisolated struct LauncherQuota: Decodable, Hashable, Sendable {
    /// The gateway's id (the plugin's contribution).
    var gateway: String
    /// What to call it, from the hub (an older hub leaves it out: the id is shown).
    var gatewayLabel: String?
    var keyName: String?
    var usage: [Usage]?
    var budget: [Window]?
    var pools: [Pool]?
    var note: String?

    enum CodingKeys: String, CodingKey {
        case gateway, usage, budget, pools, note
        case keyName = "key_name", gatewayLabel = "gateway_name"
    }

    var gatewayName: String {
        if let n = gatewayLabel?.trimmingCharacters(in: .whitespaces), !n.isEmpty { return n }
        return gateway.isEmpty ? "gateway" : gateway
    }

    nonisolated struct Usage: Decodable, Hashable, Sendable {
        var label: String
        var requests: Int64
        var tokens: Int64
        var costUSD: Double?
        enum CodingKeys: String, CodingKey { case label, requests, tokens; case costUSD = "cost_usd" }
    }

    nonisolated struct Pool: Decodable, Hashable, Sendable {
        var name: String
        var state: String?
        var detail: String?
        var windows: [Window]?
        var models: [ModelWindows]?
    }

    nonisolated struct ModelWindows: Decodable, Hashable, Sendable {
        var model: String
        var windows: [Window]
    }

    nonisolated struct Window: Decodable, Hashable, Sendable {
        var label: String
        var usedRatio: Double?
        var used: Double?
        var limit: Double?
        var unit: String?
        var resetsAt: Date?
        var exhausted: Bool?
        enum CodingKeys: String, CodingKey {
            case label, used, limit, unit, exhausted
            case usedRatio = "used_ratio", resetsAt = "resets_at"
        }
    }
}

extension APIClient {
    /// `refresh`: read the gateway again now (the hub keeps an answer a minute).
    func launcherQuota(_ name: String, machine: String? = nil, refresh: Bool = false) async throws -> LauncherQuotaAnswer {
        var q: [String: String] = [:]
        if let machine { q["machine"] = machine }
        if refresh { q["refresh"] = "1" }
        let data = try await request("GET", "/api/v1/launchers/\(name)/quota", query: q, timeout: 50)
        return try JSON.decoder.decode(LauncherQuotaAnswer.self, from: data)
    }
}
