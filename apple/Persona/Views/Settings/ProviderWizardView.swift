import SwiftUI

/// Where the provider sheet starts.
enum ProviderWizardStep: String, Hashable, Identifiable {
    /// 选供应商 (adding).
    case pick
    /// 连接 (editing starts here).
    case connect
    /// 勾选模型.
    case models
    /// 从 ~/.zshrc 导入.
    case importZshrc = "import"

    var id: Self { self }
}

/// 设置 › 模型供应商's sheet to add or change a provider (#2455, after cindy): three steps in one form, the way back in
/// the bar's leading button (a Mac sheet does not push, #1170), and 从 ~/.zshrc 导入 as a step of its own. 其他接入方式
/// (直接 API, a plugin's provider) is the form it had before (`LauncherEditor`).
struct ProviderWizard: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var saved: () async -> Void
    @State private var wizard: ProviderWizardModel
    @State private var imports: LauncherImportModel
    @State private var step: ProviderWizardStep
    private let start: ProviderWizardStep
    /// 其他接入方式: the older form in this sheet.
    @State private var other = false
    /// The model whose context window and thinking levels are open under its row.
    @State private var expanded: String?
    @State private var newModel = ""
    @State private var showAdvanced = false

    init(existing: LauncherView? = nil, start: ProviderWizardStep? = nil, saved: @escaping () async -> Void = {},
         wizard: ProviderWizardModel? = nil, imports: LauncherImportModel? = nil) {
        self.saved = saved
        let first = start ?? (existing == nil ? .pick : .connect)
        self.start = first
        _step = State(initialValue: first)
        _wizard = State(initialValue: wizard ?? ProviderWizardModel(existing: existing))
        _imports = State(initialValue: imports ?? LauncherImportModel())
    }

    var body: some View {
        if other {
            LauncherEditor(existing: nil, saved: saved)
        } else {
            NavigationStack {
                Form {
                    switch step {
                    case .pick: pickSections
                    case .connect: connectSections
                    case .models: modelSections
                    case .importZshrc: LauncherImportSections(imports: imports, machines: importMachines)
                    }
                }
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbar }
                // 取消 on the first step (a Mac asks first once something is typed); 上一步 after it.
                .sheetCancel(unsaved: !wizard.name.isEmpty || !wizard.apiKey.isEmpty, when: step == start) { dismiss() }
                .animation(Theme.Motion.standard, value: step)
                #if DEBUG
                .task { await debugFill() }
                #endif
            }
            #if os(macOS)
            .macSheet()
            #endif
        }
    }

    private var title: String {
        switch step {
        case .importZshrc: String(localized: "从 ~/.zshrc 导入")
        default: wizard.isNew ? String(localized: "新增供应商") : String(localized: "编辑 \(wizard.existing?.name ?? "")")
        }
    }

    /// 「第 2 步，共 3 步 · 连接」 over each step of the wizard (not the import).
    private func stepHeader(_ n: Int, _ what: String) -> some View {
        Text(wizard.isNew ? String(localized: "第 \(n) 步，共 3 步 · \(what)") : what)
            .accessibilityIdentifier("wizard.step")
    }

    // MARK: Bar

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        if step != start {
            ToolbarItem(placement: .cancellationAction) {
                Button("上一步") { back() }.lineLimit(1).accessibilityIdentifier("wizard.back")
            }
        }
        ToolbarItem(placement: .confirmationAction) {
            switch step {
            case .pick: EmptyView()
            case .connect:
                Button("下一步") { Task { await toModels() } }.lineLimit(1)
                    .disabled(!wizard.connectionReady || wizard.discovering)
                    .accessibilityIdentifier("wizard.next")
            case .models:
                Button { Task { await save() } } label: {
                    if wizard.saving { ProgressView() } else { Text("保存").lineLimit(1) }
                }
                .disabled(!wizard.canSave)
                .accessibilityIdentifier("wizard.save")
            case .importZshrc:
                switch imports.phase {
                case .preview, .importing:
                    Button("导入") { Task { await runImport() } }.lineLimit(1)
                        .disabled(!imports.canImport).accessibilityIdentifier("import.confirm")
                case .done:
                    Button("完成") { dismiss() }.lineLimit(1).accessibilityIdentifier("import.done")
                default: EmptyView()
                }
            }
        }
    }

    private func back() {
        switch step {
        case .models: step = .connect
        case .importZshrc where imports.phase == .preview: imports.restart()
        default: step = .pick
        }
    }

    /// 下一步 from 连接: a new provider reads the gateway's models first (once), so the next step has them.
    private func toModels() async {
        if !wizard.discoveredOnce && wizard.choices.isEmpty { await wizard.discover() }
        step = .models
    }

    private func save() async {
        if await finished(wizard.save()) { dismiss() }
    }

    private func runImport() async {
        await imports.runImport()
        _ = await finished(imports.phase == .done)
    }

    /// After a save or an import: the lists read the launchers again and the page that opened this hears of it.
    private func finished(_ ok: Bool) async -> Bool {
        guard ok else { Haptics.error(); return false }
        model.launchersChanged()
        Haptics.success()
        await saved()
        return true
    }

    // MARK: 1 选供应商

    @ViewBuilder private var pickSections: some View {
        Section {
            pickRow(title: String(localized: "Anthropic 官方"), detail: String(localized: "api.anthropic.com，用 Anthropic Console 的 API Key"), id: "wizard.kind.anthropic") {
                BrandMark(Theme.Brand.claude, side: Theme.Size.icon)
            } action: {
                wizard.kind = .anthropic
                step = .connect
            }
            pickRow(title: String(localized: "自定义网关"), detail: String(localized: "兼容 Anthropic 接口的地址，比如各家 Coding Plan、公司的代理"), id: "wizard.kind.custom") {
                Image(systemName: Theme.Icon.server).foregroundStyle(Theme.accent)
            } action: {
                wizard.kind = .custom
                step = .connect
            }
        } header: { stepHeader(1, String(localized: "选供应商")) } footer: {
            Text("供应商在各台电脑上以 Claude Code 运行，主对话和 Agents 都可以用；具体用哪个在「模型配置」里选。")
        }
        Section {
            pickRow(title: String(localized: "从 ~/.zshrc 导入"), detail: String(localized: "把一台电脑上调用 claude 的 alias 变成供应商"), id: "wizard.kind.import") {
                Image(systemName: Theme.Icon.terminal).foregroundStyle(Theme.accent)
            } action: { step = .importZshrc }
            pickRow(title: String(localized: "其他接入方式"), detail: String(localized: "直接 API（Messages / Responses）、扩展提供的供应商"), id: "wizard.kind.other") {
                Image(systemName: Theme.Icon.options).foregroundStyle(.secondary)
            } action: { other = true }
        }
    }

    private func pickRow(title: String, detail: String, id: String, @ViewBuilder icon: () -> some View,
                         action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.m) {
                icon().scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(title).font(Theme.Font.heading).foregroundStyle(.primary).lineLimit(1)
                    Text(detail).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                }
                Spacer(minLength: Theme.Space.s)
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
            }
            .padding(.vertical, Theme.Space.xxs)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(id)
    }

    // MARK: 2 连接

    @ViewBuilder private var connectSections: some View {
        @Bindable var wizard = wizard
        Section {
            TextField("名字，比如 jv_glm", text: $wizard.name)
                .font(Theme.Font.body.monospaced())
                .textInputAutocapitalization(.never).autocorrectionDisabled()
                .disabled(!wizard.isNew)
                .accessibilityIdentifier("wizard.name")
        } header: { stepHeader(2, String(localized: "连接")) } footer: {
            Text(wizard.nameProblem ?? String(localized: "字母、数字、_ 和 -；在各处按这个名字选择。"))
                .foregroundStyle(wizard.nameProblem == nil ? Color.secondary : Theme.danger)
        }
        Section {
            if wizard.kind == .anthropic {
                LabeledContent("地址", value: "api.anthropic.com")
            } else {
                TextField("https://…", text: $wizard.baseURL)
                    .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                    .accessibilityIdentifier("wizard.url")
                Picker("鉴权方式", selection: $wizard.auth) {
                    Text("x-api-key").tag("api_key")
                    Text("Bearer").tag("bearer")
                }
                .lineLimit(1)
            }
            SecretField(wizard.hasStoredKey ? String(localized: "API Key（不改就留空）") : "API Key", filled: String(localized: "已填写"), text: $wizard.apiKey,
                        identifier: "wizard.key")
        } header: { Text("网关") } footer: {
            Text(wizard.kind == .anthropic ? "Key 加密存在 hub，启动时才交给那台电脑，不会返回到客户端。"
                 : "网关只认 Authorization: Bearer（ANTHROPIC_AUTH_TOKEN）时选 Bearer。Key 加密存在 hub，不会返回到客户端。")
        }
        Section {
            Button { Task { await wizard.runTest() } } label: {
                HStack {
                    Label("测试连接", systemImage: Theme.Icon.tryOut).lineLimit(1)
                    if wizard.testing { Spacer(); ProgressView() }
                }
            }
            .disabled(!wizard.canTest)
            .accessibilityIdentifier("wizard.test")
            if let t = wizard.test { ProbeResultRow(result: t) }
            Button { Task { await wizard.discover() } } label: {
                HStack {
                    Label("获取模型列表", systemImage: Theme.Icon.reload).lineLimit(1)
                    Spacer()
                    if wizard.discovering { ProgressView() } else if wizard.discoveredOnce {
                        Text("\(wizard.discovered.count) 个").font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
            }
            .disabled(!wizard.connectionReady || wizard.discovering)
            .accessibilityIdentifier("wizard.discover")
            if let e = wizard.discoverError {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).lineLimit(4)
            }
        } header: { Text("检查") } footer: {
            Text("测试连接会真的发一个很小的请求（\(wizard.testModel.isEmpty ? String(localized: "先获取模型列表") : wizard.testModel)），可能产生少量费用。")
        }
    }

    // MARK: 3 勾选模型

    @ViewBuilder private var modelSections: some View {
        @Bindable var wizard = wizard
        Section {
            if wizard.choices.isEmpty {
                Text(wizard.discoverError.map { String(localized: "模型列表没读到：\($0)。可以在下面手动添加。") } ?? String(localized: "网关没有给出模型列表，在下面手动添加。"))
                    .font(Theme.Font.label).foregroundStyle(.secondary)
            }
            ForEach(wizard.choices) { m in modelRow(m) }
            HStack(spacing: Theme.Space.s) {
                TextField("手动添加模型 ID", text: $newModel)
                    .font(Theme.Font.body.monospaced())
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                    .onSubmit { addModel() }
                    .accessibilityIdentifier("wizard.newModel")
                Button("添加") { addModel() }.lineLimit(1).buttonStyle(.borderless)
                    .disabled(newModel.trimmingCharacters(in: .whitespaces).isEmpty)
            }
        } header: { stepHeader(3, String(localized: "勾选模型")) } footer: {
            Text("勾选这个网关要用的模型。上下文窗口超过 200K 的会以「[1m]」运行；点 ⓘ 改上下文窗口和思考档位。")
        }
        if !wizard.models.isEmpty {
            Section {
                Picker("默认模型", selection: $wizard.defaultModel) {
                    ForEach(wizard.models) { Text($0.title).tag($0.id) }
                }
                .lineLimit(1)
                .accessibilityIdentifier("wizard.default")
            } footer: { Text("没有单独指定模型时使用它；各用途的选择在「模型配置」里。") }
        }
        Section {
            DisclosureGroup(isExpanded: $showAdvanced) { advancedFields } label: { Text("高级").lineLimit(1) }
                .accessibilityIdentifier("wizard.advanced")
        }
        if let e = wizard.error {
            Section { LoadProblem(text: e) }
        }
    }

    private func addModel() {
        if wizard.addModel(newModel) { newModel = "" }
    }

    @ViewBuilder private func modelRow(_ m: ProviderModel) -> some View {
        let picked = wizard.isPicked(m.id)
        HStack(spacing: Theme.Space.m) {
            CheckRow(on: picked, title: m.title, id: "wizard.model.\(m.id)") {
                wizard.toggle(m)
                Haptics.tap()
            } content: {
                HStack(spacing: Theme.Space.sm) {
                    Text(m.title).font(Theme.Font.body).foregroundStyle(.primary).lineLimit(1)
                    if m.id == wizard.defaultModel { Chip(text: String(localized: "默认"), look: .tint(Theme.accent)) }
                }
                Text(ProviderModelText.detail(wizard.model(m.id) ?? m)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            if picked {
                Button {
                    withAnimation(Theme.Motion.standard) { expanded = expanded == m.id ? nil : m.id }
                } label: { Image(systemName: Theme.Icon.info) }
                .buttonStyle(.borderless)
                .accessibilityLabel("\(m.title) 的上下文和思考档位")
            }
        }
        if picked, expanded == m.id { modelDetail(m.id) }
    }

    /// One picked model's context window and default thinking level, under its row.
    @ViewBuilder private func modelDetail(_ id: String) -> some View {
        let m = wizard.model(id) ?? ProviderModel(id: id)
        let levels = (m.efforts ?? []).isEmpty ? SessionModes.effortLevels.map(\.id) : (m.efforts ?? [])
        numberField(String(localized: "上下文窗口"), Binding(get: { m.contextWindow.map(String.init) ?? "" },
                                     set: { v in wizard.update(id) { $0.contextWindow = Int(v) } }), String(localized: "未知（按 200K）"))
        Picker("默认思考档位", selection: Binding(get: { m.defaultEffort ?? "" },
                                            set: { v in wizard.update(id) { $0.defaultEffort = v.isEmpty ? nil : v } })) {
            Text("不设").tag("")
            ForEach(levels, id: \.self) { level in
                Text(SessionModes.effortLevels.first { $0.id == level }?.title ?? level).tag(level)
            }
        }
        .lineLimit(1)
    }

    @ViewBuilder private var advancedFields: some View {
        @Bindable var wizard = wizard
        Group {
            tierField(String(localized: "Opus 档位"), $wizard.opusModel)
            tierField(String(localized: "Sonnet 档位"), $wizard.sonnetModel)
            tierField(String(localized: "Haiku 档位"), $wizard.haikuModel)
            tierField(String(localized: "Fable 档位"), $wizard.fableModel)
            tierField(String(localized: "小模型"), $wizard.smallModel)
            tierField(String(localized: "子 Agent 模型"), $wizard.subagentModel)
            Toggle("子 Agent 一律用这个模型", isOn: $wizard.subagentForce).lineLimit(1)
            Picker("默认思考档位", selection: $wizard.effort) {
                Text("跟随模型").tag("")
                ForEach(SessionModes.effortLevels) { Text($0.title).tag($0.id) }
            }
            .lineLimit(1)
        }
        Group {
            numberField(String(localized: "自动压缩窗口"), $wizard.compactWindow, String(localized: "默认按模型"))
            numberField(String(localized: "最大上下文"), $wizard.maxContext, String(localized: "默认按模型"))
            numberField(String(localized: "请求超时（毫秒）"), $wizard.timeoutMS, String(localized: "默认 3000000"))
            TextField("代理，比如 http://127.0.0.1:7890", text: $wizard.proxy)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
            TextField("不走代理的地址（NO_PROXY）", text: $wizard.noProxy)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                TextField(wizard.hasStoredHeaders ? "请求头已设置（不显示，不改就留空）" : "请求头，一行一个 Name: value",
                          text: $wizard.headers, axis: .vertical)
                    .font(Theme.Font.body.monospaced())
                    .lineLimit(2...5)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                    .privacySensitive() // header values are credentials: redacted in snapshots and the app switcher
                    .accessibilityIdentifier("wizard.headers")
                if wizard.hasStoredHeaders {
                    Button("清除已设置的请求头", role: .destructive) { wizard.clearHeaders = true }
                        .lineLimit(1).buttonStyle(.borderless)
                } else if wizard.clearHeaders {
                    Text("保存后清除已设置的请求头").font(Theme.Font.label).foregroundStyle(Theme.cautionText)
                }
            }
        }
    }

    private func tierField(_ label: String, _ text: Binding<String>) -> some View {
        LabeledContent(label) {
            TextField("同默认模型", text: text)
                .font(Theme.Font.body.monospaced()).multilineTextAlignment(.trailing)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
        }
    }

    private func numberField(_ label: String, _ text: Binding<String>, _ hint: String) -> some View {
        LabeledContent(label) {
            TextField(hint, text: Binding(get: { text.wrappedValue }, set: { text.wrappedValue = $0.filter(\.isNumber) }))
                .keyboardType(.numberPad).multilineTextAlignment(.trailing).font(Theme.Font.body.monospacedDigit())
        }
    }

    // MARK: Import

    /// The machines to import from: the hub group's, online first.
    private var importMachines: [Machine] {
        model.hubMachines.sorted { ($0.online ? 0 : 1, $0.name) < ($1.online ? 0 : 1, $1.name) }
    }

    #if DEBUG
    /// `-openProviderWizard connect|models` fills the form in with made-up values (screenshots).
    private func debugFill() async {
        guard let want = UserDefaults.standard.string(forKey: "openProviderWizard"), wizard.isNew, wizard.name.isEmpty,
              want == "connect" || want == "models" else { return }
        wizard.kind = .custom
        wizard.name = "demo_gateway"
        wizard.baseURL = "https://gateway.example.com"
        wizard.apiKey = "sk-test-demo"
        if want == "models" { await toModels() }
    }
    #endif
}

/// A row that is ticked or not: the tick, what it is about, and 已勾选 / 未勾选 for VoiceOver.
struct CheckRow<Content: View>: View {
    var on: Bool
    var title: String
    var id: String
    var action: () -> Void
    @ViewBuilder var content: Content

    var body: some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: Theme.Space.m) {
                Image(systemName: on ? Theme.Icon.successFilled : Theme.Icon.unchecked)
                    .foregroundStyle(on ? Theme.accent : Color.secondary).font(Theme.Font.heading)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) { content }
                Spacer(minLength: 0)
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(on ? String(localized: "已勾选 \(title)") : String(localized: "未勾选 \(title)"))
        .accessibilityIdentifier(id)
    }
}

