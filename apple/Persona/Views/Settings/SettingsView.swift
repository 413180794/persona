import BeautifulUI
import SwiftUI
import VisionKit

struct SettingsView: View {
    @Environment(\.shell) private var shell
    @Environment(AppModel.self) private var model
    @State private var confirmUnpair = false
    @State private var showPair = false
    @State private var storage: Int64?
    @State private var plugins: [HubPlugin]?
    @AppStorage(TranslationPrefs.enabledKey) private var translating = false
    @AppStorage(TranslationPrefs.targetKey) private var translateTarget = TranslationPrefs.defaultTarget

    /// The hub row's value: an update out or under way, else its version.
    private var hubSummary: String {
        guard let s = model.hubUpdate else { return "" }
        if s.working { return s.stateTitle }
        if s.latest?.newer == true { return s.channel == "docker" ? "有新镜像" : "有新版本" }
        return s.version
    }

    /// 翻译's row value: off, or the target language.
    private var translationSummary: String { translating ? TranslationPrefs.name(of: translateTarget) : "关闭" }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("SettingsView")
        #endif
        // By value, every page: in the split (iPhone Duo open) they open beside the list, not inside its column.
        SplitNavigation(path: Bindable(model).settingsPath, destinations: SettingsDestinations()) {
            List {
                Section {
                    HStack(spacing: Theme.Space.l) {
                        AppGlyph(size: 54)
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text("Persona").font(Theme.Font.title)
                            if model.isDemo {
                                Text("体验模式 · 数据都是编造的").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                            } else {
                                if model.isMultiHub {
                                    // Several groups of hubs: the one the hub-level settings below are about, to pick another.
                                    HubSwitcher()
                                } else {
                                    Text(model.config?.url.absoluteString ?? "").font(Theme.Font.code).foregroundStyle(.secondary).lineLimit(1)
                                }
                                HStack(spacing: Theme.Space.sm) {
                                    Circle().fill(model.live.state == .connected ? Theme.positive : Theme.caution).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                                    Text(linkState).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                                }
                            }
                        }
                    }
                    .padding(.vertical, Theme.Space.xs)
                }

                if model.isDemo {
                    DemoSection(pair: { showPair = true })
                }

                AppearanceSection()

                Section {
                    Group {
                        NavigationLink(value: ModelLaunchersRoute()) {
                            SettingsRow(title: "模型供应商", symbol: Theme.Icon.cpu, tint: Theme.Hue.purple)
                        }
                        NavigationLink(value: SettingsPage.modelConfiguration) {
                            SettingsRow(title: "模型配置", symbol: Theme.Icon.model, tint: Theme.ai)
                        }
                        NavigationLink(value: SettingsPage.assistant) {
                            // No model here: the assistant's model is 模型配置 › 主对话 (#1291).
                            SettingsRow(title: "助手", symbol: Theme.Icon.ai, tint: Theme.ai)
                        }
                        NavigationLink(value: SettingsPage.claude) {
                            SettingsRow(title: "Claude Code 设置", symbol: Theme.Icon.options, tint: Theme.Hue.orange)
                        }
                        NavigationLink(value: SettingsPage.codex) {
                            SettingsRow(title: "Codex 设置", symbol: Theme.Icon.options, tint: Theme.Hue.gray)
                        }
                    }
                    .bareListRow()
                }

                ConnectorsSection()

                // 插件 (docs/plugins.md): only with a hub that has the plugin base.
                if model.hubFeatures.contains("plugin_manager") {
                    Section {
                        Group {
                            NavigationLink(value: SettingsPage.plugins) {
                                SettingsRow(title: "插件", symbol: Theme.Icon.plugins, tint: Theme.Hue.purple,
                                            value: PluginText.listSummary(plugins), look: .oneLine)
                            }
                            .accessibilityIdentifier("settings-plugins")
                        }
                        .bareListRow()
                    }
                    .task(id: model.settingsPath.count) { plugins = (try? await model.api?.plugins()) ?? plugins }
                }

                Section {
                    Group {
                        NavigationLink(value: MachineListRoute()) {
                            SettingsRow(title: "机器", symbol: Theme.Icon.machines, tint: Theme.Hue.blue,
                                        value: "\(model.machines.filter(\.online).count) / \(model.machines.count) 在线")
                        }
                        NavigationLink(value: SettingsPage.devices) {
                            SettingsRow(title: "设备", symbol: Theme.Icon.credentials, tint: Theme.Hue.gray)
                        }
                        NavigationLink(value: SettingsPage.notifications) {
                            SettingsRow(title: "通知", symbol: Theme.Icon.notifications, tint: Theme.danger)
                        }
                        NavigationLink(value: SettingsPage.translation) {
                            SettingsRow(title: "翻译", symbol: Theme.Icon.translate, tint: Theme.accent, value: translationSummary, look: .oneLine)
                        }
                        NavigationLink(value: SettingsPage.storage) {
                            SettingsRow(title: "存储空间", symbol: "internaldrive", tint: Theme.Hue.gray, value: storage?.storageText ?? "", look: .digits)
                        }
                    }
                    .bareListRow()
                }

                #if os(iOS)
                // A Mac pairs with no watch (WatchSync does nothing there): no button that would claim it sent something.
                if !model.isDemo { watchSection }
                #endif

                if !model.isDemo {
                    Section {
                        Group {
                            // Every hub paired, by group: add one, rename, disconnect one (docs/multi-hub.md). Also with
                            // one hub: it is where a second one is added (the only row the single-hub page gained).
                            NavigationLink(value: SettingsPage.hubs) {
                                SettingsRow(title: "hub", symbol: Theme.Icon.hubGroup, tint: Theme.Hue.green,
                                            value: model.isMultiHub ? "\(model.groupIDs.count) 组" : model.groupName(model.hub?.group ?? ""),
                                            look: .oneLine)
                            }
                            .accessibilityIdentifier("settings.hubs")
                            NavigationLink(value: SettingsPage.hub) {
                                SettingsRow(title: "hub", symbol: Theme.Icon.server, tint: Theme.Hue.blue, value: hubSummary, look: .oneLine)
                            }
                            .accessibilityIdentifier("settings.hub")
                            Button { showPair = true } label: { SettingsRow(title: "重新配对", symbol: "qrcode.viewfinder", tint: Theme.accent) }
                                .modifier(SettingsButtonLook())
                            Button(role: .destructive) { confirmUnpair = true } label: {
                                SettingsRow(title: model.isMultiHub ? "断开所有 hub" : "断开并忘记服务器", symbol: Theme.Icon.end, tint: Theme.danger)
                                    #if os(macOS)
                                    .foregroundStyle(Theme.danger) // a plain row on the Mac: red says what it does
                                    #endif
                            }
                            .modifier(SettingsButtonLook())
                        }
                        .bareListRow()
                    } header: {
                        SettingsHeading("服务器")
                    }
                }

                Section {
                    Group {
                        let version = "\(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "") (\(Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? ""))"
                        // The commit this build was made from (stamped at build time, `project.yml`): which one is on the phone.
                        let commit = Bundle.main.object(forInfoDictionaryKey: "JarvisCommit") as? String ?? "未知"
                        if AppStyle.current.usesBeautifulUI {
                            BUIListRow("版本", value: version)
                            BUIListRow("构建") { EmptyView() } trailing: {
                                Text(commit).font(Theme.Font.code).foregroundStyle(.secondary).textSelection(.enabled)
                            }
                        } else {
                            LabeledContent("版本", value: version)
                            LabeledContent("构建") {
                                Text(commit)
                                    .font(Theme.Font.code)
                                    .textSelection(.enabled)
                            }
                        }
                    }
                    .bareListRow()
                } header: {
                    SettingsHeading("关于")
                }
            }
            #if os(macOS)
            // Below the section's title in the title bar's row, not scrolling through it (#1243); inside the page's
            // background, which still reaches up behind the title.
            .modifier(MacBelowTitleRow())
            #endif
            .themedPage()
            .toolbar {
                    // As a sheet over the shell (docs/ios.md「新外壳」): ChatGPT's × to close it.
                    if shell != nil {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button { model.shellSettings = false } label: { Label("关闭", systemImage: Theme.Icon.close) }
                                .foregroundStyle(Color.primary)
                                .accessibilityIdentifier("settings-close")
                        }
                    }
                }
                .navigationTitle("设置")
            .task { await model.refreshAssistant() }
            // Measured each time the page shows and whenever a page closes (back from 存储空间 after clearing; in the
            // split the list never leaves the screen).
            .onAppear(perform: measureStorage)
            .onChange(of: model.settingsPath) { measureStorage() }
            #if DEBUG
            // With `-tab settings`: `-openAssistantPage assistant|routines|routine|newRoutine|followups|memory|memoryFile|
            // memoryEdit|about`, `-openStorage YES` (存储空间), `-openNotificationSettings YES` (通知), `-openMachines YES` (机器;
            // with `-openMachineDetail <id>` that machine's page), `-openDevices YES` (设备), `-openHubUpdate YES` (hub),
            // `-openSkills YES` (Skills), `-openPlugins YES` (插件; `-openPlugin <id>` one of them).
            .onAppear(perform: openDebugPage)
            #endif
            .confirmationDialog("断开后需要重新扫码配对。", isPresented: $confirmUnpair, titleVisibility: .visible) {
                Button("断开", role: .destructive) { model.unpair() }
            }
            .sheet(isPresented: $showPair) { PairView() }
            #if os(macOS) && DEBUG
            // `-tab settings -openPairSheet YES` (体验模式, or paired: 重新配对): the 连接 hub sheet, for screenshots
            // (scripts/app/mac_shot.sh; with `-openPairSSH YES` its SSH 隧道 sheet over it).
            .onAppear { if UserDefaults.standard.bool(forKey: "openPairSheet") { showPair = true } }
            #endif
        } empty: {
            SplitPlaceholder(symbol: Theme.Icon.settings, title: "设置", message: "左边选一项。")
        }
    }

    private func measureStorage() {
        Task { storage = await Task.detached(priority: .utility) { StorageUsage.scan().values.reduce(0, +) }.value }
    }

    /// The header's line about the link: connected (and how, when it goes through the hub's SSH tunnel), or why not.
    private var linkState: String {
        guard model.live.state == .connected else { return model.connection.phase == .online ? "未连接" : model.connection.phase.title }
        let tunneled = model.hub.flatMap { model.tunnel($0.group) }?.routed == true
        return tunneled ? "已连接 hub · 经 SSH 隧道" : "已连接 hub"
    }

    #if DEBUG
    /// Once per launch: a new look rebuilds the tree (and this view's state), and the route would undo a page opened since.
    @MainActor private static var debugOpened = false

    private func openDebugPage() {
        guard !Self.debugOpened else { return }
        let d = UserDefaults.standard
        // Appended with their own types: a path of AnyHashable finds no destination.
        var p = NavigationPath()
        if d.bool(forKey: "openMachines") {
            p.append(MachineListRoute())
            if let id = d.string(forKey: "openMachineDetail") { p.append(MachineDetailRoute(id: id)) }
        }
        else if d.bool(forKey: "openStorage") { p.append(SettingsPage.storage) }
        else if d.bool(forKey: "openNotificationSettings") { p.append(SettingsPage.notifications) }
        else if d.string(forKey: "openAssistantPage") != nil { p.append(SettingsPage.assistant) }
        else if d.bool(forKey: "openDevices") { p.append(SettingsPage.devices) }
        else if d.bool(forKey: "openHubs") || d.bool(forKey: "openSSHTunnel") {
            p.append(SettingsPage.hubs)
            // `-openSSHTunnel YES`: the first hub's 设置 › hub › SSH 隧道 (screenshots).
            if d.bool(forKey: "openSSHTunnel"), let first = model.registry.hubs.first { p.append(SettingsPage.sshTunnel(first.id)) }
        }
        else if d.bool(forKey: "openHubUpdate") { p.append(SettingsPage.hub) }
        else if d.bool(forKey: "openSkills") { p.append(SettingsPage.skills) }
        else if d.bool(forKey: "openPlugins") || d.string(forKey: "openPlugin") != nil { // 设置 › 插件 (› one (› a page of it))
            p.append(SettingsPage.plugins)
            if let id = d.string(forKey: "openPlugin") {
                p.append(SettingsPage.plugin(id))
                if let ui = d.string(forKey: "openPluginPage") { p.append(SettingsPage.pluginPage(plugin: id, ui: ui, title: "")) }
            }
        }
        else if d.bool(forKey: "openPluginMarket") || d.bool(forKey: "openPluginMarkets") || d.string(forKey: "openMarketPlugin") != nil {
            // 设置 › 插件 › 插件市场 (› 市场, or › <market>/<plugin>)
            p.append(SettingsPage.plugins)
            p.append(SettingsPage.pluginMarket)
            if d.bool(forKey: "openPluginMarkets") { p.append(SettingsPage.pluginMarkets) }
            if let m = d.string(forKey: "openMarketPlugin"), let slash = m.firstIndex(of: "/") {
                p.append(SettingsPage.marketPlugin(market: String(m[..<slash]), id: String(m[m.index(after: slash)...])))
            }
        }
        else if d.bool(forKey: "openCodexSettings") { p.append(SettingsPage.codex) }
        else if d.bool(forKey: "openClaudeSettings") { p.append(SettingsPage.claude) } // with its 屏蔽的配置目录
        else if d.bool(forKey: "openConnectorMail") || d.bool(forKey: "addMail") { p.append(SettingsPage.mail) }
        else if d.bool(forKey: "openConnectorFeishu") { p.append(SettingsPage.feishu) }
        else if d.bool(forKey: "openConnectorGitHub") { p.append(SettingsPage.connector("github")) }
        else if d.bool(forKey: "openConnectorTelegram") { p.append(SettingsPage.connector("telegram")) }
        else if d.bool(forKey: "openConnectorMessages") || d.string(forKey: "openMessagesChat") != nil { // 信息 (› 会话 › one)
            p.append(SettingsPage.connector("imessage"))
            if d.bool(forKey: "openMessagesChats") || d.string(forKey: "openMessagesChat") != nil { p.append(MessagesChatsRoute()) }
            if let id = d.string(forKey: "openMessagesChat") { p.append(MessagesThreadRoute(id: id, name: "")) }
        }
        else if d.bool(forKey: "openConnectorBot") { p.append(SettingsPage.feishuBot) }
        else if d.bool(forKey: "openConnectorDevice") { p.append(SettingsPage.phoneData) }
        else if d.bool(forKey: "openTranslationSettings") { p.append(SettingsPage.translation) }
        else if let a = d.string(forKey: "openAppearance") { // persona (or YES) | color | style | icon | layout | scheme
            p.append(a == "color" ? SettingsPage.themeColor : a == "style" ? SettingsPage.style
                     : a == "icon" ? SettingsPage.appIcon : a == "layout" ? SettingsPage.layout
                     : a == "scheme" ? SettingsPage.colorScheme : SettingsPage.persona)
        }
        else if d.bool(forKey: "openModelConfiguration") {
            p.append(SettingsPage.modelConfiguration)
            if let id = d.string(forKey: "openModelPurpose") { p.append(ModelPurposeRoute(id: id)) }
        }
        else if let n = d.string(forKey: "openLauncherPage") { p.append(ModelLaunchersRoute()); p.append(ModelLauncherRoute(name: n)) }
        else if d.bool(forKey: "openModelLaunchers") || d.string(forKey: "openModelLauncher") != nil { // 设置 › 模型 (› one)
            p.append(ModelLaunchersRoute())
            if let n = d.string(forKey: "openModelLauncher") { p.append(ModelLauncherRoute(name: n)) }
        }
        guard !p.isEmpty else { return }
        Self.debugOpened = true
        model.settingsPath = p
    }
    #endif

    private var watchSection: some View {
        Section {
            Group {
                Button {
                    if let cfg = model.config {
                        WatchSync.shared.push(config: cfg)
                        WatchSync.shared.push(summary: model.watchSummary())
                        Haptics.success()
                        model.toast = Toast("已发送到 Apple Watch，打开手表上的 Persona 即可", style: .success)
                    }
                } label: {
                    if AppStyle.current.usesBeautifulUI {
                        SettingsRow(title: "同步到 Apple Watch", symbol: Theme.Icon.watch, tint: Theme.accent)
                    } else {
                        Label("同步到 Apple Watch", systemImage: Theme.Icon.watch).lineLimit(1)
                    }
                }
                .modifier(SettingsButtonLook())
            }
            .bareListRow()
        } footer: {
            Text("手表上的 Persona 用 iPhone 的配对信息直接连 hub；手表显示「未配对」时点这里，或在手表上点「从 iPhone 同步」。")
        }
    }
}

