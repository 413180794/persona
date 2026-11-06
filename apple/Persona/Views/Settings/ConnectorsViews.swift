import PersonaKit
import SwiftUI

// Settings › Connectors (2026-11-01): where Persona' data comes from and acts — this device's data (phone_* tools), the mail
// accounts, Lark (the hub's Lark gateway), and the Lark Bot the user talks to Persona through (FeishuBotView). Mail used to be managed from the inbox's ⋯ menu, this device's data was Phone Data.

/// What a connector is called and says in the settings list.
enum ConnectorText {
    /// This device as a connector: This iPhone / iPad / Mac.
    static var device: String {
        #if os(macOS)
        String(localized: "这台 Mac")
        #else
        UIDevice.current.userInterfaceIdiom == .pad ? String(localized: "这台 iPad") : String(localized: "这台 iPhone")
        #endif
    }

    static func deviceSummary() -> String {
        let on = PhoneDataSource.onThisDevice.filter(\.isEnabled).count
        return on == 0 ? String(localized: "未打开") : String(localized: "\(on) 项已打开")
    }

    static func mailSummary(_ accounts: [MailAccount]?) -> String {
        guard let accounts else { return "" }
        if accounts.isEmpty { return String(localized: "未添加") }
        if accounts.contains(where: { $0.status == "error" }) { return String(localized: "\(accounts.count) 个 · 有错误") }
        return String(localized: "\(accounts.count) 个账户")
    }

    static func larkSummary(_ st: LarkStatus?) -> String {
        guard let st else { return "" }
        if !st.configured { return String(localized: "未配置") }
        return st.connected ? (st.name ?? String(localized: "已连接")) : String(localized: "连不上")
    }

    /// A hub connector's state in a word or two (its row, its page). Only "error" is Something Went Wrong: a state this app does not
    /// know yet (a newer hub's) is not one.
    static func state(_ c: HubConnector) -> String {
        switch c.status {
        case "ok": String(localized: "已连接")
        case "not_configured": String(localized: "未配置")
        case "not_signed_in": String(localized: "未登录")
        case "no_permission": String(localized: "没有权限")
        case "rate_limited": String(localized: "请求太多，暂停中")
        case "disabled": String(localized: "已关闭")
        case "error": String(localized: "出错了")
        default: String(localized: "状态未知")
        }
    }

    /// The dot beside the state: red only for what is broken; switched off, not set up yet, unknown or still loading are gray.
    static func tone(_ c: HubConnector?) -> Color {
        switch c?.status {
        case "ok": Theme.positive
        case "rate_limited": Theme.caution
        case "error", "no_permission": Theme.danger
        default: .secondary
        }
    }

    static func summary(_ c: HubConnector) -> String {
        c.status == "ok" ? (c.account ?? state(c)) : state(c)
    }

    /// The symbol of a connector without a mark of its own.
    static func symbol(_ id: String) -> String {
        switch id {
        case "imessage": Theme.Icon.messagesApp
        case "wechat": Theme.Icon.conversation // a chat with a bot, not a feed
        default: Theme.Icon.connectors
        }
    }

    /// The connector's own mark (`Theme.Brand`), nil for one the app has none for.
    nonisolated static func brand(_ id: String) -> String? { profile(id).brand }

    /// What this app knows about a connector by its id; one not listed (any plugin's) gets the generic icon and the
    /// generic wording, built from its own name. The web keeps the same table (web/src/lib/connectors.ts `PROFILES`).
    nonisolated static func profile(_ id: String) -> ConnectorProfile {
        switch id {
        case "github": ConnectorProfile(brand: Theme.Brand.github)
        case "telegram": ConnectorProfile(brand: Theme.Brand.telegram, app: "Telegram", reads: String(localized: "它的私聊和群"),
                                          signInFooter: String(localized: "API 凭据和登录状态加密存在 hub 上，只显示 api_id；退出登录时一起删掉。"),
                                          logoutEndsRemoteSession: true)
        default: ConnectorProfile()
        }
    }

    /// The sign-in step's footer: where the credentials live.
    nonisolated static func signInFooter(_ c: HubConnector) -> String {
        profile(c.id).signInFooter ?? String(localized: "登录状态加密存在 hub 上，退出登录时删掉。")
    }

