import Foundation
import Observation

// Settings › Extensions (docs/extensions.md「hub 的接口」): the hub's plugins, each one's switch, its configuration as a form drawn
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
    /// Its sign-in (a hub with extension_auth, #4507): the hub's last word on it.
    var auth: ExtensionAuth?
    /// The command-line programs this version runs with.
    var tools: [ExtensionToolUse]?

    enum CodingKeys: String, CodingKey {
        case id, name, kind, status, error, version, source, enabled, description, contributes, warnings, upgrade, nodes, auth, tools
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
        guard let s else { return String(localized: "等 hub 把程序发过去") }
        let error = s.error.map(exitText)
        switch s.state {
        case "running": return s.version.map { String(localized: "程序在运行 · \($0)") } ?? String(localized: "程序在运行")
        case "starting": return String(localized: "程序正在启动")
        case "missing": return String(localized: "正在把程序发过去")
        case "offline": return String(localized: "离线：连上后会启动")
        case "lease_expired": return String(localized: "和 hub 断开太久，程序已停，连上后会再启动")
        case "outdated": return String(localized: "这台电脑上的 personad 太旧，更新后才能运行")
        case "error": return error.map { String(localized: "程序出错：\($0)") } ?? String(localized: "程序出错")
        default: return error.map { String(localized: "\(s.state)：\($0)", comment: "A program state the app does not know, and its error") } ?? s.state
        }
    }

    /// Go's words for how a program ended (exec.ExitError), in the apps' words: exit status 3 → Exit code 3.
    static func exitText(_ error: String) -> String {
        error.replacing(#/\bexit status (-?\d+)/#) { m in let code = String(m.1); return String(localized: "退出码 \(code)") }
            .replacing(#/\bsignal: ([a-z ]+)/#) { m in let signal = String(m.1); return String(localized: "被信号结束（\(signal)）") }
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
    var `enum`: [ExtensionFormChoice]?
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
    var `enum`: [ExtensionFormChoice]?
    /// machine: the hub's machines by id, drawn as a switch each (`ExtensionMachineSwitches`).
    var format: String?
    /// For machine: only machines of these systems (darwin, linux, windows); nil, any.
    var os: [String]?
}

/// One value of a field's choices, with its label.
nonisolated struct ExtensionFormChoice: Codable, Hashable, Sendable, Identifiable {
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
    var ui: [ExtensionUIPlace]
    /// The field (format machine) that picks the computers its program runs on; their rows say how it is there.
    var nodeMachines: String?
    var contributions: [ExtensionContributionSwitch]?
    var depends: [ExtensionDependencyStatus]?
    var extensionPack: [ExtensionPackMember]?

    private struct Form: Decodable { var fields: [ExtensionField]? }
    private enum CodingKeys: String, CodingKey {
        case publisher, homepage, origin, form, config, secrets, permissions, ui, contributions, depends
        case extensionPack = "extension_pack"
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
        ui = (try c.decodeIfPresent([ExtensionUIPlace].self, forKey: .ui) ?? []).filter(\.isExtensionPage)
        nodeMachines = try c.decodeIfPresent(String.self, forKey: .nodeMachines)
        contributions = try c.decodeIfPresent([ExtensionContributionSwitch].self, forKey: .contributions)
        depends = try c.decodeIfPresent([ExtensionDependencyStatus].self, forKey: .depends)
        extensionPack = try c.decodeIfPresent([ExtensionPackMember].self, forKey: .extensionPack)
    }
}

/// GET /api/v1/plugins: the plugins, and whether this hub has plugin markets (`markets`: Settings › Extensions › Extension Market).
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

    func setExtension(_ id: String, enabled: Bool, cascade: Bool = false) async throws -> HubExtensionDetail {
        var body = ["enabled": enabled]
        if cascade { body["cascade"] = true }
        return try JSON.decoder.decode(HubExtensionDetail.self, from: try await request("PUT", extensionPath("/\(id)/-/enabled"), body: body))
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
    /// Names in a row, with the language's list separator (「、」 in Chinese): a, b.
    static func list(_ items: [String]) -> String {
        items.joined(separator: String(localized: "、", comment: "Separator between list items"))
    }

    /// To the user a plugin is on or off (#1684, as the web and Android since #1670): an idle one starts by itself when
    /// it is used, so running and idle are both On; whether it runs right now is the detail page's small print
    /// (`runNote`). hub's `status` values are unchanged.
    static func state(_ p: HubExtension) -> String {
        switch p.status {
        case "running", "stopped": String(localized: "启动", comment: "Extension state: on")
        case "starting": String(localized: "正在启动")
        case "needs_setup": String(localized: "需要设置")
        case "disabled": String(localized: "停用", comment: "Extension state: off")
        case "incompatible": String(localized: "这台 hub 用不了")
        case "error": String(localized: "出错了")
        default: String(localized: "状态未知")
        }
    }

    /// Whether an enabled plugin's program runs right now, under its description; nil for any other state.
    static func runNote(_ p: HubExtension) -> String? {
        switch p.status {
        case "running": String(localized: "正在运行")
        case "stopped": String(localized: "暂未运行，用到时自动启动")
        default: nil
        }
    }

    /// Broken: the state's dot is red (`ExtensionStateDot`: yellow waiting on the user, green on, gray otherwise).
    static func isProblem(_ p: HubExtension) -> Bool { p.status == "error" || p.status == "incompatible" }

    static func source(_ s: String?) -> String? {
        switch s {
        case "builtin": String(localized: "内置")
        case "hub.json": "hub.json"
        case "installed": String(localized: "已安装")
        default: nil
        }
    }

    static func point(_ name: String) -> String {
        switch name {
        case "connectors": String(localized: "连接器")
        case "voice": String(localized: "语音对话")
        default: name
        }
    }

    /// The row's value: its state (the version is on its page).
    static func summary(_ p: HubExtension, auth: ExtensionAuth? = nil) -> String {
        let base = p.upgrade?.state == "needs_permission" ? String(localized: "\(state(p)) · 新版本待确认") : state(p)
        // Not signed in (#4507): said after the state, as the hub's warning says it to older apps.
        guard p.enabled ?? true, let suffix = ExtensionAuthText.listSuffix(auth ?? p.auth) else { return base }
        return base + " · " + suffix // i18n-ignore
    }

    /// A permission's title; the hub's lines for command-line programs (#4507) in this app's words.
    static func permissionTitle(_ p: ExtensionPermission) -> String {
        guard p.key == "tools" else { return p.title }
        let more = p.title.contains("更多") // i18n-ignore: the hub's title for an upgrade's added programs
        return more ? String(localized: "安装更多命令行程序") : String(localized: "安装命令行程序")
    }

    /// Under the form: which switches take effect at once (lists of machines, `ExtensionModel.saveNow`) and that the rest
    /// waits for Save. Nil when there is only such switches: the fields say so themselves, nothing waits for Save.
    static func saveFooter(_ fields: [ExtensionField]) -> String? {
        let all = fields.flatMap { $0.type == "object" ? ($0.fields ?? []) : [$0] }
        let now = all.filter { $0.items?.format == "machine" }.map { String(localized: "「\($0.label)」", comment: "A field's name in quotes") }
        if now.isEmpty { return String(localized: "改完点右上角「保存」；保存后扩展会重新启动。留空的项用扩展的默认值。") }
        if now.count == all.count { return nil }
        let names = list(now)
        return String(localized: "\(names)的开关即时生效；其他项改完点右上角「保存」，保存后扩展会重新启动。留空的项用扩展的默认值。")
    }

    /// The Update section's line for a newer version: what happens to it (docs/extensions.md「自动升级」).
    static func upgradeDetail(_ u: ExtensionUpgrade) -> String {
        let why = u.detail.flatMap { $0.isEmpty ? nil : $0 }
        switch u.state {
        case "needs_permission": return morePermissions(u.addedPermissions ?? [])
        case "waiting_idle": return String(localized: "扩展空闲时自动升级")
        case "available": return String(localized: "稍后自动升级")
        case "off": return String(localized: "自动升级关着")
        case "skipped": return String(localized: "上次升级后起不来，已回到 \(u.from)，不再自动升级到这个版本")
        case "failed": return why.map { String(localized: "没装上：\($0)") } ?? String(localized: "没装上")
        case "incompatible": return why.map { String(localized: "这台 hub 装不了：\($0)") } ?? String(localized: "这台 hub 装不了")
        default: return ""
        }
    }

    /// Whether the line asks something of the user (its row opens the upgrade in the market).
    static func upgradeAction(_ u: ExtensionUpgrade) -> String? {
        switch u.state {
        case "needs_permission": return String(localized: "去确认")
        case "off", "skipped", "failed": return String(localized: "去升级")
        default: return nil
        }
    }

    /// What a newer version asks for more, as the hub says it in its notice.
    static func morePermissions(_ added: [ExtensionPermission]) -> String {
        guard !added.isEmpty else { return String(localized: "要的权限有变化") }
        let lines = added.map { p in
            p.detail.flatMap { $0.isEmpty ? nil : $0 }.map { String(localized: "\(p.title)（\($0)）", comment: "A permission and its detail") } ?? p.title
        }
        let all = list(lines)
        // One sentence per small count: the hub's notice says them with Chinese numerals (「要多一项权限」).
        return switch added.count {
        case 1: String(localized: "要多一项权限：\(all)")
        case 2: String(localized: "要多两项权限：\(all)")
        case 3: String(localized: "要多三项权限：\(all)")
        case 4: String(localized: "要多四项权限：\(all)")
        case 5: String(localized: "要多五项权限：\(all)")
        default: String(localized: "要多 \(added.count) 项权限：\(all)")
        }
    }

    /// The last upgrade, when it is news of its own (asking and failing are the newer version's line).
    static func upgradeNote(_ n: ExtensionUpgradeNote) -> String? {
        switch n.kind {
        case "upgraded": return String(localized: "已自动升级到 \(n.to)（原来是 \(n.from)）")
        case "rolled_back": return String(localized: "\(n.to) 起不来，已回到 \(n.from)")
        default: return nil
        }
    }

    /// Settings' row: how many, and how many need the user.
    static func listSummary(_ list: [HubExtension]?) -> String {
        guard let list else { return "" }
        if list.isEmpty { return String(localized: "没有", comment: "No extensions") }
        let attention = list.filter { $0.status == "needs_setup" || isProblem($0) }.count
        return attention > 0 ? String(localized: "\(list.count) 个 · \(attention) 个要处理") : String(localized: "\(list.count) 个")
    }
}

/// Settings › Extensions: the list.
@Observable final class ExtensionsModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    private(set) var plugins: [HubExtension]?
    /// The hub has plugin markets: the list shows Extension Market.
    private(set) var markets = false
    private(set) var loading = false
    private(set) var error: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    /// The hub has no extensions to list (404: an older hub, or Demo Mode): the Mac says so instead of the error.
    private(set) var missing = false

    func load() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
            let list = try await api.pluginList()
            plugins = list.plugins
            ExtensionAuthStore.shared.received(list.plugins.filter { $0.auth != nil })
            markets = list.markets
            error = nil
            missing = false
        } catch {
            self.error = error.localizedDescription
            missing = (error as? APIError)?.status == 404
        }
    }
}