/// The pages of 设置 that are not routes of their own elsewhere.
enum SettingsPage: Hashable {
    case persona, themeColor, style, appIcon, layout, colorScheme, assistant, claude, codex, devices, phoneData, mail, feishu, feishuBot, notifications, storage, translation, modelConfiguration
    /// 设置 › hub: the hubs paired, by group (docs/multi-hub.md).
    case hubs
    case hub, skills
    /// A connector of the hub's by its id (GET /api/v1/connectors): GitHub…
    case connector(String)
    /// 设置 › 插件, and one plugin by its id (docs/plugins.md).
    case plugins
    case plugin(String)
    /// A page a plugin draws (placement plugin_page, docs/plugins.md「声明式 UI」), titled until its View says otherwise.
    case pluginPage(plugin: String, ui: String, title: String)
    /// 设置 › 插件 › 插件市场, one plugin of a market, and the markets (docs/plugins.md「插件市场」).
    case pluginMarket
    case marketPlugin(market: String, id: String)
    case pluginMarkets
    /// 设置 › hub › SSH 隧道 of one paired hub (its id; docs/network.md「经 SSH 隧道连 hub」).
    case sshTunnel(String)

}

/// The pages the settings list opens, in either layout (`SplitNavigation`). MachinesView pushes its cards by value
/// (MachineDetailRoute), and a machine's page its agents (SessionKey).
private struct SettingsDestinations: ViewModifier {
    func body(content: Content) -> some View {
        content
            .navigationDestination(for: SettingsPage.self) { page(for: $0).splitDetailPage() }
            .navigationDestination(for: MachineListRoute.self) { _ in MachinesView().splitDetailPage() }
            .navigationDestination(for: ModelLaunchersRoute.self) { _ in ModelLaunchersView().splitDetailPage() }
            .navigationDestination(for: ModelPurposeRoute.self) { r in ModelPurposeEditorView(purposeID: r.id).splitDetailPage() }
            .navigationDestination(for: ModelLauncherRoute.self) { r in ModelLauncherDetailView(name: r.name).splitDetailPage() }
            .navigationDestination(for: MachineDetailRoute.self) { r in MachineDetailView(ref: r.ref).splitDetailPage() }
            .navigationDestination(for: MessagesChatsRoute.self) { _ in MessagesChatListView().splitDetailPage() }
            .navigationDestination(for: MessagesThreadRoute.self) { r in MessagesThreadView(id: r.id, name: r.name).splitDetailPage() }
            .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key).splitDetailPage() }
    }

    @ViewBuilder private func page(for page: SettingsPage) -> some View {
        switch page {
        case .persona: PersonaSettingsPage()
        case .themeColor: ThemeColorSettingsPage()
        case .style: StyleSettingsPage()
        #if os(iOS)
        case .appIcon: AppIconSettingsPage()
        #else
        case .appIcon: EmptyView()
        #endif
        #if os(iOS)
        case .layout: LayoutSettingsPage()
        case .colorScheme: ColorSchemeSettingsPage()
        #else
        case .layout, .colorScheme: EmptyView()
        #endif
        case .assistant: AssistantProfileView()
        case .claude: ClaudeSettingsView()
        case .codex: ClaudeSettingsView(agent: .codex)
        case .devices: DevicesView()
        case .phoneData: PhoneDataView()
        case .mail: MailAccountsView(inSheet: false)
        case .feishu: FeishuConnectorView()
        case .connector(let id):
            if id == "imessage" { MessagesConnectorView() } else { HubConnectorView(id: id) }
        case .feishuBot: FeishuBotView()
        case .notifications: NotificationSettingsView()
        case .storage: StorageView()
        case .translation: TranslationSettingsView()
        case .modelConfiguration: ModelConfigurationView()
        case .hubs: HubsView()
        case .sshTunnel(let id): HubSSHTunnelPage(hubID: id)
        case .hub: HubSettingsPage()
        case .skills: SkillsPage()
        case .plugins: PluginsView()
        case .plugin(let id): PluginDetailView(id: id)
        case .pluginPage(let plugin, let ui, let title): PluginPageView(plugin: plugin, ui: ui, title: title)
        case .pluginMarket: PluginMarketView()
        case .marketPlugin(let market, let id): MarketPluginDetailView(market: market, id: id)
        case .pluginMarkets: PluginMarketsView()
        }
    }

}