/// How a provider's model reads under its name: 「glm-5 · 200K · 思考 low–high」.
enum ProviderModelText {
    static func detail(_ m: ProviderModel) -> String {
        var parts = [m.id]
        if let w = m.contextWindow, w > 0 { parts.append(LauncherDetail.tokens(w)) }
        if let e = m.efforts, !e.isEmpty {
            let range = e.count > 1 ? "\(e.first!)–\(e.last!)" : e[0]
            parts.append(String(localized: "思考 \(range)"))
        }
        return parts.joined(separator: " · ")
    }
}

/// 测试连接's answer: ✓ with how long and which model, ✗ with why.
struct ProbeResultRow: View {
    var result: LauncherProbeResult

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.m) {
            Image(systemName: result.ok ? Theme.Icon.successFilled : Theme.Icon.errorFilled)
                .foregroundStyle(result.ok ? Theme.positive : Theme.danger)
                .scaledFrame(width: Theme.Size.iconS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(result.ok ? "连接正常" : "连接失败").font(Theme.Font.heading).lineLimit(1)
                let detail = [result.model, result.ms.map { LauncherCheckRow.seconds($0) }].compactMap { $0 }.joined(separator: " · ")
                if result.ok, !detail.isEmpty {
                    Text(detail).font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
                }
                if let e = result.error, !e.isEmpty {
                    Text(e).font(Theme.Font.code).foregroundStyle(Theme.danger).lineLimit(6).textSelection(.enabled)
                }
            }
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityIdentifier("wizard.testResult")
    }
}

