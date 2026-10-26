import SwiftUI
import VisionKit

struct SettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var confirmUnpair = false
    @State private var showPair = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    HStack(spacing: 14) {
                        AppGlyph(size: 54)
                        VStack(alignment: .leading, spacing: 3) {
                            Text("Jarvis").font(.title3.weight(.bold))
                            Text(model.config?.url.absoluteString ?? "").font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                            HStack(spacing: 5) {
                                Circle().fill(model.live.state == .connected ? Theme.idle : Theme.attention).frame(width: 7, height: 7)
                                Text(model.live.state == .connected ? "已连接 hub" : "未连接").font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                    .padding(.vertical, 4)
                }

                Section {
                    StylePicker()
                        .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
                } header: {
                    Text("界面风格")
                } footer: {
                    Text("整个 App 一起换，包括信息里的飞书页面。")
                }

                Section {
                    NavigationLink { AssistantSettingsView() } label: {
                        LabeledContent {
                            Text(model.assistantConfig?.launcher ?? "").font(.callout.monospaced())
                        } label: {
                            Label("助手", systemImage: "sparkles")
                        }
                    }
                    NavigationLink { ClaudeSettingsView() } label: {
                        Label("Claude Code 设置", systemImage: "slider.horizontal.3")
                    }
                    NavigationLink { MachinesView() } label: {
                        LabeledContent {
                            Text("\(model.machines.filter(\.online).count) / \(model.machines.count) 在线")
                        } label: {
                            Label("机器", systemImage: "macbook.and.iphone")
                        }
                    }
                }

                Section {
                    NavigationLink { PhoneDataView() } label: {
                        Label("手机数据", systemImage: "iphone.gen3")
                    }
                    Button { UIApplication.shared.open(URL(string: UIApplication.openNotificationSettingsURLString)!) } label: {
                        Label { Text("通知").foregroundStyle(Color.primary) } icon: { Image(systemName: "bell.badge") }
                    }
                } footer: {
                    Text("打开的数据，Claude 可以通过 phone_* 工具读取；新建日程、提醒等写操作会先在手机上问你。")
                }

                Section {
                    Button {
                        if let cfg = model.config {
                            WatchSync.shared.push(config: cfg)
                            WatchSync.shared.push(summary: model.watchSummary())
                            Haptics.success()
                            model.toast = Toast("已发送到 Apple Watch，打开手表上的 Jarvis 即可", style: .success)
                        }
                    } label: {
                        Label("同步到 Apple Watch", systemImage: "applewatch").lineLimit(1)
                    }
                } footer: {
                    Text("手表上的 Jarvis 用 iPhone 的配对信息直接连 hub；手表显示「未配对」时点这里，或在手表上点「从 iPhone 同步」。")
                }

                Section("服务器") {
                    Button { showPair = true } label: { Label("重新配对", systemImage: "qrcode.viewfinder") }
                    Button(role: .destructive) { confirmUnpair = true } label: { Label("断开并忘记服务器", systemImage: "xmark.circle") }
                }

                Section("关于") {
                    LabeledContent("版本", value: "\(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "") (\(Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? ""))")
                }
            }
            .themedPage()
            .navigationTitle("设置")
            .navigationDestination(for: String.self) { id in MachineDetailView(machineID: id) }
            .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key) }
            .task { await model.refreshAssistant() }
            .confirmationDialog("断开后需要重新扫码配对。", isPresented: $confirmUnpair, titleVisibility: .visible) {
                Button("断开", role: .destructive) { model.unpair() }
            }
            .sheet(isPresented: $showPair) { PairView() }
        }
    }
}