/// 设置 in 体验模式: what it is, and the two ways out.
private struct DemoSection: View {
    @Environment(AppModel.self) private var model
    var pair: () -> Void

    var body: some View {
        Section {
            Group {
                #if os(macOS)
                // Rows like the list's other rows on the Mac (#1224 B4): as buttons they were two small gray capsules
                // at the list's leading edge, the sidebar-style list's own button look.
                Button(action: pair) {
                    SettingsRow(title: "连接我的 hub…", symbol: "qrcode.viewfinder", tint: Theme.accent)
                }
                .modifier(SettingsButtonLook())
                Button(role: .destructive) { model.unpair() } label: {
                    SettingsRow(title: "退出体验模式", symbol: "rectangle.portrait.and.arrow.right", tint: Theme.danger)
                        .foregroundStyle(Theme.danger)
                }
                .modifier(SettingsButtonLook())
                .accessibilityIdentifier("settings.leaveDemo")
                #else
                Button(action: pair) {
                    if AppStyle.current.usesBeautifulUI {
                        SettingsRow(title: "连接我的 hub", symbol: "qrcode.viewfinder", tint: Theme.accent)
                    } else {
                        Label("连接我的 hub", systemImage: "qrcode.viewfinder").lineLimit(1)
                    }
                }
                .modifier(SettingsButtonLook())
                Button(role: .destructive) { model.unpair() } label: {
                    if AppStyle.current.usesBeautifulUI {
                        SettingsRow(title: "退出体验模式", symbol: "rectangle.portrait.and.arrow.right", tint: Theme.danger)
                    } else {
                        Label("退出体验模式", systemImage: "rectangle.portrait.and.arrow.right").lineLimit(1)
                    }
                }
                .modifier(SettingsButtonLook())
                .accessibilityIdentifier("settings.leaveDemo")
                #endif
            }
            .bareListRow()
        } header: {
            SettingsHeading("体验模式")
        } footer: {
            Text("现在看到的机器、Agent、对话和收件箱都是演示数据，不连接任何服务器，发消息、审批等操作也不会真的发出去。连接自己的 hub 后自动退出体验模式。")
        }
    }
}

