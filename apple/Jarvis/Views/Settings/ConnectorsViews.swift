import SwiftUI

// 设置 › 连接器 (2026-11-01): where Jarvis' data comes from and acts — this device's data (phone_* tools), the mail
// accounts, 飞书 (the hub's lark-connector). Mail used to be managed from the inbox's ⋯ menu, this device's data was 手机数据.

/// What a connector is called and says in the settings list.
enum ConnectorText {
    /// This device as a connector: 这台 iPhone / iPad / Mac.
    static var device: String {
        #if os(macOS)
        "这台 Mac"
        #else
        UIDevice.current.userInterfaceIdiom == .pad ? "这台 iPad" : "这台 iPhone"
        #endif
    }

    static func deviceSummary() -> String {
        let on = PhoneDataSource.onThisDevice.filter(\.isEnabled).count
        return on == 0 ? "未打开" : "\(on) 项已打开"
    }

    static func mailSummary(_ accounts: [MailAccount]?) -> String {
        guard let accounts else { return "" }
        if accounts.isEmpty { return "未添加" }
        if accounts.contains(where: { $0.status == "error" }) { return "\(accounts.count) 个 · 有错误" }
        return "\(accounts.count) 个账户"
    }

    static func larkSummary(_ st: LarkStatus?) -> String {
        guard let st else { return "" }
        if !st.configured { return "未配置" }
        return st.connected ? (st.name ?? "已连接") : "连不上"
    }
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

extension APIClient {
    func larkStatus() async throws -> LarkStatus { try await get("/api/v1/lark/status") }
}

/// The 连接器 section of 设置: one row per connector with what it has now.
struct ConnectorsSection: View {
    @Environment(AppModel.self) private var model
    @State private var accounts: [MailAccount]?
    @State private var lark: LarkStatus?

    var body: some View {
        Section {
            NavigationLink(value: SettingsPage.phoneData) {
                LabeledContent {
                    Text(ConnectorText.deviceSummary()).lineLimit(1)
                } label: {
                    Label(ConnectorText.device, systemImage: Theme.Icon.thisDevice)
                }
            }
            NavigationLink(value: SettingsPage.mail) {
                LabeledContent {
                    Text(ConnectorText.mailSummary(accounts)).lineLimit(1)
                } label: {
                    Label("邮箱", systemImage: Theme.Icon.mailAccounts)
                }
            }
            NavigationLink(value: SettingsPage.feishu) {
                LabeledContent {
                    Text(ConnectorText.larkSummary(lark)).lineLimit(1)
                } label: {
                    Label { Text("飞书") } icon: { Image(Theme.Brand.lark).resizable().scaledToFit() }
                }
            }
        } header: {
            Text("连接器")
        } footer: {
            Text("Jarvis 能读、能替你操作的数据来源：助手、AI 分身和 Agent 通过它们查日程、读消息、发邮件；替你出面的操作都会先问你。")
        }
        .task(id: model.settingsPath.count) { await load() } // again when a page closes: it may have changed them
    }

    private func load() async {
        guard let api = model.api else { return }
        async let a = try? api.mailAccounts()
        async let l = try? api.larkStatus()
        let (accs, st) = await (a, l)
        if let accs { accounts = accs }
        if let st { lark = st }
    }
}

/// 设置 › 连接器 › 飞书: whether the hub reaches lark-connector, who is signed in, and what Jarvis does with it. The account
/// itself is signed in on the hub's machine (docs/feishu.md), not here.
struct FeishuConnectorView: View {
    @Environment(AppModel.self) private var model
    @State private var status: LarkStatus?
    @State private var loading = true
    @State private var failure: String?

    var body: some View {
        List {
            Section {
                HStack(spacing: Theme.Space.m) {
                    Image(Theme.Brand.lark).resizable().scaledToFit().scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(status?.name ?? "飞书").font(Theme.Font.heading).lineLimit(1)
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
                     ? "hub 还没接飞书：在 hub 所在的机器上运行 lark-connector，并在 hub.json 的 lark 里填上它（docs/feishu.md）。"
                     : "飞书账号在 hub 所在机器的 lark-connector 里登录，这里只看连接状态。")
            }

            Section("Jarvis 用飞书做什么") {
                use(Theme.Icon.inbox, "收件箱", "私聊、@我、群消息和云文档里提到你的地方，AI 先分拣、起草回复")
                use(Theme.Icon.knowledge, "知识库", "会话、人、群、项目归档下来，给助手和 AI 分身做上下文")
                use(Theme.Icon.ai, "助手", "Jarvis 能搜消息、读文档；发消息等写操作先在手机上问你")
                use(Theme.Icon.agents, "/agent", "在飞书任何地方以 /agent 开头说一句，就起一个 Agent 处理并替你回复")
            }
        }
        .themedPage()
        .navigationTitle("飞书")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private var tint: Color {
        guard let st = status else { return .secondary }
        return st.connected ? Theme.positive : st.configured ? Theme.danger : .secondary
    }

    private var stateText: String {
        guard let st = status else { return loading ? "正在检查…" : "读不到状态" }
        if !st.configured { return "未配置" }
        return st.connected ? "已连接" : "连不上 lark-connector"
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
        do {
            status = try await model.api?.larkStatus()
            failure = nil
        } catch {
            failure = error.shown.map { "读不到飞书的状态：\($0)" }
        }
    }
}
