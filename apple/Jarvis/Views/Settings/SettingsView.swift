import SwiftUI
import VisionKit

struct SettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var confirmUnpair = false
    @State private var showPair = false
    @State private var debugOpened = false
    @State private var storage: Int64?
    @AppStorage(TranslationPrefs.enabledKey) private var translating = false
    @AppStorage(TranslationPrefs.targetKey) private var translateTarget = TranslationPrefs.defaultTarget

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
                            Text("Jarvis").font(Theme.Font.title)
                            if model.isDemo {
                                Text("体验模式 · 数据都是编造的").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                            } else {
                                Text(model.config?.url.absoluteString ?? "").font(Theme.Font.code).foregroundStyle(.secondary).lineLimit(1)
                                HStack(spacing: Theme.Space.sm) {
                                    Circle().fill(model.live.state == .connected ? Theme.positive : Theme.caution).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                                    Text(model.live.state == .connected ? "已连接 hub" : model.connection.phase == .online ? "未连接" : model.connection.phase.title).font(Theme.Font.label).foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                    .padding(.vertical, Theme.Space.xs)
                }

                if model.isDemo {
                    DemoSection(pair: { showPair = true })
                }

                Section {
                    NavigationLink(value: SettingsPage.appearance) {
                        LabeledContent {
                            Text("\(model.persona.name) · \(model.style.title)").lineLimit(1)
                        } label: {
                            Label("外观", systemImage: Theme.Icon.appearance)
                        }
                    }
                    .accessibilityIdentifier("settings-appearance")
                }

                Section {
                    NavigationLink(value: SettingsPage.assistant) {
                        LabeledContent {
                            Text(model.assistantConfig?.launcher ?? "").font(Theme.Font.body.monospaced())
                        } label: {
                            Label("助手", systemImage: Theme.Icon.ai)
                        }
                    }
                    NavigationLink(value: ModelLaunchersRoute()) {
                        Label("模型", systemImage: Theme.Icon.cpu)
                    }
                    NavigationLink(value: SettingsPage.claude) {
                        Label("Claude Code 设置", systemImage: Theme.Icon.options)
                    }
                    NavigationLink(value: MachineListRoute()) {
                        LabeledContent {
                            Text("\(model.machines.filter(\.online).count) / \(model.machines.count) 在线")
                        } label: {
                            Label("机器", systemImage: Theme.Icon.machines)
                        }
                    }
                    NavigationLink(value: SettingsPage.devices) {
                        Label("设备", systemImage: Theme.Icon.credentials)
                    }
                }

                ConnectorsSection()

                Section {
                    NavigationLink(value: SettingsPage.notifications) {
                        Label("通知", systemImage: Theme.Icon.notifications)
                    }
                }

                if !model.isDemo { watchSection }

                Section {
                    NavigationLink(value: SettingsPage.translation) {
                        LabeledContent {
                            Text(translationSummary).lineLimit(1)
                        } label: {
                            Label("翻译", systemImage: Theme.Icon.translate)
                        }
                    }
                }

                Section {
                    NavigationLink(value: SettingsPage.storage) {
                        LabeledContent {
                            Text(storage?.storageText ?? "").monospacedDigit()
                        } label: {
                            Label("存储空间", systemImage: "internaldrive")
                        }
                    }
                }

                if !model.isDemo {
                    Section("服务器") {
                        Button { showPair = true } label: { Label("重新配对", systemImage: "qrcode.viewfinder") }
                        Button(role: .destructive) { confirmUnpair = true } label: { Label("断开并忘记服务器", systemImage: Theme.Icon.end) }
                    }
                }

                Section("关于") {
                    LabeledContent("版本", value: "\(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "") (\(Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? ""))")
                    // The commit this build was made from (stamped at build time, `project.yml`): which one is on the phone.
                    LabeledContent("构建") {
                        Text(Bundle.main.object(forInfoDictionaryKey: "JarvisCommit") as? String ?? "未知")
                            .font(Theme.Font.code)
                            .textSelection(.enabled)
                    }
                }
            }
            .themedPage()
            .navigationTitle("设置")
            .task { await model.refreshAssistant() }
            // Measured each time the page shows and whenever a page closes (back from 存储空间 after clearing; in the
            // split the list never leaves the screen).
            .onAppear(perform: measureStorage)
            .onChange(of: model.settingsPath) { measureStorage() }
            #if DEBUG
            // With `-tab settings`: `-openAssistantPage assistant|routines|routine|newRoutine|followups|memory|memoryFile|
            // memoryEdit|about`, `-openStorage YES` (存储空间), `-openNotificationSettings YES` (通知), `-openMachines YES` (机器;
            // with `-openMachineDetail <id>` that machine's page), `-openDevices YES` (设备).
            .onAppear(perform: openDebugPage)
            #endif
            .confirmationDialog("断开后需要重新扫码配对。", isPresented: $confirmUnpair, titleVisibility: .visible) {
                Button("断开", role: .destructive) { model.unpair() }
            }
            .sheet(isPresented: $showPair) { PairView() }
        } empty: {
            SplitPlaceholder(symbol: Theme.Icon.settings, title: "设置", message: "左边选一项。")
        }
    }

    private func measureStorage() {
        Task { storage = await Task.detached(priority: .utility) { StorageUsage.scan().values.reduce(0, +) }.value }
    }

    #if DEBUG
    private func openDebugPage() {
        guard !debugOpened else { return }
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
        else if d.bool(forKey: "openConnectorMail") || d.bool(forKey: "addMail") { p.append(SettingsPage.mail) }
        else if d.bool(forKey: "openConnectorFeishu") { p.append(SettingsPage.feishu) }
        else if d.bool(forKey: "openConnectorDevice") { p.append(SettingsPage.phoneData) }
        else if d.bool(forKey: "openTranslationSettings") { p.append(SettingsPage.translation) }
        else if d.bool(forKey: "openAppearance") { p.append(SettingsPage.appearance) }
        else if let n = d.string(forKey: "openLauncherPage") { p.append(ModelLaunchersRoute()); p.append(ModelLauncherRoute(name: n)) }
        else if d.bool(forKey: "openModelLaunchers") || d.string(forKey: "openModelLauncher") != nil { // 设置 › 模型 (› one)
            p.append(ModelLaunchersRoute())
            if let n = d.string(forKey: "openModelLauncher") { p.append(ModelLauncherRoute(name: n)) }
        }
        guard !p.isEmpty else { return }
        debugOpened = true
        model.settingsPath = p
    }
    #endif

    private var watchSection: some View {
        Section {
            Button {
                if let cfg = model.config {
                    WatchSync.shared.push(config: cfg)
                    WatchSync.shared.push(summary: model.watchSummary())
                    Haptics.success()
                    model.toast = Toast("已发送到 Apple Watch，打开手表上的 Jarvis 即可", style: .success)
                }
            } label: {
                Label("同步到 Apple Watch", systemImage: Theme.Icon.watch).lineLimit(1)
            }
        } footer: {
            Text("手表上的 Jarvis 用 iPhone 的配对信息直接连 hub；手表显示「未配对」时点这里，或在手表上点「从 iPhone 同步」。")
        }
    }
}