/// Settings › Extensions › one: its switch, its form (a draft saved as a whole), its secrets.
@Observable final class ExtensionModel {
    struct Deps {
        var api: () -> APIClient?
        var drafts: ExtensionDrafts = ExtensionDrafts()
        static var live: Deps { .init(api: { AppModel.shared.api }, drafts: .shared) }
    }

    let id: String
    private(set) var detail: HubExtensionDetail?
    private(set) var loading = false
    private(set) var saving = false
    private(set) var error: String?
    private(set) var changingContribution: String?
    private(set) var changingEnabled = false
    var disableConfirmation: ExtensionDisableConfirmation?
    /// The hub's word on a field it refused, by its dotted path ("proxy.host").
    private(set) var fieldErrors: [String: String] = [:]
    /// The configuration being edited; saved as a whole (PUT replaces it).
    var draft: [String: JSONValue] = [:] { didSet { keepDraft() } }
    /// What number fields hold as typed, by path, until they read as a number.
    var numberText: [String: String] = [:] { didSet { keepDraft() } }
    @ObservationIgnored private let deps: Deps
    @ObservationIgnored private var draftKey: ExtensionDrafts.Key?

    init(id: String, deps: Deps = .live) {
        self.id = id
        self.deps = deps
    }

    var dirty: Bool { (detail.map { draft != $0.config } ?? false) || !numberProblems.isEmpty }

