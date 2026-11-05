import Foundation
import Observation

// 设置 › 扩展 (docs/extensions.md「hub 的接口」): the hub's plugins, each one's switch, its configuration as a form drawn
// from its schema (`form.fields`), its secrets, write-only, and the pages it draws (`ui`, ExtensionUIModel). Installing,
// upgrading and removing are done on the hub's machine (`personad plugin`), or from a plugin market the hub has
// (ExtensionMarketModel: only what a market lists).

/// One of GET /api/v1/plugins.
nonisolated struct HubExtension: Codable, Hashable, Sendable, Identifiable {
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
    /// A plugin from a market (a hub with plugin_auto_upgrade): its own auto-upgrade switch, its newer version and what
    /// happened last with an upgrade (docs/extensions.md「自动升级」).
    var autoUpgrade: Bool?
    var upgrade: ExtensionUpgrade?
    var upgradeNote: ExtensionUpgradeNote?
    /// An extension with a program on computers: how that program is on each machine it is switched on for.
    var nodes: [ExtensionNodeState]?

    enum CodingKeys: String, CodingKey {
        case id, name, kind, status, error, version, source, enabled, description, contributes, warnings, upgrade, nodes
        case needsSetup = "needs_setup", autoUpgrade = "auto_upgrade", upgradeNote = "upgrade_note"
    }
}