/// 设置 › 连接器 › 这台 iPhone: which of the device's data Claude may use (phone_* tools). One switch per source, and
/// the switch is the whole state: on = turned on and allowed (2026-11-01, no 已授权 / 点此授权 labels). Turning one on
/// asks for the permission; refused (or refused before) it stays off and 设置 opens where it can be allowed.
struct PhoneDataView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @State private var statuses: [PhoneDataSource: PhoneAuthStatus] = [:]
    @State private var enabled: [PhoneDataSource: Bool] = [:]
    @State private var asking: PhoneDataSource?

    var body: some View {
        List {
            Section {
                ForEach(PhoneDataSource.allCases) { s in
                    HStack(spacing: Theme.Space.m) {
                        SourceIcon(source: s)
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text(s.title).lineLimit(1)
                            Text(s.detail).font(Theme.Font.label).foregroundStyle(.secondary)
                        }
                        Spacer(minLength: Theme.Space.s)
                        if asking == s {
                            ProgressView()
                        } else {
                            Toggle(s.title, isOn: Binding(get: { isOn(s) }, set: { v in Task { await set(s, v) } }))
                                .labelsHidden()
                        }
                    }
                    .padding(.vertical, Theme.Space.xxs)
                }
            } footer: {
                Text("打开的数据，Claude 可以通过 phone_* 工具读取；新建日程、提醒等写操作会先在手机上问你。打开时会请求系统权限，以前拒绝过的会跳到系统设置。Persona 在前台（或刚切到后台）时，hub 才能调到手机。iOS 不开放短信、备忘录、第三方 App 的数据。")
            }
        }
        .themedPage()
        .navigationTitle(ConnectorText.device)
        .navigationBarTitleDisplayMode(.inline) // like the other connectors' pages
        .task { refresh() }
        // Back from the system's Settings, where a permission may have been given or taken away.
        .onChange(of: phase) { _, p in if p == .active { refresh() } }
    }

    private func isOn(_ s: PhoneDataSource) -> Bool {
        (enabled[s] ?? s.isEnabled) && (statuses[s] ?? PhoneAuthorization.status(s)).isUsable
    }

    private func refresh() {
        for s in PhoneDataSource.allCases { statuses[s] = PhoneAuthorization.status(s) }
    }

    private func set(_ s: PhoneDataSource, _ on: Bool) async {
        defer { model.registerPhone() } // what the hub may call follows the switch
        guard on else {
            s.isEnabled = false
            enabled[s] = false
            return
        }
        var st = statuses[s] ?? PhoneAuthorization.status(s)
        if st == .notDetermined {
            asking = s
            st = await PhoneAuthorization.request(s)
            asking = nil
            statuses[s] = st
        }
        if st.isUsable {
            s.isEnabled = true
            enabled[s] = true
            Haptics.tap()
        } else {
            // Refused: iOS asks only once, so the rest is in its Settings; the switch stays off until it is allowed there.
            model.toast = Toast("在系统设置里允许 Persona 访问\(s.title)", style: .info)
            s.isEnabled = true // on its own once allowed there
            enabled[s] = true
            if let url = URL(string: UIApplication.openSettingsURLString) { await UIApplication.shared.open(url) }
        }
    }
}