    /// The sign-out confirmation. Only a connector known to end the session at the service says so; signing out of any
    /// other (GitHub's device token, a plugin's key) only has the hub forget its copy, and the authorization stays there.
    nonisolated static func logoutMessage(_ c: HubConnector) -> String {
        profile(c.id).logoutEndsRemoteSession
            ? String(localized: "hub 上存的 API 凭据和登录状态都会删掉，\(c.name) 那边这个设备会话也会结束。再用要重新登录。")
            : String(localized: "hub 上存的凭据和登录状态会删掉，再用要重新登录。对方服务里的授权不会自动撤销，需要的话到那边撤销。")
    }

    /// A conversation's message opened where it came from: Open in Telegram; a connector without an app of its own
    /// says its name.
    nonisolated static func openInTitle(_ id: String, name: String) -> String {
        let place = profile(id).app ?? name
        return String(localized: "在 \(place) 里打开")
    }
}

/// See `ConnectorText.profile`. Every part is optional.
nonisolated struct ConnectorProfile: Equatable, Sendable {
    /// Its mark (`Theme.Brand`).
    var brand: String?
    /// The app its items are answered in, for Reply in Telegram and Open in Telegram.
    var app: String?
    /// What switching it on brings in, in the switch's footer; the default is "its content".
    var reads: String?
    var signInFooter: String?
    /// Signing out also ends the session at the service (Telegram's log-out call).
    var logoutEndsRemoteSession = false
}

/// GET /api/v1/lark/status.
nonisolated struct LarkStatus: Codable, Hashable, Sendable {
    var configured: Bool
    var connected: Bool
    var name: String?
    var error: String?
    var lastEventAt: Int64?

    enum CodingKeys: String, CodingKey {
        case configured, connected, name, error
        case lastEventAt = "last_event_at"
    }
}

/// One of GET /api/v1/connectors: a source the hub reads (GitHub…) and how it is doing (docs/protocol.md「连接器 API」).
/// Feishu and mail are in it too when asked for (`all`): `ConnectorRow` draws them as the rows they always had.
nonisolated struct HubConnector: Codable, Hashable, Sendable, Identifiable {
    var id: String
    var name: String
    var status: String
    var account: String?
    var via: String?
    var error: String?
    /// How to fix it, a step a sentence, when the user can.
    var guide: [String]?
    var lastSync: Date?
    var lastEvent: Date?
    var capabilities: [String]?
    var hosts: [String]?
    /// Its own settings (GitHub's: GitHubInboxOptions), as the hub gives them.
    var options: JSONValue?
    /// Switched on in Settings (nil: a hub from before the switch, always on).
    var enabled: Bool?
    /// Save to Knowledge and Show in Inbox (nil: it has no archive, no inbox, or the hub is from before these switches).
    var archive: Bool?
    var inbox: Bool?

    enum CodingKeys: String, CodingKey {
        case id, name, status, account, via, error, guide, capabilities, hosts, options, enabled, archive, inbox
        case lastSync = "last_sync"
        case lastEvent = "last_event"
    }

    /// The user signs in to it here, a step at a time (Telegram), rather than on the hub's side.
    var signsInHere: Bool { capabilities?.contains("login") == true }

    /// It brings things into the knowledge base or the inbox (Telegram), so it has data to delete and its switch syncs;
    /// a chat bot (WeChat) only talks. Not described yet counts as bringing them, as before there were bots.
    var storesData: Bool {
        guard let caps = capabilities, !caps.isEmpty else { return true }
        return caps.contains("archive") || caps.contains("inbox")
    }

    /// A program the hub runs (Telegram) that has not said what it has: switched off since the hub started, or still
    /// starting. Its capabilities come from the program, so an empty list (nil is a hub from before them) does not
    /// mean it signs in on the hub's side.
    var undescribed: Bool { capabilities?.isEmpty == true }
}

/// Where a hub connector's things go besides the assistant's tools, each switched on its page.
nonisolated enum ConnectorFeed: String, Sendable {
    case archive, inbox
}