/// Which launcher (model) new assistant conversations use, and what the assistant can reach.
struct AssistantSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var launchers: [Launcher] = []
    @State private var selection = ""

    /// The machines the hub connects to, by name ("服务器、笔记本…"), from the hub's own list.
    private var machinesLine: String {
        let names = model.machines.map(\.name)
        return names.isEmpty ? "hub 接入的各台电脑（ssh）" : names.joined(separator: "、") + "（ssh）"
    }

    var body: some View {
        let machineID = model.assistantConfig?.machineID ?? model.hubMachineID
        List {
            Section {
                NavigationLink {
                    LauncherPicker(launchers: launchers, selection: $selection)
                } label: {
                    LabeledContent("启动器（模型）") {
                        Text(selection).font(.callout.monospaced())
                    }
                }
                LabeledContent("运行在", value: model.machine(machineID)?.name ?? machineID)
                LabeledContent("工作目录", value: "~/.jarvis/assistant")
            } footer: {
                Text("每段对话是 \(model.machine(machineID)?.name ?? machineID) 上 herdr 里的一个 Claude，改了只影响新对话。")
            }
            Section("能访问") {
                Label(machinesLine, systemImage: "macbook.and.iphone")
                Label("飞书日历、文档、消息、邮件等工作数据", systemImage: "building.2")
                Label("iPhone：通讯录、日历、提醒、位置、健康、照片", systemImage: "iphone")
                Label("你添加的所有邮箱", systemImage: "envelope")
            }
        }
        .navigationTitle("助手")
        .task {
            selection = model.assistantConfig?.launcher ?? "ccc"
            launchers = (try? await model.launchers(machineID)) ?? []
        }
        .onChange(of: selection) { old, new in
            guard !old.isEmpty, old != new else { return }
            Task { await model.setAssistantLauncher(new) }
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
                ForEach(PhoneDataSource.allCases) { s in
                    HStack(spacing: 12) {
                        Image(systemName: s.symbol)
                            .font(.body.weight(.medium))
                            .foregroundStyle(.white)
                            .frame(width: 30, height: 30)
                            .background(Theme.accent.gradient, in: .rect(cornerRadius: 8, style: .continuous))
                        VStack(alignment: .leading, spacing: 2) {
                            Text(s.title)
                            Text(s.detail).font(.caption).foregroundStyle(.secondary)
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
                                        .font(.caption.weight(.semibold))
                                        .foregroundStyle(st.isUsable ? Theme.idle : Theme.accent)
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
                    .padding(.vertical, 2)
                }
            } footer: {
                Text("Jarvis 在前台（或刚切到后台）时，hub 才能调到手机。iOS 不开放短信、备忘录、第三方 App 的数据。")
            }
        }
        .navigationTitle("手机数据")
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
                Image(systemName: "sparkle")
                    .font(.system(size: size * 0.48, weight: .semibold))
                    .foregroundStyle(LinearGradient(colors: [Color(hex: 0xFFB08A), Theme.accent], startPoint: .top, endPoint: .bottom))
            }
            .shadow(color: Theme.accent.opacity(0.25), radius: size * 0.15, y: size * 0.05)
    }
}

struct WelcomeView: View {
    @State private var showPair = false
    @State private var appear = false