/// A data source's icon: the iPhone app it comes from, else its symbol on a tile of the same size and shape.
private struct SourceIcon: View {
    var source: PhoneDataSource

    var body: some View {
        if let icon = source.appIcon {
            BrandMark(icon, side: Theme.Size.wellS)
        } else {
            Image(systemName: source.symbol)
                .font(Theme.Font.heading)
                .foregroundStyle(.white)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Theme.accent.gradient)
                .scaledFrame(width: Theme.Size.wellS, height: Theme.Size.wellS)
                .clipShape(AppIconShape())
                .accessibilityHidden(true)
        }
    }
}

struct AppGlyph: View {
    @Environment(AppModel.self) private var model
    @Environment(\.colorScheme) private var pageLook
    var size: CGFloat
    var body: some View {
        // Persona's mark beside its name (设置's header, the welcome page) is the app icon itself: the one picked in
        // 设置 › 外观 › App 图标, in the look the home screen shows it in (#1312).
        let icon = model.appIcon.current
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.appIcon(size), style: .continuous)
        Image(icon.preview)
            .resizable()
            .interpolation(.high)
            .frame(width: size, height: size)
            .environment(\.colorScheme, icon.homeScreenLook(model.colorScheme, system: systemLook))
            .clipShape(shape)
            .overlay(shape.stroke(Theme.cardStroke, lineWidth: Theme.Size.line))
    }

    /// Under 跟随系统 the page's look (read here so the glyph redraws when the system's changes); else the screen's.
    private var systemLook: ColorScheme {
        #if os(iOS)
        model.colorScheme == .system ? pageLook : AppIconChoice.systemLook
        #else
        pageLook
        #endif
    }
}

struct WelcomeView: View {
    @Environment(AppModel.self) private var model
    @State private var showPair = false
    @State private var appear = false
    // The iPhone scans the QR code `personad hub pair` prints; the Mac has no camera and pastes the link it prints.
    #if os(macOS)
    private static let tagline = "把每台电脑上的 Claude 收进一个窗口"
    private static let pairIcon = Theme.Icon.link
    private static let pairHint: LocalizedStringKey = "在 hub 所在的机器上运行 `personad hub pair`，拷贝它打印的链接"
    #else
    private static let tagline = "把每台电脑上的 Claude 装进口袋"
    private static let pairIcon = "qrcode.viewfinder"
    private static let pairHint: LocalizedStringKey = "在 hub 所在的机器上运行 `personad hub pair` 显示二维码"
    #endif