/// The pages of 设置 that are not routes of their own elsewhere.
enum SettingsPage: Hashable {
    case appearance, assistant, claude, devices, phoneData, mail, feishu, notifications, storage, translation

}

/// The pages the settings list opens, in either layout (`SplitNavigation`). MachinesView pushes its cards by value
/// (MachineDetailRoute), and a machine's page its agents (SessionKey).
private struct SettingsDestinations: ViewModifier {
    func body(content: Content) -> some View {
        content
            .navigationDestination(for: SettingsPage.self) { page(for: $0).splitDetailPage() }
            .navigationDestination(for: MachineListRoute.self) { _ in MachinesView().splitDetailPage() }
            .navigationDestination(for: ModelLaunchersRoute.self) { _ in ModelLaunchersView().splitDetailPage() }
            .navigationDestination(for: ModelLauncherRoute.self) { r in ModelLauncherDetailView(name: r.name).splitDetailPage() }
            .navigationDestination(for: MachineDetailRoute.self) { r in MachineDetailView(machineID: r.id).splitDetailPage() }
            .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key).splitDetailPage() }
    }

    @ViewBuilder private func page(for page: SettingsPage) -> some View {
        switch page {
        case .appearance: AppearanceSettingsView()
        case .assistant: AssistantProfileView()
        case .claude: ClaudeSettingsView()
        case .devices: DevicesView()
        case .phoneData: PhoneDataView()
        case .mail: MailAccountsView(inSheet: false)
        case .feishu: FeishuConnectorView()
        case .notifications: NotificationSettingsView()
        case .storage: StorageView()
        case .translation: TranslationSettingsView()
        }
    }

}