/// What deleting a connector's data removed (DELETE /api/v1/connectors/{id}/data, …/archive).
nonisolated struct ConnectorDataDeleted: Codable, Sendable {
    var chats: Int?
    var people: Int?
    var inbox: Int?
}

extension APIClient {
    func larkStatus() async throws -> LarkStatus { try await get("/api/v1/lark/status") }

    func connectorLogin(_ id: String, step: String, values: [String: String]) async throws -> ConnectorPrompt {
        struct Body: Encodable { var step: String; var values: [String: String] }
        return try await post("/api/v1/connectors/\(id)/login", body: Body(step: step, values: values), timeout: 90)
    }

    func connectorLogout(_ id: String) async throws { _ = try await post("/api/v1/connectors/\(id)/logout", timeout: 60) }

    func setConnector(_ id: String, enabled: Bool) async throws -> HubConnector {
        try JSON.decoder.decode(HubConnector.self, from: try await request("PUT", "/api/v1/connectors/\(id)/settings", body: ["enabled": enabled]))
    }

    func deleteConnectorData(_ id: String) async throws -> ConnectorDataDeleted {
        try JSON.decoder.decode(ConnectorDataDeleted.self, from: try await request("DELETE", "/api/v1/connectors/\(id)/data"))
    }

    /// Turns Save to Knowledge (`ConnectorFeed.archive`) or Show in Inbox on or off.
    func setConnector(_ id: String, feed: ConnectorFeed, on: Bool) async throws -> HubConnector {
        try JSON.decoder.decode(HubConnector.self, from: try await request("PUT", "/api/v1/connectors/\(id)/settings", body: [feed.rawValue: on]))
    }

    /// Delete Saved Knowledge: the knowledge base's chats and people of it; Save to Knowledge is turned off with it.
    func deleteConnectorArchive(_ id: String) async throws -> ConnectorDataDeleted {
        try JSON.decoder.decode(ConnectorDataDeleted.self, from: try await request("DELETE", "/api/v1/connectors/\(id)/archive"))
    }

    /// The hub's connectors; `all` takes Feishu and mail too (a hub from before M6 leaves them out anyway).
    func connectors(all: Bool = false) async throws -> [HubConnector] {
        struct List: Decodable { var connectors: [HubConnector] }
        let list: List = try await get("/api/v1/connectors", query: all ? ["all": "1"] : [:])
        return list.connectors
    }
}

/// A row of Settings › Connectors after this device's, from the hub's list (GET /api/v1/connectors?all=1): mail and Feishu
/// first and drawn as they always were (their own pages, their own status), then the rest in the hub's order.
nonisolated enum ConnectorRow: Hashable, Identifiable {
    case mail, lark
    case hub(HubConnector)

    var id: String {
        switch self {
        case .mail: "mail"
        case .lark: "lark"
        case .hub(let c): c.id
        }
    }

    /// A hub that lists Feishu says whether it has mail; one that does not (from before M6, or no list yet) gets both
    /// rows, as before. Feishu's row (its page reads `/api/v1/lark/…`) is there only while the hub has Feishu chats
    /// (`LarkFeatures`, the Lark extension).
    static func rows(_ list: [HubConnector], lark: Bool = true) -> [ConnectorRow] {
        let listed = list.contains { $0.id == "lark" }
        let legacy: [ConnectorRow] = listed ? (list.contains { $0.id == "mail" } ? [.mail, .lark] : [.lark]) : [.mail, .lark]
        return legacy.filter { lark || $0 != .lark } + list.filter { $0.id != "mail" && $0.id != "lark" }.map(ConnectorRow.hub)
    }
}

/// An extension's page (Settings › Extensions › …) for the connectors it contributes (#3228, as the web's
/// `ExtensionConnectorInbox`): each one's Show in Inbox switch, the hub's own (PUT /api/v1/connectors/{id}/settings, the same
/// switch as on the connector's page, not a copy), and the way to that page for the rest.
@Observable final class ExtensionConnectorsModel {
    struct Deps {
        var api: () -> APIClient?
        var report: (Error) -> Void

        static var live: Deps {
            let model = AppModel.shared
            return Deps(api: { model.api }, report: { model.show($0) })
        }
    }