    var body: some View {
        VStack(spacing: 0) {
            Spacer()
            AppGlyph(size: 96)
                .scaleEffect(appear ? 1 : 0.8)
                .opacity(appear ? 1 : 0)
            Text("Persona")
                .font(Theme.Font.display)
                .padding(.top, Theme.Space.xl)
            Text(Self.tagline)
                .font(Theme.Font.title)
                .foregroundStyle(.secondary)
                .padding(.top, Theme.Space.sm)
            VStack(alignment: .leading, spacing: Theme.Space.l) {
                feature("bubble.left.and.text.bubble.right", "像聊天一样驱动 Claude", "你的每台电脑，herdr 里的同一个会话")
                feature("bolt.horizontal", "实时看它在做什么", "思考、工具调用、终端画面，流式更新")
                #if os(macOS)
                // A Mac decides in its own window and has no health data (PhoneDataSource.onThisDevice).
                feature(Theme.Icon.click, "在一个窗口里拍板", "回答提问、审批计划、授权工具，不用切到终端")
                // The phone's data comes from the iPhone's Persona; the Mac is a desktop (AppModel.registration).
                feature(Theme.Icon.mailAccounts, "带上邮箱和你的 iPhone", "Gmail 等邮箱；日历、通讯录、照片来自 iPhone 上的 Persona")
                #else
                feature("hand.tap", "在手机上拍板", "回答提问、审批计划、授权工具，手表也行")
                feature("iphone.gen3", "带上你的手机和邮箱", "日历、提醒、通讯录、健康、照片、Gmail")
                #endif
            }
            .padding(.top, 40)
            .padding(.horizontal, Theme.Space.xxl)
            Spacer()
            Button { showPair = true } label: {
                Label("连接 hub", systemImage: Self.pairIcon).font(Theme.Font.heading).scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.well)
            }
            .themedButton(prominent: true)
            .controlSize(.large)
            .padding(.horizontal, Theme.Space.xl)
            .accessibilityIdentifier("welcome.pair")
            Text(Self.pairHint)
                .font(Theme.Font.label)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .padding(.top, Theme.Space.m)
            // Without a hub there is nothing else to see: App Review and friends with the TestFlight link start here.
            Button { model.enterDemo() } label: {
                Label("没有 hub？先体验一下", systemImage: "play.circle").font(Theme.Font.heading).lineLimit(1).scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.well)
            }
            .themedButton()
            .controlSize(.large)
            .padding(.horizontal, Theme.Space.xl)
            .padding(.top, Theme.Space.l)
            .accessibilityIdentifier("welcome.demo")
            Text("用编造的数据看看每个页面，不连接任何服务器")
                .font(Theme.Font.label)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .padding(.top, Theme.Space.s)
                .padding(.bottom, Theme.Space.m)
        }
        // iPad: a centered column, not buttons from edge to edge.
        .frame(maxWidth: Theme.Size.readable)
        .frame(maxWidth: .infinity)
        .sheet(isPresented: $showPair) { PairView() }
        .onAppear { withAnimation(.spring(duration: 0.8, bounce: 0.35)) { appear = true } }
        #if os(macOS) && DEBUG
        // `-openWelcome YES -openPairSheet YES`: the 连接 hub sheet, for screenshots (scripts/app/mac_shot.sh).
        .onAppear { if UserDefaults.standard.bool(forKey: "openPairSheet") { showPair = true } }
        #endif
    }

    private func feature(_ symbol: String, _ title: String, _ detail: String) -> some View {
        HStack(alignment: .top, spacing: Theme.Space.l) {
            Image(systemName: symbol)
                .font(Theme.Font.title)
                .foregroundStyle(Theme.accent)
                .scaledFrame(width: Theme.Size.wellS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(title).font(Theme.Font.heading)
                Text(detail).font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
        }
    }
}

struct PairView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var url = ""
    @State private var token = ""
    /// The SSH tunnel set up here, for a hub this device cannot reach directly (docs/network.md「经 SSH 隧道连 hub」).
    @State private var ssh: SSHRoute?
    #if os(macOS)
    @State private var editingSSH = false
    #endif
    @State private var connecting = false
    @State private var error: String?
    @State private var scanning = DataScannerViewController.isSupported && DataScannerViewController.isAvailable

    var body: some View {
        NavigationStack {
            Form {
                if scanning {
                    Section {
                        QRScanner { code in
                            if let cfg = ServerConfig(pairURL: code) {
                                url = cfg.url.absoluteString
                                token = cfg.token
                                scanning = false
                                Task { await connect() }
                            }
                        }
                        #if os(iOS)
                        .scaledFrame(height: Theme.Size.box280)
                        .clipShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous))
                        .listRowInsets(EdgeInsets())
                        #endif
                    } footer: {
                        #if os(iOS)
                        Text("扫描 `personad hub pair` 打印的二维码。")
                        #else
                        Text("拷贝 `personad hub pair` 打印的配对链接（jarvis://pair?…），再点「粘贴配对链接」。")
                        #endif
                    }
                }
                Section("手动输入") {
                    #if os(macOS)
                    // A grouped form puts the label on the left and the field on the right with no box: the prompt
                    // shows where to type.
                    TextField("服务器地址", text: $url, prompt: Text(verbatim: "http://hub.example.com:18660")).autocorrectionDisabled()
                    SecureField("Token", text: $token, prompt: Text("配对链接里的 token"))
                    #else
                    TextField("服务器地址", text: $url).keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                    // A one-time credential, not a password: iOS would otherwise offer to save it as one ("Save
                    // Password?") the moment the page moves on, e.g. to 经 SSH 隧道连接.
                    SecureField("Token", text: $token).textContentType(.oneTimeCode)
                    #endif
                }
                Section {
                    #if os(macOS)
                    // A sheet of its own over this one, with its own 取消 / 保存, not a page pushed inside the sheet: a
                    // pushed page got an iPhone's round back button, and the sheet shrank to the page's minimum width
                    // while the form kept its own, cut off on the right (#1170).
                    LabeledContent {
                        HStack(spacing: Theme.Space.s) {
                            if let ssh { Text(ssh.label).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle) }
                            Button(ssh == nil ? "设置…" : "修改…") { editingSSH = true }
                                .accessibilityIdentifier("pair.ssh")
                        }
                    } label: {
                        Label("经 SSH 隧道连接", systemImage: Theme.Icon.sshTunnel).lineLimit(1)
                    }
                    #else
                    NavigationLink {
                        SSHTunnelForm(saved: ssh, dismissOnSave: true) { ssh = $0 }.navigationTitle("SSH 隧道")
                    } label: {
                        SettingsRow(title: "经 SSH 隧道连接", symbol: Theme.Icon.sshTunnel, tint: Theme.Hue.green,
                                    value: ssh.map { $0.label } ?? "", look: .oneLine)
                    }
                    .accessibilityIdentifier("pair.ssh")
                    #endif
                } footer: {
                    Text(ssh == nil ? "这台设备访问不到 hub 的地址时（不在同一个网络、没开 VPN），可以经一台能访问 hub 的 SSH 服务器转过去。"
                                    : ssh!.always ? "总是经 SSH 隧道连接这台 hub。" : "先直连这个地址，连不上再经 SSH 隧道。")
                }
                if let error { Section { Text(error).foregroundStyle(Theme.danger).font(Theme.Font.caption) } }
            }
            .themedPage()
            #if os(macOS)
            // A window sheet does not take the main window's grouped forms: without this the Mac laid the fields out
            // in two columns, the labels flush against the sheet's left edge (2026-11-02).
            .macSheet()
            .sheet(isPresented: $editingSSH) {
                NavigationStack {
                    SSHTunnelForm(saved: ssh, dismissOnSave: true) { ssh = $0 }.navigationTitle("SSH 隧道")
                }
            }
            #if DEBUG
            // `-openPairSheet YES -openPairSSH YES`: this sheet's SSH 隧道 sheet, for screenshots.
            .onAppear { if UserDefaults.standard.bool(forKey: "openPairSSH") { editingSSH = true } }
            #endif
            // ⌘V lands in the focused field (the address field has it when the sheet opens), not on the paste button:
            // a pairing link pasted there pairs as the button does; an address stays an address.
            .onChange(of: url) { _, typed in
                guard let cfg = ServerConfig(pairURL: typed) else { return }
                url = cfg.url.absoluteString
                token = cfg.token
                Task { await connect() }
            }
            #endif
            .navigationTitle("连接 hub")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button { Task { await connect() } } label: { if connecting { ProgressView() } else { Text("连接") } }
                        .disabled(connecting || token.isEmpty || URL(string: url) == nil)
                }
            }
        }
    }

    private func connect() async {
        guard let u = URL(string: url.trimmingCharacters(in: .whitespaces)) else { return }
        connecting = true
        defer { connecting = false }
        do {
            try await model.pair(ServerConfig(url: u, token: token.trimmingCharacters(in: .whitespaces)), ssh: ssh)
            Haptics.success()
            dismiss()
        } catch {
            if ssh != nil {
                if !error.isCancellation { self.error = "连接失败：\(error.localizedDescription)" }
            } else {
            #if os(macOS)
            if !error.isCancellation { self.error = "连接失败：\(error.localizedDescription)。确认这台 Mac 能访问这个地址（比如在同一个网络或 VPN 上），或者经 SSH 隧道连接。" }
            #else
            if !error.isCancellation { self.error = "连接失败：\(error.localizedDescription)。确认手机能访问这个地址（比如在同一个网络或 VPN 上），或者经 SSH 隧道连接。" }
            #endif
            }
            Haptics.error()
        }
    }
}

