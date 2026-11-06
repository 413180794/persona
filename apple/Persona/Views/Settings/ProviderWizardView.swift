import SwiftUI

/// Where the provider sheet starts.
enum ProviderWizardStep: String, Hashable, Identifiable {
    /// Choose Provider (adding).
    case pick
    /// Connect (editing starts here).
    case connect
    /// Choose Models.
    case models
    /// Import from ~/.zshrc.
    case importZshrc = "import"
    /// A preset's key (a hub with built-in presets).
    case key
    /// What adding a preset came to.
    case done
    /// "I already use Claude Code": try the main conversation once.
    case claude
    /// "Where to get a model": where each built-in provider gives out keys.
    case getModel = "get"

    var id: Self { self }
}

/// Settings › Model Providers' sheet to add or change a provider (#2455, after cindy): three steps in one form, the way back in
/// the bar's leading button (a Mac sheet does not push, #1170), and Import from ~/.zshrc as a step of its own. Other Connection Types
/// (Direct API, a plugin's provider) is the form it had before (`LauncherEditor`).
///
/// On a hub with built-in presets (#4197, docs/onboarding.md 4.3「三条路」) the first step lists them: a preset goes
/// Paste Key → Done, the custom compatible gateway the three steps; "I already use Claude Code" and "Where to get a
/// model" sit beside them, and the import / other connection types under Advanced.
struct ProviderWizard: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var saved: () async -> Void
    @State private var wizard: ProviderWizardModel
    @State private var imports: LauncherImportModel
    @State private var step: ProviderWizardStep
    private let start: ProviderWizardStep
    /// Other Connection Types: the older form in this sheet.
    @State private var other = false
    /// The model whose context window and thinking levels are open under its row.
    @State private var expanded: String?
    @State private var newModel = ""
    @State private var showAdvanced = false
    /// The first step's Advanced (import, other ways), open.
    @State private var showPickAdvanced = false
    /// The preset's More Options (name, display name, model), open.
    @State private var showPresetMore = false
    /// Where Back goes from the key step: the list, or "Where to get a model" it came from.
    @State private var keyFrom: ProviderWizardStep = .pick
    /// A signup page open in the app (「Key 从哪拿」, 「去开通」).
    @State private var link: LinkTarget?
    /// The preset whose Sign Up page is open: once it closes, its key is next.
    @State private var signingUp: String?

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
                    case .key: keySections
                    case .done: doneSections
                    case .claude: claudeSections
                    case .getModel: getModelSections
                    }
                }
                .pluginLinks($link)
                .onChange(of: link) { _, now in
                    // Back from a Sign Up page: its key is next.
                    guard now == nil, let id = signingUp else { return }
                    signingUp = nil
                    pickPreset(id, from: .getModel)
                }
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbar }
                // Cancel on the first step (a Mac asks first once something is typed); Back after it.
                .sheetCancel(unsaved: !wizard.name.isEmpty || !wizard.apiKey.isEmpty || !wizard.presetKey.isEmpty,
                             when: step == start) { dismiss() }
                .animation(Theme.Motion.standard, value: step)
                // Again once the hub's features are in (a sheet opened while connecting starts without them).
                .task(id: wizard.presetsFlow) {
                    // The toggle's default reads the readiness: ask for a fresh one alongside the presets.
                    if wizard.presetsFlow, model.hubFeatures.contains(AssistantReadiness.feature) {
                        Task { await model.readiness.refresh(api: model.api) }
                    }
                    await wizard.loadPresets()
                    #if DEBUG
                    await debugFill()
                    #endif
                }
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

    /// 「第 2 步，共 3 步 · 连接」 over each step of the wizard (not the import, not the two side paths).
    private func stepHeader(_ n: Int, _ what: String) -> some View {
        Text(wizard.isNew ? String(localized: "第 \(n) 步，共 3 步 · \(what)") : what)
            .accessibilityIdentifier("wizard.step")
    }

    // MARK: Bar

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        if step != start && step != .done {
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
            case .key:
                Button { Task { await savePreset() } } label: {
                    if wizard.saving { ProgressView() } else { Text("保存").lineLimit(1) }
                }
                .disabled(!wizard.canSavePreset)
                .accessibilityIdentifier("wizard.save")
            case .done:
                Button("完成") { dismiss() }.lineLimit(1).accessibilityIdentifier("wizard.done")
            case .claude:
                if wizard.claudeTry?.ok == true {
                    Button("完成") { dismiss() }.lineLimit(1).accessibilityIdentifier("wizard.done")
                }
            case .getModel: EmptyView()
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
        case .key: step = keyFrom
        default: step = .pick
        }
    }

    /// Next from Connect: a new provider reads the gateway's models first (once), so the next step has them.
    private func toModels() async {
        if !wizard.discoveredOnce && wizard.choices.isEmpty { await wizard.discover() }
        step = .models
    }

    private func save() async {
        if await finished(wizard.save()) { dismiss() }
    }

    /// Saves the preset's provider; its last screen says what now runs on it.
    private func savePreset() async {
        if await finished(wizard.savePreset()) { step = .done }
    }

    private func pickPreset(_ id: String, from: ProviderWizardStep) {
        if wizard.presetID != id { showPresetMore = false }
        wizard.presetID = id
        keyFrom = from
        step = .key
    }

    private func runImport() async {
        await imports.runImport()
        _ = await finished(imports.phase == .done)
    }

    /// After a save or an import: the lists read the launchers again and the page that opened this hears of it.
    private func finished(_ ok: Bool) async -> Bool {
        guard ok else { Haptics.error(); return false }
        model.launchersChanged()
        model.readinessChanged()
        Haptics.success()
        await saved()
        return true
    }

    // MARK: 1 Choose Provider

    @ViewBuilder private var pickSections: some View {
        if wizard.presetsFlow { presetPickSections } else { legacyPickSections }
    }

    /// An older hub's first step, as it was: Anthropic official (written into the app), a gateway, the import, other ways.
    @ViewBuilder private var legacyPickSections: some View {
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

    /// No computer yet: nothing to read a ~/.zshrc on (the import would wait for one forever).
    private var noComputer: Bool { model.hubMachines.isEmpty }

    /// A hub with built-in presets: its providers in its order, the gateway typed by hand last; the two side paths;
    /// the import and the other ways under Advanced.
    @ViewBuilder private var presetPickSections: some View {
        Section {
            if !wizard.presetsLoaded {
                ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.l)
            }
            ForEach(wizard.presets) { p in
                pickRow(title: p.title, detail: presetDetail(p), id: "wizard.preset.\(p.id)",
                        badge: p.isBuiltin ? nil : String(localized: "扩展")) {
                    Image(systemName: p.isBuiltin ? Theme.Icon.ai : Theme.Icon.plugins).foregroundStyle(Theme.accent)
                } action: { pickPreset(p.id, from: .pick) }
            }
            pickRow(title: String(localized: "自定义兼容网关"), detail: String(localized: "中转站、公司网关或其他兼容 Anthropic 接口的服务，自己填地址和鉴权方式"),
                    id: "wizard.kind.custom") {
                Image(systemName: Theme.Icon.server).foregroundStyle(Theme.accent)
            } action: {
                wizard.kind = .custom
                step = .connect
            }
        } header: { stepHeader(1, String(localized: "选择供应商")) } footer: {
            if let e = wizard.presetsError {
                Text("读不到 hub 提供的供应商：\(e)。可以先用自定义兼容网关。").foregroundStyle(Theme.cautionText)
            } else {
                Text("选一家，粘贴它的 API Key 就能用；地址和模型都已经填好。")
            }
        }
        Section {
            pickRow(title: String(localized: "我已经在用 Claude Code"), detail: String(localized: "不用加供应商，试一句看主对话能不能用"), id: "wizard.path.claude") {
                BrandMark(Theme.Brand.claude, side: Theme.Size.icon)
            } action: { step = .claude }
            pickRow(title: String(localized: "去哪弄一个模型"), detail: String(localized: "还没有 API Key：看看各家在哪开通"), id: "wizard.path.get") {
                Image(systemName: Theme.Icon.help).foregroundStyle(Theme.accent)
            } action: { step = .getModel }
            .disabled(wizard.signupPresets.isEmpty)
        }
        Section {
            DisclosureGroup(isExpanded: $showPickAdvanced) {
                pickRow(title: String(localized: "从 ~/.zshrc 导入"),
                        detail: noComputer ? String(localized: "还没有电脑：先添加一台电脑，再从它导入") : String(localized: "把一台电脑上调用 claude 的 alias 变成供应商"),
                        id: "wizard.kind.import") {
                    Image(systemName: Theme.Icon.terminal).foregroundStyle(noComputer ? Color.secondary : Theme.accent)
                } action: { step = .importZshrc }
                .disabled(noComputer)
                pickRow(title: String(localized: "其他接入方式"), detail: String(localized: "直接 API（Messages / Responses）、扩展提供的供应商"), id: "wizard.kind.other") {
                    Image(systemName: Theme.Icon.options).foregroundStyle(.secondary)
                } action: { other = true }
            } label: { Text("高级").lineLimit(1) }
            .accessibilityIdentifier("wizard.pick.advanced")
        }
    }

    /// A built-in provider says who it is for; an extension's says its kind and host.
    private func presetDetail(_ p: LauncherPreset) -> String {
        if p.isBuiltin { return p.description.flatMap { $0.isEmpty ? nil : $0 } ?? p.host }
        return [p.description, LauncherPresetText.subtitle(p)].compactMap { $0?.isEmpty == false ? $0 : nil }.joined(separator: "\n")
    }

    // MARK: 2 Paste Key

    @ViewBuilder private var keySections: some View {
        @Bindable var wizard = wizard
        if let p = wizard.preset {
            Section {
                HStack(spacing: Theme.Space.m) {
                    Image(systemName: p.isBuiltin ? Theme.Icon.ai : Theme.Icon.plugins).foregroundStyle(Theme.accent)
                        .scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(p.title).font(Theme.Font.heading).lineLimit(1)
                        Text(p.isBuiltin ? p.host : String(localized: "由扩展「\(p.plugin)」提供"))
                            .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                .accessibilityElement(children: .combine)
                SecretField(String(localized: "粘贴 API Key"), filled: String(localized: "已填写"), text: $wizard.presetKey, identifier: "wizard.preset.key")
                    .onChange(of: wizard.presetKey) { wizard.keyChanged() }
                if let url = p.signup {
                    Button { link = LinkTarget(url: url) } label: {
                        Label("Key 从哪拿", systemImage: Theme.Icon.open).lineLimit(1)
                    }
                    .accessibilityIdentifier("wizard.preset.signup")
                }
            } header: { stepHeader(2, String(localized: "粘贴 Key")) } footer: {
                Text([p.keyHint, String(localized: "Key 加密存在 hub，不会返回到客户端。")].compactMap { $0?.isEmpty == false ? $0 : nil }.joined(separator: "\n"))
            }
            Section {
                keyTestRow(p)
            } header: { Text("测试") } footer: {
                if p.testable { Text("粘贴后会自动测试一次：发一条很短的请求，可能产生少量费用。") }
            }
            Section {
                Toggle(isOn: Binding(get: { wizard.assign }, set: { wizard.assignChoice = $0 })) {
                    Text("主对话和新开的 Agent 都用它").lineLimit(2)
                }
                .accessibilityIdentifier("wizard.assign")
            } footer: { Text("只改还没人设置过的；你选过的模型不会动。") }
            Section {
                DisclosureGroup(isExpanded: $showPresetMore) { presetMoreFields(p) } label: {
                    HStack {
                        Text("更多选项").lineLimit(1)
                        Spacer()
                        if !showPresetMore { Text("名字、默认模型").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1) }
                    }
                }
                .accessibilityIdentifier("wizard.preset.more")
            }
            if let e = wizard.error {
                Section { LoadProblem(text: e) }
            }
        }
    }

    @ViewBuilder private func keyTestRow(_ p: LauncherPreset) -> some View {
        if !p.testable {
            Text("Responses 协议的 Key 没法在这里测试，保存后直接用就行。").font(Theme.Font.label).foregroundStyle(.secondary)
        } else if wizard.keyTesting {
            HStack(spacing: Theme.Space.s) {
                ProgressView()
                Text("正在测试这个 Key…").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
        } else if let t = wizard.keyTest {
            ProbeResultRow(result: t)
            if !t.ok {
                Button { Task { await wizard.testKey() } } label: {
                    Label("重新测试", systemImage: Theme.Icon.reload).lineLimit(1)
                }
                .accessibilityIdentifier("wizard.preset.retest")
            }
        } else {
            Text(wizard.presetKey.isEmpty ? String(localized: "粘贴 Key 后自动测试") : String(localized: "停下来就开始测试…"))
                .font(Theme.Font.label).foregroundStyle(.secondary)
        }
    }

    @ViewBuilder private func presetMoreFields(_ p: LauncherPreset) -> some View {
        @Bindable var wizard = wizard
        TextField("名字（留空自动起名）", text: $wizard.presetName)
            .font(Theme.Font.body.monospaced())
            .textInputAutocapitalization(.never).autocorrectionDisabled()
            .accessibilityIdentifier("wizard.preset.name")
        if let problem = wizard.presetNameProblem {
            Text(problem).font(Theme.Font.label).foregroundStyle(Theme.danger)
        }
        TextField(String(localized: "显示名，留空用「\(p.title)」"), text: $wizard.presetDisplayName)
            .accessibilityIdentifier("wizard.preset.displayName")
        if p.choices.isEmpty {
            TextField("默认模型", text: $wizard.presetModel).textInputAutocapitalization(.never).autocorrectionDisabled()
                .onChange(of: wizard.presetModel) { wizard.keyChanged() }
        } else {
            Picker("默认模型", selection: $wizard.presetModel) {
                ForEach(p.choices) { m in Text(m.label).tag(m.id) }
            }
            .lineLimit(1)
            .onChange(of: wizard.presetModel) { wizard.keyChanged() }
        }
    }

    // MARK: 3 Done

    @ViewBuilder private var doneSections: some View {
        let summary = wizard.addedSummary
        Section {
            VStack(spacing: Theme.Space.s) {
                Image(systemName: Theme.Icon.successFilled).font(Theme.Font.display).foregroundStyle(Theme.positive)
                Text("\(wizard.addedTitle) 接好了").font(Theme.Font.title).multilineTextAlignment(.center)
                if !summary.assigned.isEmpty {
                    Text(summary.assigned).font(Theme.Font.body).multilineTextAlignment(.center)
                }
                ForEach(summary.kept, id: \.self) { k in
                    Text(k).font(Theme.Font.secondary).foregroundStyle(.secondary).multilineTextAlignment(.center)
                }
                if summary.assigned.isEmpty {
                    Text("要让主对话或 Agent 用它，到「模型配置」里选。").font(Theme.Font.secondary).foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Space.l)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("wizard.doneResult")
        } header: { stepHeader(3, String(localized: "完成")) }
    }

    // MARK: I already use Claude Code

    @ViewBuilder private var claudeSections: some View {
        Section {
            Button { Task { await wizard.tryClaude() } } label: {
                HStack {
                    Label(wizard.claudeTry == nil ? "试一句" : "再试一次", systemImage: Theme.Icon.tryOut).lineLimit(1)
                    if wizard.claudeTrying { Spacer(); ProgressView() }
                }
            }
            .disabled(wizard.claudeTrying)
            .accessibilityIdentifier("wizard.claude.try")
            if let t = wizard.claudeTry { claudeTryRow(t) }
        } header: { Text("我已经在用 Claude Code") } footer: {
            Text("主对话默认用你电脑上的 Claude Code，不用另加供应商。试一句会真的发一个很小的请求，可能产生少量费用。")
        }
        if wizard.claudeTry != nil && wizard.claudeTry?.ok != true {
            Section {
                Button { step = .pick } label: { Text("改用 API Key").lineLimit(1) }
                    .accessibilityIdentifier("wizard.claude.useKey")
            } footer: { Text("没登录或者用不了时，也可以选一家供应商，粘贴它的 API Key。") }
        }
    }

    @ViewBuilder private func claudeTryRow(_ t: ClaudeTry) -> some View {
        switch t {
        case .answered(let r):
            if let p = r.probe {
                let machine = r.machine.flatMap { $0.name.isEmpty ? nil : $0.name }
                let ok = p.ok == true
                ClaudeTryResult(ok: ok,
                                title: ok ? (machine.map { String(localized: "\($0) 上的 Claude Code 能用") } ?? String(localized: "Claude Code 能用"))
                                            : String(localized: "试了一句，没成功"),
                                detail: ok ? [p.model, p.answer.map { String(localized: "回复「\($0)」") }, p.millis.map { LauncherCheckRow.seconds($0) }]
                                                .compactMap { $0?.isEmpty == false ? $0 : nil }.joined(separator: " · ")
                                             : (p.error ?? ""))
            } else {
                // The computer is not ready: no try ran; the step in the way says why.
                let step = r.steps.first { $0.state != "ok" && $0.state != "skipped" && $0.title?.isEmpty == false }
                ClaudeTryResult(ok: false, title: step?.title ?? String(localized: "现在试不了"),
                                detail: step?.detail ?? String(localized: "主对话还没有能用的电脑。"))
            }
        case .unsupported:
            ClaudeTryResult(ok: false, title: String(localized: "这里还试不了"), detail: String(localized: "直接在主对话里发一句试试。"))
        case .failed(let e):
            ClaudeTryResult(ok: false, title: String(localized: "试了一句，没成功"), detail: e)
        }
    }

    // MARK: Where to get a model

    @ViewBuilder private var getModelSections: some View {
        Section {
            ForEach(wizard.signupPresets) { p in
                HStack(spacing: Theme.Space.m) {
                    Image(systemName: Theme.Icon.ai).foregroundStyle(Theme.accent)
                        .scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(p.title).font(Theme.Font.heading).lineLimit(1)
                        if let d = p.description, !d.isEmpty {
                            Text(d).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(3)
                        }
                    }
                    Spacer(minLength: Theme.Space.s)
                    Button("去开通") {
                        guard let url = p.signup else { return }
                        signingUp = p.id
                        link = LinkTarget(url: url)
                    }
                    .lineLimit(1)
                    .themedButton()
                    .accessibilityIdentifier("wizard.get.\(p.id)")
                }
                .padding(.vertical, Theme.Space.xxs)
            }
        } header: { Text("去哪弄一个模型") } footer: {
            Text("在厂商的网站上开通并拿到 API Key，回来就能粘贴。")
        }
    }

    private func pickRow(title: String, detail: String, id: String, badge: String? = nil, @ViewBuilder icon: () -> some View,
                         action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.m) {
                icon().scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    HStack(spacing: Theme.Space.sm) {
                        Text(title).font(Theme.Font.heading).foregroundStyle(.primary).lineLimit(1)
                        if let badge { Chip(text: badge) }
                    }
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

    // MARK: 2 Connect

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

    // MARK: 3 Choose Models

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
    /// `-openProviderWizard connect|models` fills the form in with made-up values (screenshots); `key` / `done` take the
    /// first built-in preset (`-wizardPreset <id>` another) with a made-up key (`-wizardKey <key>`: the mock hub
    /// refuses one with "bad" in it), `done` saves it; `claude` tries the main conversation.
    private func debugFill() async {
        guard let want = UserDefaults.standard.string(forKey: "openProviderWizard"), wizard.isNew, wizard.name.isEmpty else { return }
        if want == "claude" { await wizard.tryClaude(); return }
        if want == "key" || want == "done" {
            let d = UserDefaults.standard
            guard let p = wizard.presets.first(where: { $0.id == d.string(forKey: "wizardPreset") }) ?? wizard.presets.first(where: \.isBuiltin)
            else { return }
            pickPreset(p.id, from: .pick)
            wizard.presetKey = d.string(forKey: "wizardKey") ?? "sk-demo-0123456789"
            await wizard.testKey()
            if want == "done" { await savePreset() }
            return
        }
        guard want == "connect" || want == "models" else { return }
        wizard.kind = .custom
        wizard.name = "demo_gateway"
        wizard.baseURL = "https://gateway.example.com"
        wizard.apiKey = "sk-test-demo"
        if want == "models" { await toModels() }
    }
    #endif
}

/// A row that is ticked or not: the tick, what it is about, and selected / not selected for VoiceOver.
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

/// Test Connection's answer: ✓ with how long and which model, ✗ with why.
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

/// "I already use Claude Code"'s answer: ✓ the computer's Claude Code answered (the model, its words, how long), ✗ why not.
struct ClaudeTryResult: View {
    var ok: Bool
    var title: String
    var detail: String

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.m) {
            Image(systemName: ok ? Theme.Icon.successFilled : Theme.Icon.errorFilled)
                .foregroundStyle(ok ? Theme.positive : Theme.danger)
                .scaledFrame(width: Theme.Size.iconS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(title).font(Theme.Font.heading).lineLimit(2)
                if !detail.isEmpty {
                    Text(detail).font(ok ? Theme.Font.label : Theme.Font.code).foregroundStyle(ok ? Color.secondary : Theme.danger)
                        .lineLimit(6).textSelection(.enabled)
                }
            }
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("wizard.claude.result")
    }
}

/// Import from ~/.zshrc's sections: the machine, what was found (to tick), and what came of it.
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
            if machines.isEmpty {
                Text("还没有电脑。先添加一台电脑，再从它导入。").font(Theme.Font.label).foregroundStyle(.secondary)
                    .accessibilityIdentifier("import.noComputer")
            }
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