    /// The ones to draw, in the extension's order.
    private(set) var connectors: [HubConnector] = []
    /// The connector whose switch is being changed.
    private(set) var changing: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    /// The contributed connectors the hub lists with an inbox switch, in `ids`' order. A hub from before the switch
    /// (no `inbox`) and a connector it does not list draw nothing.
    nonisolated static func shown(_ all: [HubConnector], ids: [String]) -> [HubConnector] {
        ids.compactMap { id in all.first { $0.id == id && $0.inbox != nil } }
    }

    /// Unreadable (a hub without connectors answers 404), the sections stay away: the connector's page says what is
    /// wrong. Not gated on the hub's features: opened at launch, the page is there before they are.
    func load(ids: [String]) async {
        guard !ids.isEmpty, let api = deps.api() else { connectors = []; return }
        guard let all = try? await api.connectors() else { connectors = []; return }
        connectors = Self.shown(all, ids: ids)
    }

    func setInbox(_ id: String, on: Bool) async {
        guard changing == nil, let api = deps.api() else { return }
        changing = id
        defer { changing = nil }
        do {
            let updated = try await api.setConnector(id, feed: .inbox, on: on)
            if let i = connectors.firstIndex(where: { $0.id == id }) {
                // A hub that answers without the switch still switched it: kept as asked.
                connectors[i] = updated.inbox == nil ? { var c = updated; c.inbox = on; return c }() : updated
            }
        } catch {
            deps.report(error)
        }
    }
}

/// The Connectors section of Settings: one row per connector with what it has now.
struct ConnectorsSection: View {
    @Environment(AppModel.self) private var model
    @State private var accounts: [MailAccount]?
    @State private var lark: LarkStatus?
    @State private var hubConnectors: [HubConnector] = []
    @State private var bot: FeishuBotStatus?

    var body: some View {
        Section {
            Group {
                #if os(iOS) || os(visionOS)
                // A Mac is a desktop, not the phone: the hub never calls its tools (AppModel.registration).
                NavigationLink(value: SettingsPage.phoneData) {
                    ConnectorRowLabel(title: ConnectorText.device, symbol: Theme.Icon.thisDevice, value: ConnectorText.deviceSummary())
                }
                .settingsListRow(SettingsPage.phoneData)
                #endif
                ForEach(ConnectorRow.rows(hubConnectors, lark: model.larkFeatures.chats)) { row in
                    switch row {
                    case .mail:
                        NavigationLink(value: SettingsPage.mail) {
                            ConnectorRowLabel(title: String(localized: "邮箱"), symbol: Theme.Icon.mailAccounts, value: ConnectorText.mailSummary(accounts))
                        }
                        .settingsListRow(SettingsPage.mail)
                    case .lark:
                        NavigationLink(value: SettingsPage.feishu) {
                            ConnectorRowLabel(title: String(localized: "飞书"), symbol: Theme.Icon.mailAccounts, brand: Theme.Brand.lark, value: ConnectorText.larkSummary(lark))
                        }
                        .settingsListRow(SettingsPage.feishu)
                    case .hub(let c):
                        NavigationLink(value: SettingsPage.connector(c.id)) {
                            ConnectorRowLabel(title: c.name, symbol: ConnectorText.symbol(c.id), brand: ConnectorText.brand(c.id), value: ConnectorText.summary(c))
                        }
                        .settingsListRow(SettingsPage.connector(c.id))
                    }
                }
                NavigationLink(value: SettingsPage.feishuBot) {
                    ConnectorRowLabel(title: String(localized: "飞书机器人"), symbol: Theme.Icon.chatBot, value: FeishuBotModel.summary(bot))
                }
                .settingsListRow(SettingsPage.feishuBot)
            }
            .bareListRow()
        } header: {
            SettingsHeading(String(localized: "连接器"))
        } footer: {
            Text("Persona 能读、能替你操作的数据来源：助手、AI 分身和 Agent 通过它们查日程、读消息、发邮件；替你出面的操作都会先问你。")
        }
        .task(id: model.settingsPath.count) { await load() } // again when a page closes: it may have changed them
    }