/// 设置 in 体验模式: what it is, and the two ways out.
private struct DemoSection: View {
    @Environment(AppModel.self) private var model
    var pair: () -> Void

    var body: some View {
        Section {
            Button(action: pair) { Label("连接我的 hub", systemImage: "qrcode.viewfinder").lineLimit(1) }
            Button(role: .destructive) { model.unpair() } label: {
                Label("退出体验模式", systemImage: "rectangle.portrait.and.arrow.right").lineLimit(1)
            }
            .accessibilityIdentifier("settings.leaveDemo")
        } header: {
            Text("体验模式")
        } footer: {
            Text("现在看到的机器、Agent、对话和收件箱都是演示数据，不连接任何服务器，发消息、审批等操作也不会真的发出去。连接自己的 hub 后自动退出体验模式。")
        }
    }
}

struct PhoneDataView: View {
    @Environment(AppModel.self) private var model
    @State private var statuses: [PhoneDataSource: PhoneAuthStatus] = [:]
    @State private var enabled: [PhoneDataSource: Bool] = [:]

    var body: some View {
        List {
            Section {
                ForEach(PhoneDataSource.onThisDevice) { s in
                    HStack(spacing: Theme.Space.m) {
                        Image(systemName: s.symbol)
                            .font(Theme.Font.heading)
                            .foregroundStyle(.white)
                            .scaledFrame(width: Theme.Size.wellS, height: Theme.Size.wellS)
                            .background(Theme.accent.gradient, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text(s.title)
                            Text(s.detail).font(Theme.Font.label).foregroundStyle(.secondary)
                            if let st = statuses[s], st != .notNeeded {
                                Button {
                                    Task {
                                        if st == .denied {
                                            await UIApplication.shared.open(URL(string: UIApplication.openSettingsURLString)!)
                                        } else {
                                            statuses[s] = await PhoneAuthorization.request(s)
                                        }
                                    }
                                } label: {
                                    Text(st == .granted || st == .limited ? st.title : st == .denied ? "已拒绝 · 去设置" : "点此授权")
                                        .font(Theme.Font.labelStrong)
                                        .foregroundStyle(st.isUsable ? Theme.positive : Theme.accent)
                                }
                                .buttonStyle(.plain)
                                .disabled(st == .granted)
                            }
                        }
                        Spacer()
                        Toggle("", isOn: Binding(get: { enabled[s] ?? s.isEnabled }, set: { v in
                            s.isEnabled = v
                            enabled[s] = v
                            model.registerPhone()
                        }))
                        .labelsHidden()
                    }
                    .padding(.vertical, Theme.Space.xxs)
                }
            } footer: {
                #if os(macOS)
                Text("打开的数据，Claude 可以通过 phone_* 工具读取；新建日程、提醒等写操作会先在这台 Mac 上问你。Jarvis 开着（窗口关了也算）时，hub 才能调到这里。健康数据 Mac 上没有。")
                #else
                Text("打开的数据，Claude 可以通过 phone_* 工具读取；新建日程、提醒等写操作会先在手机上问你。Jarvis 在前台（或刚切到后台）时，hub 才能调到手机。iOS 不开放短信、备忘录、第三方 App 的数据。")
                #endif
            }
        }
        .themedPage()
        .navigationTitle(ConnectorText.device)
        .navigationBarTitleDisplayMode(.inline) // like the other connectors' pages
        .task {
            for s in PhoneDataSource.allCases { statuses[s] = PhoneAuthorization.status(s) }
        }
    }
}