/// 从 ~/.zshrc 导入's sections: the machine, what was found (to tick), and what came of it.
struct LauncherImportSections: View {
    var imports: LauncherImportModel
    var machines: [Machine]

    var body: some View {
        switch imports.phase {
        case .machine, .loading: machineSection
        case .preview, .importing: previewSections
        case .done: resultSection
        }
    }

    @ViewBuilder private var machineSection: some View {
        Section {
            ForEach(machines) { m in
                Button {
                    imports.machine = m.id
                    Task { await imports.load() }
                } label: {
                    HStack(spacing: Theme.Space.m) {
                        Image(systemName: Theme.Icon.machine).foregroundStyle(m.online ? Theme.accent : Color.secondary)
                        Text(m.name).font(Theme.Font.body).foregroundStyle(.primary).lineLimit(1)
                        Spacer(minLength: Theme.Space.s)
                        if imports.phase == .loading, imports.machine == m.id { ProgressView() }
                        else if !m.online { Text("不在线").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1) }
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .disabled(!m.online || imports.phase == .loading)
                .accessibilityIdentifier("import.machine.\(m.id)")
            }
        } header: { Text("选一台电脑") } footer: {
            Text("读这台电脑的 ~/.zshrc，找出形如 alias x=\"K=V … claude\" 的行，按网关和 Key 合成供应商。Key 只在 hub 里转手，加密保存。")
        }
        if let e = imports.error {
            Section {
                Label(e, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
                if imports.outdated {
                    Text("在「设置 › 电脑」里重新部署这台电脑的 personad 后再试。").font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
            .accessibilityIdentifier("import.error")
        }
    }

    @ViewBuilder private var previewSections: some View {
        Section {
            if imports.providers.isEmpty {
                Text("这台电脑的 ~/.zshrc 里没有调用 claude 的 alias。").font(Theme.Font.label).foregroundStyle(.secondary)
            }
            ForEach(imports.providers) { p in
                CheckRow(on: imports.picked.contains(p.name), title: p.name, id: "import.provider.\(p.name)") {
                    imports.toggle(p.name)
                } content: {
                    HStack(spacing: Theme.Space.sm) {
                        Text(p.name).font(Theme.Font.heading.monospaced()).foregroundStyle(.primary).lineLimit(1)
                        if let e = p.existing { Chip(text: String(localized: "合并到 \(e)")) }
                    }
                    Text(ImportText.detail(p)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                    if let a = p.aliases, !a.isEmpty {
                        Text("alias " + a.joined(separator: "、")).font(Theme.Font.code).foregroundStyle(.tertiary).lineLimit(1)
                    }
                }
            }
        } header: { Text(machineName) } footer: {
            Text("已有同样网关和 Key 的供应商只合并模型，其他设置不变。")
        }
        if let e = imports.error {
            Section { LoadProblem(text: e) }
        }
    }

    private var machineName: String {
        let name = machines.first { $0.id == imports.machine }?.name ?? imports.machine
        return String(localized: "在 \(name) 上找到 \(imports.providers.count) 个")
    }

    @ViewBuilder private var resultSection: some View {
        let created = imports.result?.created ?? []
        let updated = imports.result?.updated ?? []
        Section {
            ForEach(created, id: \.self) { n in
                Label { Text(n).font(Theme.Font.body.monospaced()) } icon: {
                    Image(systemName: Theme.Icon.successFilled).foregroundStyle(Theme.positive)
                }
            }
            ForEach(updated, id: \.self) { n in
                Label { Text("\(n)（合并了模型）").font(Theme.Font.body.monospaced()) } icon: {
                    Image(systemName: Theme.Icon.successFilled).foregroundStyle(Theme.accent)
                }
            }
            if created.isEmpty && updated.isEmpty {
                Text("没有导入任何供应商。").font(Theme.Font.label).foregroundStyle(.secondary)
            }
        } header: { Text("新增 \(created.count) 个，合并 \(updated.count) 个") } footer: {
            Text("在「模型配置」里给主对话和 Agents 选择它们。")
        }
        .accessibilityIdentifier("import.result")
    }
}

/// How an imported provider reads: its gateway, its models, whether it has a key.
enum ImportText {
    static func detail(_ p: LauncherImportPreview.Provider) -> String {
        var parts: [String] = []
        parts.append(p.baseURL.flatMap { URL(string: $0)?.host() } ?? String(localized: "Anthropic 官方"))
        let models = (p.models ?? []).map(\.id)
        if !models.isEmpty { parts.append(models.count == 1 ? models[0] : String(localized: "\(models.count) 个模型")) }
        else if let m = p.model, !m.isEmpty { parts.append(m) }
        if p.hasKey != true { parts.append(String(localized: "没有 Key")) }
        return parts.joined(separator: " · ")
    }
}