/// An extension's program on one machine (docs/extensions.md「在电脑上运行的程序」).
nonisolated struct ExtensionNodeState: Codable, Hashable, Sendable {
    var machineID: String
    var version: String?
    /// running | starting | stopped | error | missing | lease_expired | offline | outdated
    var state: String
    var error: String?

    enum CodingKeys: String, CodingKey {
        case version, state, error
        case machineID = "machine_id"
    }

    /// How it is, for the machine's row: what the user can make of it (nil: switched on, nothing heard yet).
    static func status(_ s: ExtensionNodeState?) -> String {
        guard let s else { return "等 hub 把程序发过去" }
        let error = s.error.map(exitText)
        switch s.state {
        case "running": return "程序在运行" + (s.version.map { " · \($0)" } ?? "")
        case "starting": return "程序正在启动"
        case "missing": return "正在把程序发过去"
        case "offline": return "离线：连上后会启动"
        case "lease_expired": return "和 hub 断开太久，程序已停，连上后会再启动"
        case "outdated": return "这台电脑上的 personad 太旧，更新后才能运行"
        case "error": return error.map { "程序出错：\($0)" } ?? "程序出错"
        default: return error.map { "\(s.state)：\($0)" } ?? s.state
        }
    }

    /// Go's words for how a program ended (exec.ExitError), in the apps' words: exit status 3 → 退出码 3.
    static func exitText(_ error: String) -> String {
        error.replacing(#/\bexit status (-?\d+)/#) { "退出码 \($0.1)" }
            .replacing(#/\bsignal: ([a-z ]+)/#) { "被信号结束（\($0.1)）" }
    }
}

/// A market plugin's newer version and where it stands (ExtensionView.upgrade, GET /api/v1/plugins/-/upgrades).
nonisolated struct ExtensionUpgrade: Codable, Hashable, Sendable, Identifiable {
    var id: String
    var name: String?
    var market: String
    var from: String
    var to: String
    /// available | waiting_idle | needs_permission | off | skipped | failed | incompatible
    var state: String
    var addedPermissions: [ExtensionPermission]?
    var detail: String?

    enum CodingKeys: String, CodingKey {
        case id, name, market, from, to, state, detail
        case addedPermissions = "added_permissions"
    }
}

/// What happened last with a plugin's upgrade.
nonisolated struct ExtensionUpgradeNote: Codable, Hashable, Sendable {
    /// upgraded | rolled_back | needs_permission | failed
    var kind: String
    var from: String
    var to: String
    var detail: String?
    var at: Date
}

/// GET /api/v1/plugins/-/upgrades: the global switch and the newer versions.
nonisolated struct ExtensionUpgrades: Decodable, Hashable, Sendable {
    var auto: Bool
    var upgrades: [ExtensionUpgrade]
}

/// One field of a plugin's configuration form (`form.fields`): the hub's reading of the manifest's JSON Schema subset.
nonisolated struct ExtensionField: Codable, Hashable, Sendable, Identifiable {
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
    var items: ExtensionItems?
    var minItems: Int?
    var maxItems: Int?
    var uniqueItems: Bool?
    /// An object's fields (a group of the form).
    var fields: [ExtensionField]?
    /// What to ask before a switch of it is turned on (x-confirm; the plugin cua's 「扩展开着时不自动锁屏」).
    var confirm: String?

    var id: String { name }
    var label: String { title?.isEmpty == false ? title! : name }

    enum CodingKeys: String, CodingKey {
        case name, type, title, description, `default`, required, `enum`, format, pattern, minimum, maximum, items, fields, confirm
        case minLength = "min_length", maxLength = "max_length", minItems = "min_items", maxItems = "max_items"
        case uniqueItems = "unique_items"
    }
}

/// An array field's items.
nonisolated struct ExtensionItems: Codable, Hashable, Sendable {
    var type: String
    var `enum`: [PluginChoice]?
    /// machine: the hub's machines by id, drawn as a switch each (`ExtensionMachineSwitches`).
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
nonisolated struct ExtensionSecret: Codable, Hashable, Sendable, Identifiable {
    var name: String
    var title: String
    var description: String?
    var required: Bool?
    var set: Bool
    var id: String { name }
}

/// A line of what a plugin may do (`permissions`).
nonisolated struct ExtensionPermission: Codable, Hashable, Sendable, Identifiable {
    var key: String
    var title: String
    var detail: String?
    var id: String { key }
}

/// GET /api/v1/plugins/{id}/-/: the plugin with its form, settings, secrets and permissions.
nonisolated struct HubExtensionDetail: Decodable, Hashable, Sendable {
    var plugin: HubExtension
    var publisher: String?
    var homepage: String?
    var origin: String?
    var previousVersion: String?
    /// hub.json's: switched and set up there only.
    var readOnly: Bool
    var form: [ExtensionField]
    var config: [String: JSONValue]
    var secrets: [ExtensionSecret]
    var permissions: [ExtensionPermission]
    /// The pages it draws (placement plugin_page), a row each.
    var ui: [PluginUIPlace]
    /// The field (format machine) that picks the computers its program runs on; their rows say how it is there.
    var nodeMachines: String?

    private struct Form: Decodable { var fields: [ExtensionField]? }
    private enum CodingKeys: String, CodingKey {
        case publisher, homepage, origin, form, config, secrets, permissions, ui
        case previousVersion = "previous_version", readOnly = "read_only", nodeMachines = "node_machines"
    }

    init(from decoder: Decoder) throws {
        plugin = try HubExtension(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        publisher = try c.decodeIfPresent(String.self, forKey: .publisher)
        homepage = try c.decodeIfPresent(String.self, forKey: .homepage)
        origin = try c.decodeIfPresent(String.self, forKey: .origin)
        previousVersion = try c.decodeIfPresent(String.self, forKey: .previousVersion)
        readOnly = try c.decodeIfPresent(Bool.self, forKey: .readOnly) ?? false
        form = try c.decodeIfPresent(Form.self, forKey: .form)?.fields ?? []
        config = try c.decodeIfPresent([String: JSONValue].self, forKey: .config) ?? [:]
        secrets = try c.decodeIfPresent([ExtensionSecret].self, forKey: .secrets) ?? []
        permissions = try c.decodeIfPresent([ExtensionPermission].self, forKey: .permissions) ?? []
        ui = (try c.decodeIfPresent([PluginUIPlace].self, forKey: .ui) ?? []).filter { ($0.placement ?? "plugin_page") == "plugin_page" }
        nodeMachines = try c.decodeIfPresent(String.self, forKey: .nodeMachines)
    }
}

/// GET /api/v1/plugins: the plugins, and whether this hub has plugin markets (`markets`: 设置 › 扩展 › 扩展市场).
nonisolated struct HubExtensionList: Decodable, Sendable {
    var plugins: [HubExtension]
    var canInstall: Bool
    var markets: Bool

    private enum CodingKeys: String, CodingKey {
        case plugins, extensions, markets
        case canInstall = "can_install"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        // `extensions` under /api/v1/extensions, `plugins` under the old path (#1844).
        plugins = try c.decodeIfPresent([HubExtension].self, forKey: .extensions)
            ?? c.decodeIfPresent([HubExtension].self, forKey: .plugins) ?? []
        canInstall = try c.decodeIfPresent(Bool.self, forKey: .canInstall) ?? false
        markets = try c.decodeIfPresent(Bool.self, forKey: .markets) ?? false
    }
}

extension APIClient {
    func plugins() async throws -> [HubExtension] { try await pluginList().plugins }

    func pluginList() async throws -> HubExtensionList { try await get(extensionPath("")) }

    func plugin(_ id: String) async throws -> HubExtensionDetail { try await get(extensionPath("/\(id)/-/")) }

    func setExtension(_ id: String, autoUpgrade: Bool) async throws -> HubExtensionDetail {
        try JSON.decoder.decode(HubExtensionDetail.self, from: try await request("PUT", extensionPath("/\(id)/-/auto-upgrade"), body: ["auto": autoUpgrade]))
    }

    func pluginUpgrades() async throws -> ExtensionUpgrades { try await get(extensionPath("/-/upgrades")) }

    func setExtensionUpgrades(auto: Bool) async throws -> ExtensionUpgrades {
        try JSON.decoder.decode(ExtensionUpgrades.self, from: try await request("PUT", extensionPath("/-/upgrades"), body: ["auto": auto]))
    }

    func setExtension(_ id: String, enabled: Bool) async throws -> HubExtensionDetail {
        try JSON.decoder.decode(HubExtensionDetail.self, from: try await request("PUT", extensionPath("/\(id)/-/enabled"), body: ["enabled": enabled]))
    }

    func setExtension(_ id: String, config: [String: JSONValue]) async throws -> HubExtensionDetail {
        struct Body: Encodable { var config: [String: JSONValue] }
        return try JSON.decoder.decode(HubExtensionDetail.self, from: try await request("PUT", extensionPath("/\(id)/-/config"), body: Body(config: config)))
    }

    func setExtension(_ id: String, secret name: String, value: String) async throws -> HubExtensionDetail {
        try JSON.decoder.decode(HubExtensionDetail.self, from: try await request("PUT", extensionPath("/\(id)/-/secrets/\(name)"), body: ["value": value]))
    }

    func clearExtension(_ id: String, secret name: String) async throws -> HubExtensionDetail {
        try JSON.decoder.decode(HubExtensionDetail.self, from: try await request("DELETE", extensionPath("/\(id)/-/secrets/\(name)")))
    }
}

/// What a plugin is called and says in the settings.
enum ExtensionText {
    /// To the user a plugin is on or off (#1684, as the web and Android since #1670): an idle one starts by itself when
    /// it is used, so running and idle are both 启动; whether it runs right now is the detail page's small print
    /// (`runNote`). hub's `status` values are unchanged.
    static func state(_ p: HubExtension) -> String {
        switch p.status {
        case "running", "stopped": "启动"
        case "starting": "正在启动"
        case "needs_setup": "需要设置"
        case "disabled": "停用"
        case "incompatible": "这台 hub 用不了"
        case "error": "出错了"
        default: "状态未知"
        }
    }

    /// Whether an enabled plugin's program runs right now, under its description; nil for any other state.
    static func runNote(_ p: HubExtension) -> String? {
        switch p.status {
        case "running": "正在运行"
        case "stopped": "暂未运行，用到时自动启动"
        default: nil
        }
    }

    /// Broken: the state's dot is red (`ExtensionStateDot`: yellow waiting on the user, green on, gray otherwise).
    static func isProblem(_ p: HubExtension) -> Bool { p.status == "error" || p.status == "incompatible" }

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
    static func summary(_ p: HubExtension) -> String {
        p.upgrade?.state == "needs_permission" ? state(p) + " · 新版本待确认" : state(p)
    }

    /// Under the form: which switches take effect at once (lists of machines, `ExtensionModel.saveNow`) and that the rest
    /// waits for 保存. Nil when there is only such switches: the fields say so themselves, nothing waits for 保存.
    static func saveFooter(_ fields: [ExtensionField]) -> String? {
        let all = fields.flatMap { $0.type == "object" ? ($0.fields ?? []) : [$0] }
        let now = all.filter { $0.items?.format == "machine" }.map { "「\($0.label)」" }
        if now.isEmpty { return "改完点右上角「保存」；保存后扩展会重新启动。留空的项用扩展的默认值。" }
        if now.count == all.count { return nil }
        return now.joined(separator: "、") + "的开关即时生效；其他项改完点右上角「保存」，保存后扩展会重新启动。留空的项用扩展的默认值。"
    }

    /// The 升级 section's line for a newer version: what happens to it (docs/extensions.md「自动升级」).
    static func upgradeDetail(_ u: ExtensionUpgrade) -> String {
        let why = u.detail.flatMap { $0.isEmpty ? nil : "：" + $0 } ?? ""
        switch u.state {
        case "needs_permission": return morePermissions(u.addedPermissions ?? [])
        case "waiting_idle": return "扩展空闲时自动升级"
        case "available": return "稍后自动升级"
        case "off": return "自动升级关着"
        case "skipped": return "上次升级后起不来，已回到 \(u.from)，不再自动升级到这个版本"
        case "failed": return "没装上" + why
        case "incompatible": return "这台 hub 装不了" + why
        default: return ""
        }
    }

    /// Whether the line asks something of the user (its row opens the upgrade in the market).
    static func upgradeAction(_ u: ExtensionUpgrade) -> String? {
        switch u.state {
        case "needs_permission": return "去确认"
        case "off", "skipped", "failed": return "去升级"
        default: return nil
        }
    }

    /// What a newer version asks for more, as the hub says it in its notice.
    static func morePermissions(_ added: [ExtensionPermission]) -> String {
        guard !added.isEmpty else { return "要的权限有变化" }
        let count = ["", "一", "两", "三", "四", "五"]
        let lines = added.map { p in p.title + (p.detail.flatMap { $0.isEmpty ? nil : "（\($0)）" } ?? "") }
        return (added.count < count.count ? "要多\(count[added.count])项权限：" : "要多 \(added.count) 项权限：") + lines.joined(separator: "、")
    }

    /// The last upgrade, when it is news of its own (asking and failing are the newer version's line).
    static func upgradeNote(_ n: ExtensionUpgradeNote) -> String? {
        switch n.kind {
        case "upgraded": return "已自动升级到 \(n.to)（原来是 \(n.from)）"
        case "rolled_back": return "\(n.to) 起不来，已回到 \(n.from)"
        default: return nil
        }
    }

    /// 设置's row: how many, and how many need the user.
    static func listSummary(_ list: [HubExtension]?) -> String {
        guard let list else { return "" }
        if list.isEmpty { return "没有" }
        let attention = list.filter { $0.status == "needs_setup" || isProblem($0) }.count
        return attention > 0 ? "\(list.count) 个 · \(attention) 个要处理" : "\(list.count) 个"
    }
}

/// 设置 › 扩展: the list.
@Observable final class ExtensionsModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    private(set) var plugins: [HubExtension]?
    /// The hub has plugin markets: the list shows 扩展市场.
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

/// 设置 › 扩展 › one: its switch, its form (a draft saved as a whole), its secrets.
@Observable final class ExtensionModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    let id: String
    private(set) var detail: HubExtensionDetail?
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
        await run { api in try await api.setExtension(self.id, enabled: on) }
    }

    func setAutoUpgrade(_ on: Bool) async {
        await run { api in try await api.setExtension(self.id, autoUpgrade: on) }
    }

    func save() async -> Bool {
        guard canSave else { return false }
        saving = true
        defer { saving = false }
        fieldErrors = [:]
        let ok = await run { api in try await api.setExtension(self.id, config: Self.compact(self.draft)) }
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
        let ok = await run { api in try await api.setExtension(self.id, config: Self.compact(Self.setting(saved, path[...], value))) }
        // run took the hub's answer as the draft: the other edits go back on top of it.
        draft = Self.setting(editing, path[...], ok ? self.value(path) : Self.value(in: saved, path))
    }

    func setSecret(_ name: String, value: String) async -> Bool {
        let value = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else { return false }
        return await run { api in try await api.setExtension(self.id, secret: name, value: value) }
    }

    func clearSecret(_ name: String) async {
        await run { api in try await api.clearExtension(self.id, secret: name) }
    }

    /// Puts the hub's answer in (the draft follows it), or its error: a field it refused goes beside that field.
    @discardableResult private func run(_ call: @escaping (APIClient) async throws -> HubExtensionDetail) async -> Bool {
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
