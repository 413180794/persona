import Foundation
import Observation

// 设置 › 插件 (docs/plugins.md「hub 的接口」): the hub's plugins, each one's switch, its configuration as a form drawn
// from its schema (`form.fields`), its secrets, write-only, and the pages it draws (`ui`, PluginUIModel). Installing,
// upgrading and removing are done on the hub's machine (`personad plugin`), or from a plugin market the hub has
// (PluginMarketModel: only what a market lists).

/// One of GET /api/v1/plugins.
nonisolated struct HubPlugin: Codable, Hashable, Sendable, Identifiable {
    var id: String
    var name: String
    var kind: String
    /// disabled | needs_setup | incompatible | stopped | starting | running | error (an older hub: starting | running |
    /// error | disabled).
    var status: String
    var error: String?
    var version: String?
    /// builtin | hub.json | installed (nil: a hub from before the plugin base).
    var source: String?
    var enabled: Bool?
    var description: String?
    /// The extension points it contributes to, with the ids.
    var contributes: [String: [String]]?
    /// What it still needs before it runs, in words.
    var needsSetup: [String]?
    var warnings: [String]?

    enum CodingKeys: String, CodingKey {
        case id, name, kind, status, error, version, source, enabled, description, contributes, warnings
        case needsSetup = "needs_setup"
    }
}

/// One field of a plugin's configuration form (`form.fields`): the hub's reading of the manifest's JSON Schema subset.
nonisolated struct PluginField: Codable, Hashable, Sendable, Identifiable {
    var name: String
    /// string | integer | number | boolean | array | object
    var type: String
    var title: String?
    var description: String?
    var `default`: JSONValue?
    var required: Bool?
    var `enum`: [PluginChoice]?
    var format: String?
    var pattern: String?
    var minLength: Int?
    var maxLength: Int?
    var minimum: Double?
    var maximum: Double?
    /// An array's items (strings, maybe from a list of choices).
    var items: PluginItems?
    var minItems: Int?
    var maxItems: Int?
    var uniqueItems: Bool?
    /// An object's fields (a group of the form).
    var fields: [PluginField]?

    var id: String { name }
    var label: String { title?.isEmpty == false ? title! : name }

    enum CodingKeys: String, CodingKey {
        case name, type, title, description, `default`, required, `enum`, format, pattern, minimum, maximum, items, fields
        case minLength = "min_length", maxLength = "max_length", minItems = "min_items", maxItems = "max_items"
        case uniqueItems = "unique_items"
    }
}

/// An array field's items.
nonisolated struct PluginItems: Codable, Hashable, Sendable {
    var type: String
    var `enum`: [PluginChoice]?
    /// machine: the hub's machines by id, drawn as a switch each (`PluginMachineSwitches`).
    var format: String?
    /// For machine: only machines of these systems (darwin, linux, windows); nil, any.
    var os: [String]?
}

/// One value of a field's choices, with its label.
nonisolated struct PluginChoice: Codable, Hashable, Sendable, Identifiable {
    var value: JSONValue
    var title: String?
    var id: String { value.string ?? "\(value)" }
    var label: String { title?.isEmpty == false ? title! : (value.string ?? "") }
}

/// A declared secret and whether it has a value (never the value).
nonisolated struct PluginSecret: Codable, Hashable, Sendable, Identifiable {
    var name: String
    var title: String
    var description: String?
    var required: Bool?
    var set: Bool
    var id: String { name }
}

/// A line of what a plugin may do (`permissions`).
nonisolated struct PluginPermission: Codable, Hashable, Sendable, Identifiable {
    var key: String
    var title: String
    var detail: String?
    var id: String { key }
}