    private func load() async {
        guard let api = model.api else { return }
        async let a = try? api.mailAccounts()
        async let l = model.larkFeatures.chats ? try? api.larkStatus() : nil
        // An older hub has no connectors (feature "connectors"): its list stays as it was.
        async let c = model.hubFeatures.contains("connectors") ? try? api.connectors(all: true) : nil
        async let b = try? api.feishuBot()
        let (accs, st, list, bs) = await (a, l, c, b)
        if let accs { accounts = accs }
        if let st { lark = st }
        if let list { hubConnectors = list }
        if let bs { bot = bs }
    }
}

/// One row of the Connectors section: the connector's mark (its brand's, or a symbol) and name, what it has now on the right.
private struct ConnectorRowLabel: View {
    var title: String
    var symbol: String
    var brand: String? = nil
    var value: String

    var body: some View {
        if AppStyle.current.usesBeautifulUI {
            SettingsRow(title: title, symbol: symbol, tint: Theme.accent, value: value, brand: brand)
        } else {
            LabeledContent {
                Text(value).lineLimit(1)
            } label: {
                Label {
                    Text(title)
                } icon: {
                    if let brand {
                        BrandMark(brand, side: Theme.Size.icon)
                    } else {
                        Image(systemName: symbol)
                    }
                }
            }
        }
    }
}

/// Settings › Connectors › Lark: whether the hub reaches the Lark gateway, who is signed in, and what Persona does with it. The account
/// itself is signed in on the hub's machine (docs/feishu.md), not here.
struct FeishuConnectorView: View {
    @Environment(AppModel.self) private var model
    @Bindable private var larkHome = LarkHomePreference.shared
    @State private var status: LarkStatus?
    @State private var accounts: APIClient.LarkAccounts?
    @State private var loading = true
    @State private var failure: String?

    var body: some View {
        SettingsForm {
            Section {
                HStack(spacing: Theme.Space.m) {
                    BrandMark(Theme.Brand.lark, side: Theme.Size.wellM)
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(status?.name ?? String(localized: "飞书")).font(Theme.Font.heading).lineLimit(1)
                        HStack(spacing: Theme.Space.sm) {
                            Circle().fill(tint).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                            Text(stateText).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                        }
                    }
                    Spacer(minLength: Theme.Space.s)
                    if loading { ProgressView() }
                }
                .padding(.vertical, Theme.Space.xs)
                if let at = status?.lastEventAt, at > 0 {
                    LabeledContent("最近一条实时消息") {
                        TimelineView(.periodic(from: .now, by: 30)) { ctx in
                            Text(Fmt.relative(Date(timeIntervalSince1970: TimeInterval(at)), now: ctx.date)).lineLimit(1)
                        }
                    }
                }
                if let e = status?.error ?? failure, !e.isEmpty {
                    Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled)
                }
            } footer: {
                Text(status?.configured == false
                     ? "hub 还没接飞书：在 hub 所在的机器上运行飞书网关，并在 hub.json 的 lark 里填上它（docs/feishu.md）。"
                     : "飞书账号在 hub 所在机器的飞书网关里登录，会话与收件箱共用这个账号，无需再次登录。")
            }

            if let accounts, accounts.accounts.count > 1 {
                Section {
                    ForEach(accounts.accounts) { account in
                        accountRow(account, isDefault: account.id == accounts.default)
                    }
                } header: {
                    Text("账号")
                } footer: {
                    Text("已登录的账号都会收进收件箱，「消息」页可以切换账号。/agent、助手和知识库使用默认账号；在飞书网关里添加或退出账号。")
                }
            }

            Section {
                Toggle("首页显示消息", isOn: $larkHome.enabled)
                .accessibilityIdentifier("feishu-home-enabled")
            } footer: {
                #if os(macOS)
                Text("在侧边栏增加「消息」，查看全部私聊和群聊。与收件箱共用账号和聊天记录。")
                #else
                Text("在首页增加「消息」入口，查看全部私聊和群聊。与收件箱共用账号和聊天记录。")
                #endif
            }

            Section {
                NavigationLink("风灵月影") { LarkMessageDisplaySettingsView() }
            }