    /// Number fields typed into that do not read as one, by path.
    var numberProblems: [String: String] {
        numberText.compactMapValues { text in
            text.trimmingCharacters(in: .whitespaces).isEmpty || Double(text.trimmingCharacters(in: .whitespaces)) != nil ? nil : String(localized: "要是一个数")
        }
    }

    var canSave: Bool { dirty && numberProblems.isEmpty && !saving && !changingEnabled && changingContribution == nil }

    func load() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        if let api = deps.api() {
            let source = api.group.isEmpty ? "url:" + api.config.url.absoluteString : "group:" + api.group
            draftKey = .init(source: source, id: id)
        }
        await run(preservingDraft: true) { api in try await api.plugin(self.id) }
    }

    func setEnabled(_ on: Bool, cascade: Bool = false) async {
        guard !changingEnabled, changingContribution == nil, !saving, detail?.readOnly == false else { return }
        changingEnabled = true
        defer { changingEnabled = false }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
            detail = try await api.setExtension(id, enabled: on, cascade: cascade)
            error = nil
            disableConfirmation = nil
        } catch let e as APIError where !on && !cascade && e.code == "has_dependents" {
            disableConfirmation = ExtensionDisableConfirmation(error: e)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    func confirmDisable() async {
        disableConfirmation = nil
        await setEnabled(false, cascade: true)
    }

    func setContribution(_ contribution: ExtensionContributionSwitch, enabled: Bool) async {
        guard changingContribution == nil, !changingEnabled, !saving, contribution.canDisable, detail?.readOnly == false else { return }
        changingContribution = contribution.id
        defer { changingContribution = nil }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
            detail = try await api.setExtensionContribution(id, point: contribution.point, id: contribution.contributionID, enabled: enabled)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    func setAutoUpgrade(_ on: Bool) async {
        await run(preservingDraft: true) { api in try await api.setExtension(self.id, autoUpgrade: on) }
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
    /// edited for Save.
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
        return await run(preservingDraft: true) { api in try await api.setExtension(self.id, secret: name, value: value) }
    }

    func clearSecret(_ name: String) async {
        await run(preservingDraft: true) { api in try await api.clearExtension(self.id, secret: name) }
    }

    private func keepDraft() {
        guard let key = draftKey, detail != nil else { return }
        deps.drafts.values[key] = dirty ? .init(config: draft, numbers: numberText) : nil
    }

    /// Takes the new configuration on save; reloading and non-configuration actions keep the unsaved draft.
    @discardableResult private func run(preservingDraft: Bool = false, _ call: @escaping (APIClient) async throws -> HubExtensionDetail) async -> Bool {
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
            let d = try await call(api)
            let editing = preservingDraft ? draftKey.flatMap { deps.drafts.values[$0] } : nil
            detail = d
            draft = editing?.config ?? d.config
            numberText = editing?.numbers ?? [:]
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