    var body: some View {
        VStack(spacing: 0) {
            Spacer()
            AppGlyph(size: 96)
                .scaleEffect(appear ? 1 : 0.8)
                .opacity(appear ? 1 : 0)
            Text("Jarvis")
                .font(.system(size: 40, weight: .bold, design: .rounded))
                .padding(.top, 24)
            Text("把每台电脑上的 Claude 装进口袋")
                .font(.title3)
                .foregroundStyle(.secondary)
                .padding(.top, 6)
            VStack(alignment: .leading, spacing: 18) {
                feature("bubble.left.and.text.bubble.right", "像聊天一样驱动 Claude", "你的每台电脑，herdr 里的同一个会话")
                feature("bolt.horizontal", "实时看它在做什么", "思考、工具调用、终端画面，流式更新")
                feature("hand.tap", "在手机上拍板", "回答提问、审批计划、授权工具，手表也行")
                feature("iphone.gen3", "带上你的手机和邮箱", "日历、提醒、通讯录、健康、照片、Gmail")
            }
            .padding(.top, 40)
            .padding(.horizontal, 32)
            Spacer()
            Button { showPair = true } label: {
                Label("连接 hub", systemImage: "qrcode.viewfinder").font(.headline).frame(maxWidth: .infinity, minHeight: 32)
            }
            .themedButton(prominent: true)
            .controlSize(.large)
            .padding(.horizontal, 24)
            Text("在 hub 所在的机器上运行 `jarvisd hub pair` 显示二维码")
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.top, 10)
                .padding(.bottom, 12)
        }
        .sheet(isPresented: $showPair) { PairView() }
        .onAppear { withAnimation(.spring(duration: 0.8, bounce: 0.35)) { appear = true } }
    }

    private func feature(_ symbol: String, _ title: String, _ detail: String) -> some View {
        HStack(alignment: .top, spacing: 14) {
            Image(systemName: symbol)
                .font(.title3)
                .foregroundStyle(Theme.accent)
                .frame(width: 30)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.headline)
                Text(detail).font(.subheadline).foregroundStyle(.secondary)
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
                        .frame(height: 280)
                        .clipShape(.rect(cornerRadius: 16, style: .continuous))
                        .listRowInsets(EdgeInsets())
                    } footer: {
                        Text("扫描 `jarvisd hub pair` 打印的二维码。")
                    }
                }
                Section("手动输入") {
                    TextField("服务器地址", text: $url).keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                    SecureField("Token", text: $token)
                }
                if let error { Section { Text(error).foregroundStyle(Theme.danger).font(.footnote) } }
            }
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
                Section { Text(error).foregroundStyle(Theme.danger).font(.footnote) }
            }
            Section {
                ForEach(settings) { s in row(s) }
            }
        }
        .navigationTitle("Claude Code 设置")
        .onAppear { if machineID.isEmpty { machineID = model.hubMachineID } } // first visit: the hub's machine
        .overlay { if loading && settings.isEmpty { ProgressView() } }
        .task(id: machineID) { await load() }
        .refreshable { await load() }
    }

    @ViewBuilder private func row(_ s: APIClient.ClaudeSetting) -> some View {
        let value = s.value ?? s.default
        VStack(alignment: .leading, spacing: 4) {
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
        .padding(.vertical, 2)
    }

    private func title(_ s: APIClient.ClaudeSetting) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                Text(s.title)
                if s.value == nil { Text("默认").font(.caption2).foregroundStyle(.tertiary) }
            }
            Text(s.detail).font(.caption).foregroundStyle(.secondary)
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

/// Five looks side by side, each drawn as a tiny screen in its own colors; tap to switch the whole app.
struct StylePicker: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 12) {
                ForEach(AppStyle.allCases) { s in
                    Button {
                        guard s != model.style else { return }
                        Haptics.success()
                        withAnimation(.snappy) { model.style = s }
                    } label: {
                        StyleCard(style: s, selected: s == model.style)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(s.title)，\(s.subtitle)")
                    .accessibilityAddTraits(s == model.style ? .isSelected : [])
                }
            }
            .padding(.horizontal, 16)
        }
    }
}

private struct StyleCard: View {
    var style: AppStyle
    var selected: Bool

    var body: some View {
        let page = Theme.pageBackground(style), card = Theme.card(style), accent = Theme.accent(style)
        let bubble = Theme.userBubble(style), corner = style.corner * 0.45
        VStack(spacing: 8) {
            ZStack {
                RoundedRectangle(cornerRadius: 14, style: .continuous).fill(style.usesGlass ? AnyShapeStyle(accent.opacity(0.10)) : AnyShapeStyle(page))
                VStack(alignment: .leading, spacing: 6) {
                    RoundedRectangle(cornerRadius: corner, style: .continuous).fill(card)
                        .frame(height: 16)
                        .shadow(color: style == .island || style == .clay ? .black.opacity(0.12) : .clear, radius: 4, y: 2)
                    HStack { Spacer(); Capsule().fill(bubble).frame(width: 40, height: 12) }
                    Capsule().fill(Color.secondary.opacity(0.25)).frame(width: 46, height: 8)
                    HStack(spacing: 4) {
                        Circle().fill(accent).frame(width: 10, height: 10)
                        Capsule().fill(accent.opacity(0.35)).frame(height: 8)
                    }
                }
                .padding(10)
            }
            .frame(width: 92, height: 120)
            .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).stroke(selected ? accent : Color.primary.opacity(0.1), lineWidth: selected ? 2.5 : 1))
            VStack(spacing: 1) {
                HStack(spacing: 3) {
                    if selected { Image(systemName: "checkmark.circle.fill").foregroundStyle(accent) }
                    Text(style.title).font(.footnote.weight(.semibold)).lineLimit(1)
                }
                Text(style.subtitle).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .frame(width: 96)
    }
}

