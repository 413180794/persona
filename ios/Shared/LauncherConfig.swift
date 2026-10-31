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
    var id: String { name }
    var managed: Bool { source == "jarvis" }

    enum CodingKeys: String, CodingKey {
        case name, source, model, args, machines, hidden, definitions
        case baseURL = "base_url", smallModel = "small_model", hasKey = "has_key"
    }
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
    func restoreLauncher(_ name: String) async throws {
        _ = try await request("POST", "/api/v1/launchers/\(name)/restore")
    }
}
