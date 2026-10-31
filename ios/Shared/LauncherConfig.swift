import Foundation

/// 设置 › 模型 (docs/launchers.md): every launcher, from each machine's ~/.zshrc or added in Jarvis.
nonisolated struct LauncherView: Codable, Identifiable, Hashable, Sendable {
    var name: String
    var source: String // zshrc | jarvis
    var baseURL: String?
    var model: String?
    var smallModel: String?
    var args: [String]?
    var hasKey: Bool?
    var machines: [String]?
    var hidden: Bool?
    var definitions: [String: String]?
    /// What a ~/.zshrc launcher really sets, per machine (the node runs it with the agent stubbed out).
    var details: [String: LauncherDetail]?
    var id: String { name }
    var managed: Bool { source == "jarvis" }

    enum CodingKeys: String, CodingKey {
        case name, source, model, args, machines, hidden, definitions, details
        case baseURL = "base_url", smallModel = "small_model", hasKey = "has_key"
    }
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
    var args: [String]
    var apiKey: String?
    enum CodingKeys: String, CodingKey {
        case name, model, args
        case baseURL = "base_url", smallModel = "small_model", apiKey = "api_key"
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
    func saveLauncher(_ input: LauncherInput, isNew: Bool) async throws {
        _ = try await request(isNew ? "POST" : "PUT", isNew ? "/api/v1/launchers" : "/api/v1/launchers/\(input.name)", body: input)
    }
    /// A launcher added in Jarvis is deleted; a ~/.zshrc one is hidden in Jarvis (the file is not touched).
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
}