            Section("Persona 用飞书做什么") {
                use(Theme.Icon.inbox, String(localized: "收件箱"), String(localized: "私聊、@我、群消息和云文档里提到你的地方，AI 先分拣、起草回复"))
                use(Theme.Icon.knowledge, String(localized: "知识库"), String(localized: "会话、人、群、项目归档下来，给助手和 AI 分身做上下文"))
                #if os(macOS)
                use(Theme.Icon.ai, String(localized: "助手"), String(localized: "Persona 能搜消息、读文档；发消息等写操作先在 App 里问你"))
                #else
                use(Theme.Icon.ai, String(localized: "助手"), String(localized: "Persona 能搜消息、读文档；发消息等写操作先在手机上问你"))
                #endif
                use(Theme.Icon.agents, "/agent", String(localized: "在飞书任何地方以 /agent 开头说一句，就起一个 Agent 处理并替你回复"))
                ModelConfigurationLink(title: String(localized: "/agent 模型"), purposeID: "lark.agent")
            }
        }
        .themedPage()
        .detailTitle(String(localized: "飞书"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private var tint: Color {
        guard let st = status else { return .secondary }
        return st.connected ? Theme.positive : st.configured ? Theme.danger : .secondary
    }

    private var stateText: String {
        guard let st = status else { return loading ? String(localized: "正在检查…") : String(localized: "读不到状态") }
        if !st.configured { return String(localized: "未配置") }
        return st.connected ? String(localized: "已连接") : String(localized: "连不上飞书网关")
    }

    private func accountRow(_ account: LarkAccount, isDefault: Bool) -> some View {
        HStack(spacing: Theme.Space.m) {
            LarkAvatarImage(name: account.title, url: account.avatar, size: Theme.Size.wellM)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(account.title).font(Theme.Font.body).lineLimit(1)
                Text(accountDetail(account)).font(Theme.Font.label).lineLimit(1)
                    .foregroundStyle(account.signedIn ? AnyShapeStyle(.secondary) : AnyShapeStyle(Theme.danger))
            }
            Spacer(minLength: Theme.Space.s)
            if isDefault { Chip(text: String(localized: "默认")) }
            if account.signedIn, account.unread > 0 { CountBadge(count: account.unread) }
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("feishu-account-row-\(account.id)")
    }

    private func accountDetail(_ account: LarkAccount) -> String {
        if account.quarantined == true { return String(localized: "无法打开，请在飞书网关里重试或移除") }
        if !account.signedIn { return String(localized: "未登录") }
        return account.tenant?.isEmpty == false ? account.tenant! : String(localized: "已登录")
    }

    private func use(_ symbol: String, _ title: String, _ detail: String) -> some View {
        Label {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(title)
                Text(detail).font(Theme.Font.label).foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: symbol).foregroundStyle(Theme.accent)
        }
        .padding(.vertical, Theme.Space.xxs)
    }

    private func load() async {
        loading = true
        defer { loading = false }
        async let list = try? await model.api?.larkAccounts() // a hub from before several accounts: none
        do {
            status = try await model.api?.larkStatus()
            failure = nil
        } catch {
            failure = error.shown.map { String(localized: "读不到飞书的状态：\($0)") }
        }
        accounts = await list
    }
}

/// Settings › Connectors › GitHub (any connector of the hub's, GET /api/v1/connectors): whether it is signed in, as whom, where
/// the sign-in comes from, what is wrong. One the user signs in to here (Telegram; GitHub with its device flow, or a
/// computer's gh login set up on the hub's side) also gets its switch, sign-in, sign-out and delete
/// (ConnectorSignInSections).
struct HubConnectorView: View {
    var id: String
    @Environment(AppModel.self) private var model
    @State private var connector: HubConnector?
    @State private var loading = true
    @State private var failure: String?