#if os(iOS)
struct QRScanner: UIViewControllerRepresentable {
    var onCode: (String) -> Void

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let vc = DataScannerViewController(recognizedDataTypes: [.barcode(symbologies: [.qr])], qualityLevel: .balanced,
                                           isHighlightingEnabled: true)
        vc.delegate = context.coordinator
        try? vc.startScanning()
        return vc
    }

    func updateUIViewController(_ vc: DataScannerViewController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(onCode: onCode) }

    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        let onCode: (String) -> Void
        init(onCode: @escaping (String) -> Void) { self.onCode = onCode }

        func dataScanner(_ scanner: DataScannerViewController, didAdd items: [RecognizedItem], allItems: [RecognizedItem]) {
            for item in items {
                if case .barcode(let b) = item, let s = b.payloadStringValue {
                    Haptics.success()
                    onCode(s)
                    return
                }
            }
        }
    }
}
#endif

/// Claude Code's /config options (a whitelist of ~/.claude/settings.json keys) on each machine.
/// Which agent's settings a page edits: Claude Code's settings.json or Codex's config.toml (the node keeps a whitelist
/// of each, claudeconfig.go / codexconfig.go).
enum AgentSettingsKind {
    case claude, codex

    var title: String { self == .claude ? "Claude Code 设置" : "Codex 设置" }

    var footer: String {
        switch self {
        case .claude: "改的是那台机器的 ~/.claude/settings.json，只动下面这几项；新开的 Claude 会话生效，正在跑的会话一般要重启才生效。"
        case .codex: "改的是那台机器的 ~/.codex/config.toml，只动下面这几项，文件里别的配置（模型提供方、项目信任等）原样保留；新开的 Codex 会话生效。"
        }
    }
}

struct ClaudeSettingsView: View {
    @Environment(AppModel.self) private var model
    var agent: AgentSettingsKind = .claude
    /// The machine id, "group|id" for another group of hubs (`MachineRef.path`).
    @AppStorage("claudeSettings.machine") private var machineID = ""
    private var ref: MachineRef { MachineRef(path: machineID) }
    @AppStorage("claudeSettings.applyAll") private var applyAll = true
    @State private var settings: [APIClient.ClaudeSetting] = []
    @State private var loading = false
    @State private var error: String?
    @State private var strings: [String: String] = [:]

    var body: some View {
        SettingsForm {
            Section {
                Picker("机器", selection: $machineID) {
                    ForEach(model.machines.filter(\.online), id: \.ref) { m in Text(m.title).tag(m.ref.path) }
                }
                Toggle("同时改所有在线机器", isOn: $applyAll)
            } footer: {
                Text(agent.footer)
            }
            if let error {
                Section { Text(error).foregroundStyle(Theme.danger).font(Theme.Font.caption) }
            }
            #if os(macOS)
            // An empty group draws as a blank gap in a Mac form (while loading, or a machine with nothing to set).
            if !settings.isEmpty { Section { ForEach(settings) { s in row(s) } } }
            #else
            Section {
                ForEach(settings) { s in row(s) }
            }
            #endif
            if agent == .claude, !machineID.isEmpty { ClaudeDirsSection(ref: ref) }
        }
        .themedPage()
        .detailTitle(agent.title)
        .onAppear { if machineID.isEmpty { machineID = model.hubRef(model.hubMachineID).path } } // first visit: the hub's machine
        .overlay { if loading && settings.isEmpty { ProgressView() } }
        .task(id: machineID) { await load() }
        .refreshable { await load() }
    }