struct AppGlyph: View {
    var size: CGFloat
    var body: some View {
        RoundedRectangle(cornerRadius: size * 0.27, style: .continuous)
            .fill(LinearGradient(colors: [Color(hex: 0x2A1810), Color(hex: 0x120B08)], startPoint: .top, endPoint: .bottom))
            .frame(width: size, height: size)
            .overlay {
                Image(systemName: Theme.Icon.jarvis)
                    .font(.system(size: size * 0.48, weight: .semibold))
                    .foregroundStyle(LinearGradient(colors: [Color(hex: 0xFFB08A), Theme.brand], startPoint: .top, endPoint: .bottom))
            }
            .shadow(color: Theme.brand.opacity(0.25), radius: size * 0.15, y: size * 0.05)
    }
}

struct WelcomeView: View {
    @Environment(AppModel.self) private var model
    @State private var showPair = false
    @State private var appear = false
    #if os(macOS)
    private static let tagline = "把每台电脑上的 Claude 收进一个窗口"
    #else
    private static let tagline = "把每台电脑上的 Claude 装进口袋"
    #endif

    var body: some View {
        VStack(spacing: 0) {
            Spacer()
            AppGlyph(size: 96)
                .scaleEffect(appear ? 1 : 0.8)
                .opacity(appear ? 1 : 0)
            Text("Jarvis")
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
                feature("hand.tap", "在一个窗口里拍板", "回答提问、审批计划、授权工具，不用切到终端")
                feature(Theme.Icon.laptop, "带上这台 Mac 和邮箱", "日历、提醒、通讯录、照片、Gmail")
                #else
                feature("hand.tap", "在手机上拍板", "回答提问、审批计划、授权工具，手表也行")
                feature("iphone.gen3", "带上你的手机和邮箱", "日历、提醒、通讯录、健康、照片、Gmail")
                #endif
            }
            .padding(.top, 40)
            .padding(.horizontal, Theme.Space.xxl)
            Spacer()
            Button { showPair = true } label: {
                Label("连接 hub", systemImage: "qrcode.viewfinder").font(Theme.Font.heading).scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.well)
            }
            .themedButton(prominent: true)
            .controlSize(.large)
            .padding(.horizontal, Theme.Space.xl)
            .accessibilityIdentifier("welcome.pair")
            Text("在 hub 所在的机器上运行 `jarvisd hub pair` 显示二维码")
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
                        .scaledFrame(height: Theme.Size.box280)
                        .clipShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous))
                        .listRowInsets(EdgeInsets())
                    } footer: {
                        #if os(iOS)
                        Text("扫描 `jarvisd hub pair` 打印的二维码。")
                        #else
                        Text("拷贝 `jarvisd hub pair` 打印的配对链接（jarvis://pair?…），再点「粘贴配对链接」。")
                        #endif
                    }
                }
                Section("手动输入") {
                    TextField("服务器地址", text: $url).keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                    SecureField("Token", text: $token)
                }
                if let error { Section { Text(error).foregroundStyle(Theme.danger).font(Theme.Font.caption) } }
            }
            .themedPage()
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
            try await model.pair(ServerConfig(url: u, token: token.trimmingCharacters(in: .whitespaces)))
            Haptics.success()
            dismiss()
        } catch {
            if !error.isCancellation { self.error = "连接失败：\(error.localizedDescription)。确认手机能访问这个地址（比如在同一个网络或 VPN 上）。" }
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
struct ClaudeSettingsView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("claudeSettings.machine") private var machineID = ""
    @AppStorage("claudeSettings.applyAll") private var applyAll = true
    @State private var settings: [APIClient.ClaudeSetting] = []
    @State private var loading = false
    @State private var error: String?
    @State private var strings: [String: String] = [:]

    var body: some View {
        List {
            Section {
                Picker("机器", selection: $machineID) {
                    ForEach(model.machines.filter(\.online)) { m in Text(m.name).tag(m.id) }
                }
                Toggle("同时改所有在线机器", isOn: $applyAll)
            } footer: {
                Text("改的是那台机器的 ~/.claude/settings.json，只动下面这几项；新开的 Claude 会话生效，正在跑的会话一般要重启才生效。")
            }
            if let error {
                Section { Text(error).foregroundStyle(Theme.danger).font(Theme.Font.caption) }
            }
            Section {
                ForEach(settings) { s in row(s) }
            }
        }
        .themedPage()
        .navigationTitle("Claude Code 设置")
        .onAppear { if machineID.isEmpty { machineID = model.hubMachineID } } // first visit: the hub's machine
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
        guard let api = model.api else { return }
        if model.machine(machineID)?.online != true, let first = model.machines.first(where: \.online) { machineID = first.id }
        loading = true
        defer { loading = false }
        do {
            settings = try await api.claudeSettings(machine: machineID)
            error = nil
        } catch {
            self.error = error.shown
        }
    }

    private func set(_ key: String, _ value: JSONValue) async {
        guard let api = model.api else { return }
        let targets = applyAll ? model.machines.filter(\.online).map(\.id) : [machineID]
        var failed: [String] = []
        for id in targets {
            do {
                let list = try await api.setClaudeSetting(machine: id, key: key, value: value)
                if id == machineID { settings = list }
            } catch {
                if !error.isCancellation { failed.append("\(model.machine(id)?.name ?? id)：\(error.localizedDescription)") }
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

/// The five looks in a grid, each drawn as a tiny screen in its own colors; tap to switch the whole app. A grid, not a
/// row: side by side they were 560 pt wide and the last two were off a 402 pt screen with nothing saying so.
struct StylePicker: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dynamicTypeSize) private var typeSize

    /// Three previews a row; two at accessibility sizes, where three cut every name.
    private var columns: [GridItem] {
        Array(repeating: GridItem(.flexible(), spacing: 12, alignment: .top), count: typeSize.isAccessibilitySize ? 2 : 3)
    }

    var body: some View {
        LazyVGrid(columns: columns, alignment: .leading, spacing: 14) {
            ForEach(AppStyle.allCases) { s in
                Button {
                    guard s != model.style else { return }
                    Haptics.success()
                    withAnimation(Theme.Motion.standard) { model.style = s }
                } label: {
                    StyleCard(style: s, selected: s == model.style)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(s.title)，\(s.subtitle)")
                .accessibilityAddTraits(s == model.style ? .isSelected : [])
            }
        }
        .padding(.horizontal, Theme.Space.l)
    }
}

private struct StyleCard: View {
    var style: AppStyle
    var selected: Bool

    var body: some View {
        let page = Theme.pageBackground(style), card = Theme.card(style), accent = Theme.accent(style)
        let bubble = Theme.userBubble(style), corner = style.corner * 0.45
        VStack(spacing: Theme.Space.s) {
            ZStack {
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).fill(style.usesGlass ? AnyShapeStyle(accent.opacity(0.10)) : AnyShapeStyle(page))
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    RoundedRectangle(cornerRadius: corner, style: .continuous).fill(card)
                        .scaledFrame(height: Theme.Size.markM)
                        .shadow(color: [.puffy, .floating].contains(style.spec.surface) ? .black.opacity(0.12) : .clear, radius: 4, y: 2)
                    HStack { Spacer(); Capsule().fill(bubble).scaledFrame(width: Theme.Size.avatar, height: Theme.Size.markXS) }
                    Capsule().fill(Color.secondary.opacity(0.25)).scaledFrame(width: Theme.Size.avatarL, height: Theme.Size.dot)
                    HStack(spacing: Theme.Space.xs) {
                        Circle().fill(accent).scaledFrame(width: Theme.Size.dotXL, height: Theme.Size.dotXL)
                        Capsule().fill(accent.opacity(0.35)).scaledFrame(height: Theme.Size.dot)
                    }
                }
                .padding(Theme.Space.m)
            }
            .frame(maxWidth: .infinity)
            .scaledFrame(height: Theme.Size.box120)
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(selected ? accent : Color.primary.opacity(0.1), lineWidth: selected ? 2.5 : 1))
            VStack(spacing: Theme.Space.xxs) {
                HStack(spacing: Theme.Space.xs) {
                    if selected { Image(systemName: Theme.Icon.successFilled).foregroundStyle(accent) }
                    Text(style.title).font(Theme.Font.captionStrong).lineLimit(1)
                }
                Text(style.subtitle).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity)
    }
}