/// GET /api/v1/plugins/{id}/-/: the plugin with its form, settings, secrets and permissions.
nonisolated struct HubPluginDetail: Decodable, Hashable, Sendable {
    var plugin: HubPlugin
    var publisher: String?
    var homepage: String?
    var origin: String?
    var previousVersion: String?
    /// hub.json's: switched and set up there only.
    var readOnly: Bool
    var form: [PluginField]
    var config: [String: JSONValue]
    var secrets: [PluginSecret]
    var permissions: [PluginPermission]
    /// The pages it draws (placement plugin_page), a row each.
    var ui: [PluginUIPlace]

    private struct Form: Decodable { var fields: [PluginField]? }
    private enum CodingKeys: String, CodingKey {
        case publisher, homepage, origin, form, config, secrets, permissions, ui
        case previousVersion = "previous_version", readOnly = "read_only"
    }

    init(from decoder: Decoder) throws {
        plugin = try HubPlugin(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        publisher = try c.decodeIfPresent(String.self, forKey: .publisher)
        homepage = try c.decodeIfPresent(String.self, forKey: .homepage)
        origin = try c.decodeIfPresent(String.self, forKey: .origin)
        previousVersion = try c.decodeIfPresent(String.self, forKey: .previousVersion)
        readOnly = try c.decodeIfPresent(Bool.self, forKey: .readOnly) ?? false
        form = try c.decodeIfPresent(Form.self, forKey: .form)?.fields ?? []
        config = try c.decodeIfPresent([String: JSONValue].self, forKey: .config) ?? [:]
        secrets = try c.decodeIfPresent([PluginSecret].self, forKey: .secrets) ?? []
        permissions = try c.decodeIfPresent([PluginPermission].self, forKey: .permissions) ?? []
        ui = (try c.decodeIfPresent([PluginUIPlace].self, forKey: .ui) ?? []).filter { ($0.placement ?? "plugin_page") == "plugin_page" }
    }
}

/// GET /api/v1/plugins: the plugins, and whether this hub has plugin markets (`markets`: 设置 › 插件 › 插件市场).
nonisolated struct HubPluginList: Decodable, Sendable {
    var plugins: [HubPlugin]
    var canInstall: Bool
    var markets: Bool

    private enum CodingKeys: String, CodingKey {
        case plugins, markets
        case canInstall = "can_install"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        plugins = try c.decodeIfPresent([HubPlugin].self, forKey: .plugins) ?? []
        canInstall = try c.decodeIfPresent(Bool.self, forKey: .canInstall) ?? false
        markets = try c.decodeIfPresent(Bool.self, forKey: .markets) ?? false
    }
}

extension APIClient {
    func plugins() async throws -> [HubPlugin] { try await pluginList().plugins }

    func pluginList() async throws -> HubPluginList { try await get("/api/v1/plugins") }

    func plugin(_ id: String) async throws -> HubPluginDetail { try await get("/api/v1/plugins/\(id)/-/") }

    func setPlugin(_ id: String, enabled: Bool) async throws -> HubPluginDetail {
        try JSON.decoder.decode(HubPluginDetail.self, from: try await request("PUT", "/api/v1/plugins/\(id)/-/enabled", body: ["enabled": enabled]))
    }

    func setPlugin(_ id: String, config: [String: JSONValue]) async throws -> HubPluginDetail {
        struct Body: Encodable { var config: [String: JSONValue] }
        return try JSON.decoder.decode(HubPluginDetail.self, from: try await request("PUT", "/api/v1/plugins/\(id)/-/config", body: Body(config: config)))
    }

    func setPlugin(_ id: String, secret name: String, value: String) async throws -> HubPluginDetail {
        try JSON.decoder.decode(HubPluginDetail.self, from: try await request("PUT", "/api/v1/plugins/\(id)/-/secrets/\(name)", body: ["value": value]))
    }

    func clearPlugin(_ id: String, secret name: String) async throws -> HubPluginDetail {
        try JSON.decoder.decode(HubPluginDetail.self, from: try await request("DELETE", "/api/v1/plugins/\(id)/-/secrets/\(name)"))
    }
}

/// What a plugin is called and says in the settings.
enum PluginText {
    static func state(_ p: HubPlugin) -> String {
        switch p.status {
        case "running": "运行中"
        case "stopped": "空闲，用到时启动"
        case "starting": "正在启动"
        case "needs_setup": "需要设置"
        case "disabled": "已停用"
        case "incompatible": "这台 hub 用不了"
        case "error": "出错了"
        default: "状态未知"
        }
    }

    /// The tone of the state's dot: red for broken, yellow for waiting on the user, green running, gray otherwise.
    static func isProblem(_ p: HubPlugin) -> Bool { p.status == "error" || p.status == "incompatible" }

    static func source(_ s: String?) -> String? {
        switch s {
        case "builtin": "内置"
        case "hub.json": "hub.json"
        case "installed": "已安装"
        default: nil
        }
    }

    static func point(_ name: String) -> String {
        switch name {
        case "connectors": "连接器"
        case "voice": "语音对话"
        default: name
        }
    }

    /// The row's value: its state (the version is on its page).
    static func summary(_ p: HubPlugin) -> String { state(p) }

    /// Under the form: which switches take effect at once (lists of machines, `PluginModel.saveNow`) and that the rest
    /// waits for 保存.
    static func saveFooter(_ fields: [PluginField]) -> String {
        let all = fields.flatMap { $0.type == "object" ? ($0.fields ?? []) : [$0] }
        let now = all.filter { $0.items?.format == "machine" }.map { "「\($0.label)」" }
        if now.isEmpty { return "改完点右上角「保存」；保存后插件会重新启动。留空的项用插件的默认值。" }
        if now.count == all.count { return now.joined(separator: "、") + "的开关即时生效，不用保存。" }
        return now.joined(separator: "、") + "的开关即时生效；其他项改完点右上角「保存」，保存后插件会重新启动。留空的项用插件的默认值。"
    }

    /// 设置's row: how many, and how many need the user.
    static func listSummary(_ list: [HubPlugin]?) -> String {
        guard let list else { return "" }
        if list.isEmpty { return "没有" }
        let attention = list.filter { $0.status == "needs_setup" || isProblem($0) }.count
        return attention > 0 ? "\(list.count) 个 · \(attention) 个要处理" : "\(list.count) 个"
    }
}

/// 设置 › 插件: the list.
@Observable final class PluginsModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    private(set) var plugins: [HubPlugin]?
    /// The hub has plugin markets: the list shows 插件市场.
    private(set) var markets = false
    private(set) var loading = false
    private(set) var error: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    func load() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
            let list = try await api.pluginList()
            plugins = list.plugins
            markets = list.markets
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// 设置 › 插件 › one: its switch, its form (a draft saved as a whole), its secrets.
@Observable final class PluginModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    let id: String
    private(set) var detail: HubPluginDetail?
    private(set) var loading = false
    private(set) var saving = false
    private(set) var error: String?
    /// The hub's word on a field it refused, by its dotted path ("proxy.host").
    private(set) var fieldErrors: [String: String] = [:]
    /// The configuration being edited; saved as a whole (PUT replaces it).
    var draft: [String: JSONValue] = [:]
    /// What number fields hold as typed, by path, until they read as a number.
    var numberText: [String: String] = [:]
    @ObservationIgnored private let deps: Deps

    init(id: String, deps: Deps = .live) {
        self.id = id
        self.deps = deps
    }

    var dirty: Bool { detail.map { draft != $0.config } ?? false || !numberProblems.isEmpty }

    /// Number fields typed into that do not read as one, by path.
    var numberProblems: [String: String] {
        numberText.compactMapValues { text in
            text.trimmingCharacters(in: .whitespaces).isEmpty || Double(text.trimmingCharacters(in: .whitespaces)) != nil ? nil : "要是一个数"
        }
    }

    var canSave: Bool { dirty && numberProblems.isEmpty && !saving }

    func load() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        await run { api in try await api.plugin(self.id) }
    }

    func setEnabled(_ on: Bool) async {
        await run { api in try await api.setPlugin(self.id, enabled: on) }
    }

    func save() async -> Bool {
        guard canSave else { return false }
        saving = true
        defer { saving = false }
        fieldErrors = [:]
        let ok = await run { api in try await api.setPlugin(self.id, config: Self.compact(self.draft)) }
        if ok { numberText = [:] }
        return ok
    }

    /// Saves one field at once (a machine's switch) on top of what the hub has, keeping the rest of what is being
    /// edited for 保存.
    func saveNow(_ value: JSONValue?, at path: [String]) async {
        guard let saved = detail?.config else { return }
        let editing = draft
        set(value, at: path) // shows at once
        saving = true
        defer { saving = false }
        let ok = await run { api in try await api.setPlugin(self.id, config: Self.compact(Self.setting(saved, path[...], value))) }
        // run took the hub's answer as the draft: the other edits go back on top of it.
        draft = Self.setting(editing, path[...], ok ? self.value(path) : Self.value(in: saved, path))
    }

    func setSecret(_ name: String, value: String) async -> Bool {
        let value = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else { return false }
        return await run { api in try await api.setPlugin(self.id, secret: name, value: value) }
    }

    func clearSecret(_ name: String) async {
        await run { api in try await api.clearPlugin(self.id, secret: name) }
    }

    /// Puts the hub's answer in (the draft follows it), or its error: a field it refused goes beside that field.
    @discardableResult private func run(_ call: @escaping (APIClient) async throws -> HubPluginDetail) async -> Bool {
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
            let d = try await call(api)
            detail = d
            draft = d.config
            error = nil
            return true
        } catch let e as APIError where e.code == "invalid_config" {
            fieldErrors = [e.fields["field"] ?? "": e.fields["message"] ?? e.message]
            error = nil
            return false
        } catch {
            self.error = error.localizedDescription
            return false
        }
    }

    // MARK: the draft by path

    func value(_ path: [String]) -> JSONValue? { Self.value(in: draft, path) }

    static func value(in obj: [String: JSONValue], _ path: [String]) -> JSONValue? {
        var cur: JSONValue = .object(obj)
        for key in path {
            guard let next = cur[key] else { return nil }
            cur = next
        }
        return cur == .null ? nil : cur
    }

    /// Sets (nil: removes) the value at path; an emptied group goes too.
    func set(_ value: JSONValue?, at path: [String]) {
        draft = Self.setting(draft, path[...], value)
    }

    private static func setting(_ obj: [String: JSONValue], _ path: ArraySlice<String>, _ value: JSONValue?) -> [String: JSONValue] {
        guard let key = path.first else { return obj }
        var out = obj
        if path.count == 1 {
            out[key] = value
        } else {
            let inner = setting(obj[key]?.object ?? [:], path.dropFirst(), value)
            out[key] = inner.isEmpty ? nil : .object(inner)
        }
        return out
    }

    /// Without empty strings and lists the user cleared: "not set" (the schema's default applies).
    static func compact(_ obj: [String: JSONValue]) -> [String: JSONValue] {
        obj.compactMapValues { v in
            switch v {
            case .null: nil
            case .string(let s) where s.isEmpty: nil
            case .array(let a) where a.isEmpty: nil
            case .object(let o): compact(o).isEmpty ? nil : .object(compact(o))
            default: v
            }
        }
    }

    /// The error of a field (the hub's, or a number that does not read as one).
    func fieldError(_ path: [String]) -> String? {
        let key = path.joined(separator: ".")
        return fieldErrors[key] ?? numberProblems[key]
    }
}