    @ViewBuilder private func row(_ s: APIClient.ClaudeSetting) -> some View {
        let value = s.value ?? s.default
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            switch s.type {
            case "bool":
                Toggle(isOn: Binding(get: { value?.bool ?? false }, set: { v in Task { await set(s.key, .bool(v)) } })) {
                    title(s)
                }
            case "enum":
                Picker(selection: Binding(get: { value?.string ?? "" }, set: { v in Task { await set(s.key, .string(v)) } })) {
                    ForEach(s.options ?? [], id: \.self) { Text($0).tag($0) }
                } label: { title(s) }
            default:
                title(s)
                TextField(s.default?.string.flatMap { $0.isEmpty ? nil : $0 } ?? "未设置",
                          text: Binding(get: { strings[s.key] ?? value?.string ?? "" }, set: { strings[s.key] = $0 }))
                    .onSubmit { Task { await set(s.key, .string(strings[s.key] ?? "")) } }
                    .submitLabel(.done)
                    #if os(macOS)
                    .labelsHidden() // a Mac form shows a field's title as its label; here it is the prompt (#1221)
                    #endif
            }
        }
        .padding(.vertical, Theme.Space.xxs)
    }

    private func title(_ s: APIClient.ClaudeSetting) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            HStack(spacing: Theme.Space.sm) {
                Text(s.title)
                if s.value == nil { Text("默认").font(Theme.Font.label).foregroundStyle(.tertiary) }
            }
            Text(s.detail).font(Theme.Font.label).foregroundStyle(.secondary)
        }
    }

    private func load() async {
        if model.machine(ref)?.online != true, let first = model.machines.first(where: \.online) { machineID = first.ref.path }
        let ref = ref
        guard let api = model.api(ref.group) else { return }
        loading = true
        defer { loading = false }
        do {
            settings = agent == .claude ? try await api.claudeSettings(machine: ref.id) : try await api.codexSettings(machine: ref.id)
            error = nil
        } catch {
            self.error = error.shown
        }
    }

    private func set(_ key: String, _ value: JSONValue) async {
        // Every online machine of every group of hubs, each through its own group's hub.
        let targets = applyAll ? model.machines.filter(\.online).map(\.ref) : [ref]
        var failed: [String] = []
        for target in targets {
            guard let api = model.api(target.group) else { continue }
            do {
                let list = agent == .claude ? try await api.setClaudeSetting(machine: target.id, key: key, value: value)
                    : try await api.setCodexSetting(machine: target.id, key: key, value: value)
                if target == ref { settings = list }
            } catch {
                if !error.isCancellation { failed.append("\(model.machine(target)?.title ?? target.id)：\(error.localizedDescription)") }
            }
        }
        if failed.isEmpty {
            Haptics.success()
            model.toast = Toast(targets.count > 1 ? "已应用到 \(targets.count) 台机器" : "已保存", style: .success)
        } else {
            Haptics.error()
            error = failed.joined(separator: "\n")
        }
    }
}

/// A row of 设置's list: the system's label, with what it is set to on the right; in the BeautifulUI style its list
/// row (a tinted tile, the name, the value) at the height the other styles' rows have.
struct SettingsRow: View {
    enum ValueLook { case plain, code, oneLine, digits }
    var title: String
    var symbol: String
    var tint: Color
    var value: String? = nil
    var look: ValueLook = .plain
    /// A brand's own mark (an asset of `Theme.Brand`) in place of the tinted tile, in the BeautifulUI style.
    var brand: String? = nil

    var body: some View {
        if AppStyle.current.usesBeautifulUI {
            BUIListRow(title, value: value.flatMap { $0.isEmpty ? nil : $0 }) {
                if let brand { SettingsBrandMark(asset: brand) } else { BUIIconTile(symbol, tint: tint) }
            }
        } else if let value {
            LabeledContent {
                switch look {
                case .plain: Text(value)
                case .code: Text(value).font(Theme.Font.body.monospaced())
                case .oneLine: Text(value).lineLimit(1)
                case .digits: Text(value).monospacedDigit()
                }
            } label: {
                Label(title, systemImage: symbol)
            }
        } else {
            #if os(macOS)
            // The settings list is a sidebar-style list on the Mac (SplitNavigation), which draws a row that is only a
            // Label as a source list's vibrant label: off its material it came out gray, as if disabled, beside the
            // rows with a value (#1170). Laid out as those rows are, it is drawn as they are.
            LabeledContent { EmptyView() } label: { Label(title, systemImage: symbol) }
            #else
            Label(title, systemImage: symbol)
            #endif
        }
    }
}

/// A brand's mark as a row's leading view: the size of `BUIIconTile`, so the names line up with the other rows'.
private struct SettingsBrandMark: View {
    var asset: String
    @Environment(\.buiTheme) private var theme

    var body: some View {
        BrandMark(asset, side: Theme.Size.iconL * max(1, theme.typeScale * 0.9), scaled: false)
    }
}

extension View {
    /// The rows of a `List` section in the BeautifulUI style: a `BUIListRow` has its own height (its padding and minimum),
    /// so the list adds no inset above and below it on top (77.7 pt rows instead of the 52.3 pt the other styles have).
    /// Apply it to the rows (a `Group` of them), not the `Section`: on the section it takes the header's padding too, and
    /// not on a row inside a `NavigationLink`'s label (it does not reach the list there). Other styles keep the list's insets.
    func bareListRow() -> some View { listRowInsets(.vertical, AppStyle.current.usesBeautifulUI ? 0 : nil) }
}

/// A section's title in 设置: the list's own; in the BeautifulUI style its heading.
struct SettingsHeading: View {
    var text: String
    init(_ text: String) { self.text = text }

    var body: some View {
        if AppStyle.current.usesBeautifulUI { BUISectionHeading(text).textCase(nil) } else { Text(text) }
    }
}

/// 设置's buttons that are rows: the list's tinted button; in the BeautifulUI style a plain row (its tile carries the color).
struct SettingsButtonLook: ViewModifier {
    func body(content: Content) -> some View {
        #if os(macOS)
        // A row like the settings list's other rows on the Mac (#1224 B4), not the sidebar list's small gray capsule.
        content.buttonStyle(.plain)
        #else
        if AppStyle.current.usesBeautifulUI { content.buttonStyle(.plain) } else { content }
        #endif
    }
}