    var body: some View {
        SettingsForm {
            // A connector that is only a channel's binding (WeChat's): the channel is where it is set up.
            if ChannelDirectory.shared.list.contains(where: { $0.id == id && $0.source == "plugin" }) {
                ChannelLinkSection(channelID: id)
            }
            Section {
                HStack(spacing: Theme.Space.m) {
                    mark
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(connector?.account ?? title).font(Theme.Font.heading).lineLimit(1)
                        HStack(spacing: Theme.Space.sm) {
                            Circle().fill(tint).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                            Text(stateText).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                        }
                    }
                    Spacer(minLength: Theme.Space.s)
                    if loading { ProgressView() }
                }
                .padding(.vertical, Theme.Space.xs)
                if let via = connector?.via, !via.isEmpty {
                    LabeledContent("登录") { Text(via).lineLimit(2).multilineTextAlignment(.trailing) }
                }
                if let at = connector?.lastSync {
                    LabeledContent("最近一次同步") {
                        TimelineView(.periodic(from: .now, by: 30)) { ctx in
                            Text(Fmt.relative(at, now: ctx.date)).lineLimit(1)
                        }
                    }
                }
                if let e = connector?.error ?? failure, !e.isEmpty {
                    // Waiting out a rate limit is not something broken: said in gray beside the yellow dot.
                    Text(e).font(Theme.Font.label)
                        .foregroundStyle(connector?.status == "rate_limited" ? Color.secondary : Theme.danger)
                        .textSelection(.enabled)
                }
            } footer: {
                if let connector, !connector.signsInHere, !connector.undescribed {
                    Text("登录在 hub 一侧设置（hub.json，见 docs/config.md），这里只看状态。")
                }
            }
            // Telegram switched off lists nothing until it runs: its switch is here all the same.
            if id == "github", let connector, connector.signsInHere {
                GitHubConnectorSections(connector: connector, reload: { await load() })
            } else if let connector, connector.signsInHere || connector.undescribed {
                ConnectorSignInSections(connector: connector, reload: { await load() })
            }
            if connector?.status != "ok", let guide = connector?.guide, !guide.isEmpty {
                Section {
                    ForEach(Array(guide.enumerated()), id: \.offset) { i, step in
                        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.m) {
                            Text("\(i + 1)").font(Theme.Font.labelStrong).monospacedDigit().foregroundStyle(Theme.accent)
                            Text(step).font(Theme.Font.body).textSelection(.enabled)
                        }
                    }
                    Button { Task { await load() } } label: {
                        Label("重新检查", systemImage: Theme.Icon.retry).lineLimit(1)
                    }
                    .disabled(loading)
                } header: {
                    SettingsHeading(String(localized: "怎么修好"))
                }
            }
            if id == "github", let connector, let options = GitHubInboxOptions(connector.options) {
                GitHubInboxSettings(options: options) { connector in self.connector = connector }
            }
        }
        .themedPage()
        .detailTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        // Again once paired (opened at launch, before the hub's address is known) or paired to another hub.
        .task(id: model.config) { await load() }
        // Switched on, its program takes a moment to start and say it signs in: asked again until it has (15 s at most).
        .task(id: starting) {
            for _ in 0..<15 where starting {
                try? await Task.sleep(for: .seconds(1))
                if Task.isCancelled { return }
                await load(quietly: true)
            }
        }
        .refreshable { await load() }
    }

    private var starting: Bool { connector?.enabled != false && connector?.undescribed == true }

    private var title: String { connector?.name ?? (id == "github" ? "GitHub" : id) }

    @ViewBuilder private var mark: some View {
        if let brand = ConnectorText.brand(id) {
            BrandMark(brand, side: Theme.Size.wellM)
        } else {
            Image(systemName: ConnectorText.symbol(id)).resizable().scaledToFit().foregroundStyle(Theme.accent)
                .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
        }
    }

    private var tint: Color { ConnectorText.tone(connector) }

    private var stateText: String {
        guard let connector else { return loading ? String(localized: "正在检查…") : String(localized: "读不到状态") }
        return ConnectorText.state(connector)
    }

    /// `quietly`: no spinner (asked again every second while the program starts).
    private func load(quietly: Bool = false) async {
        guard let api = model.api else { return } // not paired yet: still Checking…
        if !quietly { loading = true }
        defer { loading = false }
        do {
            connector = try await api.connectors().first { $0.id == id }
            failure = connector == nil ? String(localized: "hub 上没有这个连接器") : nil
        } catch {
            failure = error.shown.map { String(localized: "读不到连接器的状态：\($0)") }
        }
    }
}
