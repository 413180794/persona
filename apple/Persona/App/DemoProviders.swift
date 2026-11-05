import Foundation
// i18n-ignore-file: demo mode stand-in for the hub: its made-up replies and errors, as the hub words them

/// 体验模式's 设置 › 模型供应商 (#2455): a provider kept in Persona, two from a made-up ~/.zshrc, and the wizard's and the
/// import's answers. Nothing reaches a gateway: 测试连接 and 获取模型列表 answer with made-up values, and no key is kept.
@MainActor
final class DemoProviders {
    private var managed: [String: JSONValue] = [:]
    private var order: [String] = []

    static let catalog: [JSONValue] = [
        ["id": "claude-opus-4-1", "name": "Claude Opus", "context_window": 200_000, "efforts": ["low", "medium", "high", "xhigh", "max"], "default_effort": "high"],
        ["id": "claude-sonnet-4-5", "name": "Claude Sonnet", "context_window": 1_000_000, "efforts": ["low", "medium", "high"], "default_effort": "medium"],
        ["id": "claude-haiku-4-5", "name": "Claude Haiku", "context_window": 200_000],
        ["id": "glm-5", "name": "GLM-5", "context_window": 200_000, "efforts": ["low", "high"]],
        ["id": "kimi-k2", "name": "Kimi K2", "context_window": 256_000]
    ]

    init() { reset() }

    func reset() {
        managed = ["demo_gateway": [
            "name": "demo_gateway", "source": "jarvis", "kind": "launcher", "api": "messages", "base_url": "https://gateway.example.com",
            "model": "glm-5", "has_key": true, "auth": "bearer", "has_headers": true,
            "models": [Self.catalog[3], Self.catalog[4]]
        ]]
        order = ["demo_gateway"]
    }

    private static let zshrc: [JSONValue] = [
        ["name": "cc_glm", "source": "zshrc", "base_url": "https://gateway.example.com", "model": "glm-5", "machines": ["macbook"]],
        ["name": "cc_kimi", "source": "zshrc", "base_url": "https://kimi.example.com", "model": "kimi-k2", "machines": ["macbook", "studio"]]
    ]

    /// The machine pickers' 「默认：…」 (agent.new's effective value).
    static let launcherDefault: JSONValue = ["provider": "launcher.claude", "model": "opus"]

    /// GET under /api/v1/launchers; nil: not one of these.
    func get(_ p: [String], _ q: [String: String]) throws -> JSONValue? {
        guard p.first == "launchers" else { return nil }
        switch p.count {
        case 1: return ["launchers": .array(order.compactMap { managed[$0] } + Self.zshrc)]
        case 2 where p[1] == "presets": return ["presets": .array([])]
        case 2 where p[1] == "import": return try preview(q["machine"] ?? "")
        case 2:
            guard let l = managed[p[1]] ?? Self.zshrc.first(where: { $0["name"]?.string == p[1] }) else { return nil }
            return ["launcher": l]
        default: return nil
        }
    }

    /// POST / PUT / DELETE under /api/v1/launchers; nil: not one of these.
    func change(_ method: String, _ p: [String], _ body: JSONValue) throws -> JSONValue? {
        guard p.first == "launchers" else { return nil }
        switch (method, p.count) {
        case ("POST", 2) where p[1] == "test":
            let model = body["model"]?.string ?? ""
            return ["ok": true, "ms": 820, "model": .string(model)]
        case ("POST", 2) where p[1] == "discover":
            return ["models": .array(Self.catalog)]
        case ("POST", 2) where p[1] == "import":
            let names = body["providers"]?.array?.compactMap(\.string) ?? []
            var created: [JSONValue] = []
            for n in names where managed[n] == nil {
                managed[n] = ["name": .string(n), "source": "jarvis", "kind": "launcher", "base_url": "https://gateway.example.com",
                              "model": "glm-5", "has_key": true, "origin": .string("zshrc:\(n)@\(body["machine"]?.string ?? "")"),
                              "models": [Self.catalog[3]]]
                order.append(n)
                created.append(.string(n))
            }
            return ["created": .array(created), "updated": .array([])]
        case ("POST", 1), ("PUT", 2):
            guard case .object(var l) = body, let name = l["name"]?.string, !name.isEmpty else {
                throw APIError(status: 400, message: "缺少名字")
            }
            let old = managed[name]?.object ?? [:]
            // Like the hub: the key and the headers are kept, never sent back.
            let key = l.removeValue(forKey: "api_key")?.string
            let headers = l.removeValue(forKey: "headers")
            l["source"] = "jarvis"
            l["has_key"] = .bool(key?.isEmpty == false || old["has_key"]?.bool == true)
            switch headers {
            case .some(.string(let h)): l["has_headers"] = .bool(!h.isEmpty)
            default: l["has_headers"] = old["has_headers"] ?? false
            }
            if managed[name] == nil { order.append(name) }
            managed[name] = .object(l)
            return ["ok": true]
        case ("DELETE", 2):
            managed[p[1]] = nil
            order.removeAll { $0 == p[1] }
            return ["ok": true]
        default: return nil
        }
    }

    private func preview(_ machine: String) throws -> JSONValue {
        // The demo's Mac mini stands for a computer whose personad is too old to read ~/.zshrc.
        if machine == "mini" { throw APIError(status: 409, message: "这台机器上的 personad 太旧，不能从 ~/.zshrc 导入，需要重新部署", fields: ["code": "node_outdated"]) }
        return ["machine": .string(machine), "providers": [
            ["name": "cc_glm", "base_url": "https://gateway.example.com", "auth": "bearer", "has_key": true,
             "models": [Self.catalog[3], ["id": "glm-5-air", "context_window": 128_000]], "model": "glm-5", "aliases": ["cc_glm", "cc_glm_air"],
             "existing": "demo_gateway"],
            ["name": "cc_kimi", "base_url": "https://kimi.example.com", "auth": "api_key", "has_key": true,
             "models": [Self.catalog[4]], "model": "kimi-k2", "aliases": ["cc_kimi"]]
        ]]
    }
}
