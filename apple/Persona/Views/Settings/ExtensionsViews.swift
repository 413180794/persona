import SwiftUI

/// 设置 › 扩展: every plugin of the hub's (built in, from hub.json, installed on the hub's machine), its state, and
/// the way to its page. Extensions are installed on the hub's side only (docs/extensions.md「安装」): with `personad plugin` on
/// its machine or in its admin UI (/admin); the footer says how.
struct ExtensionsView: View {
    @State private var page = ExtensionsModel()
    @State private var cardLink: LinkTarget?

    var body: some View {
        SettingsForm {
            #if os(macOS)
            // The plugins' overview cards (docs/extensions.md「UI 插槽」): the Mac has no 总览, they lead this list (their
            // pages are in the sidebar). The iPhone has them on 设置 itself.
            ExtensionCardsSection(link: $cardLink)
            #endif
            // 扩展市场 (docs/extensions.md「扩展市场」): only with a hub that has markets.
            if page.markets {
                Section {
                    Group {
                        NavigationLink(value: SettingsPage.pluginMarket) {
                            SettingsRow(title: "扩展市场", symbol: Theme.Icon.pluginMarket, tint: Theme.accent)
                        }
                        .accessibilityIdentifier("plugins-market")
                    }
                    .bareListRow()
                }
            }
            if let plugins = page.plugins, plugins.isEmpty {
                Section {
                    EmptyStateView(symbol: Theme.Icon.plugins, title: "没有扩展",
                                   message: "扩展在 hub 的管理界面（/admin）或 hub 所在的机器上用 personad extension install 安装，装好后在这里开关和设置。")
                        .listRowBackground(Color.clear)
                }
            } else if let plugins = page.plugins {
                Section {
                    Group {
                        ForEach(plugins) { p in
                            NavigationLink(value: SettingsPage.plugin(p.id)) { ExtensionRowLabel(plugin: p) }
                                .accessibilityIdentifier("plugin-\(p.id)")
                        }
                    }
                    .bareListRow()
                } footer: {
                    Text("扩展在 hub 的管理界面（/admin）或 hub 所在的机器上（personad extension）安装、升级和卸载，这里可以开关、改设置、填密钥。")
                }
            }
            if let error = page.error {
                #if os(macOS)
                // A page's empty state, not the request's error in red (#2113).
                Section {
                    EmptyStateView(symbol: Theme.Icon.plugins, title: page.missing ? "没有扩展" : "扩展没有加载出来",
                                   message: page.missing
                                       ? (AppModel.shared.isDemo ? "体验模式里没有扩展。连接自己的 hub 后，装好的扩展在这里开关和设置。"
                                                                 : "这个 hub 的版本还不支持扩展，升级 hub 后再来看。")
                                       : error,
                                   action: page.missing ? nil : ("重试", { Task { await page.load() } }))
                        .listRowBackground(Color.clear)
                }
                #else
                Section { Text(error).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled) }
                #endif
            }
        }
        .accessibilityIdentifier("extensions-list")
        .themedPage()
        #if os(macOS)
        .pluginLinks($cardLink)
        #endif
        .overlay { if page.plugins == nil && page.loading { ProgressView() } }
        .detailTitle("扩展")
        .navigationBarTitleDisplayMode(.inline)
        .task { await page.load() }
        #if os(macOS)
        .task { await ExtensionSlots.shared.reload() }
        #endif
        .refreshable {
            await page.load()
            await ExtensionSlots.shared.reload()
        }
    }
}

/// A plugin's row: its name, and its state (or version) on the right with a dot that says whether it needs the user.
private struct ExtensionRowLabel: View {
    var plugin: HubExtension

    var body: some View {
        if AppStyle.current.usesBeautifulUI {
            SettingsRow(title: plugin.name, symbol: Theme.Icon.plugins, tint: ExtensionStateDot.tone(plugin), value: ExtensionText.summary(plugin))
        } else {
            LabeledContent {
                HStack(spacing: Theme.Space.sm) {
                    ExtensionStateDot(plugin: plugin)
                    Text(ExtensionText.summary(plugin)).lineLimit(1)
                }
            } label: {
                Label {
                    Text(plugin.name).lineLimit(1)
                } icon: {
                    Image(systemName: Theme.Icon.plugins)
                }
            }
        }
    }
}

/// A line of a plugin's 升级 section: what, and below it what happens (in the caution colour when it needs the user).
private struct ExtensionUpgradeLine: View {
    var title: String
    var detail: String
    var attention: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            Text(title).font(Theme.Font.body)
            if !detail.isEmpty {
                Text(detail).font(Theme.Font.label).foregroundStyle(attention ? Theme.cautionText : Color.secondary)
            }
        }
        .padding(.vertical, Theme.Space.xxs)
    }
}

/// The dot beside a plugin's state.
private struct ExtensionStateDot: View {
    var plugin: HubExtension?

    var body: some View {
        Circle().fill(Self.tone(plugin)).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
    }

    static func tone(_ p: HubExtension?) -> Color {
        guard let p else { return .secondary }
        if ExtensionText.isProblem(p) { return Theme.danger }
        switch p.status {
        case "running", "stopped": return Theme.positive // on, running or idle (#1684)
        case "needs_setup": return Theme.caution
        default: return .secondary
        }
    }
}

/// 设置 › 扩展 › one: what it is and how it is doing, its switch, its settings as a form drawn from its schema, its
/// secrets (set here, never shown again), what it contributes and what it may do. A plugin written in hub.json is only
/// looked at here.
struct ExtensionDetailView: View {
    @State private var page: ExtensionModel
    @State private var editingSecret: ExtensionSecret?
    @State private var declarations = ExtensionTextContributions()
    @Environment(\.scenePhase) private var phase

    init(id: String) { _page = State(initialValue: ExtensionModel(id: id)) }

    var body: some View {
        SettingsForm {
            header
            if let d = page.detail {
                if d.readOnly {
                    Section {
                        Text("这个扩展写在 hub.json 里：开关和设置在 hub 所在的机器上改。").font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                } else {
                    Section {
                        Toggle(isOn: Binding(get: { d.plugin.enabled ?? true }, set: { on in Task { await page.setEnabled(on) } })) {
                            Text("启用").lineLimit(1)
                        }
                        .disabled(page.changingEnabled || page.saving || page.changingContribution != nil)
                        .accessibilityIdentifier("plugin-enabled")
                    } footer: {
                        Text("停用后它不再运行，提供的连接器等也一起关掉；设置和密钥留着。")
                    }
                }
                if let auto = d.plugin.autoUpgrade { upgrades(d.plugin, auto: auto) }
                pages(d)
                if !d.readOnly && !d.form.isEmpty { form(d) }
                if !d.readOnly && !d.secrets.isEmpty { secrets(d) }
                contributions(d)
                dependencies(d)
                permissions(d)
                about(d)
            }
        }
        .themedPage()
        .detailTitle(page.detail?.plugin.name ?? "扩展")
        .navigationBarTitleDisplayMode(.inline)
        .pageToolbar {
            if page.detail?.readOnly == false && !(page.detail?.form.isEmpty ?? true) {
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        Task { await page.save() }
                    } label: {
                        if page.saving { ProgressView() } else { Text("保存").lineLimit(1) }
                    }
                    .disabled(!page.canSave)
                    .accessibilityIdentifier("plugin-save")
                }
            }
        }
        .confirmationDialog("连带停用依赖它的扩展？", isPresented: Binding(
            get: { page.disableConfirmation != nil },
            set: { if !$0 { page.disableConfirmation = nil } }), titleVisibility: .visible) {
            Button(role: .destructive) { Task { await page.confirmDisable() } } label: { Text("一起停用").lineLimit(1) }
                .accessibilityIdentifier("extension-cascade-confirm")
            Button("取消", role: .cancel) {}
        } message: {
            Text(page.disableConfirmation?.message ?? "")
        }
        .sheet(item: $editingSecret) { secret in
            ExtensionSecretSheet(secret: secret, save: { await page.setSecret(secret.name, value: $0) },
                              clear: { await page.clearSecret(secret.name) })
        }
        .task { await page.load() }
        .tabActiveTask(id: ExtensionTextSource(group: AppModel.shared.hub?.group ?? "", features: AppModel.shared.hubFeatures,
                                               hub: AppModel.shared.api?.config.url.absoluteString, active: phase == .active)) {
            guard phase == .active else { return }
            await declarations.refreshWhileActive(api: AppModel.shared.api)
        }
        .refreshable { await page.load() }
    }

    // MARK: sections

    @ViewBuilder private var header: some View {
        Section {
            HStack(spacing: Theme.Space.m) {
                Image(systemName: Theme.Icon.plugins).resizable().scaledToFit().foregroundStyle(Theme.accent)
                    .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellM)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(page.detail?.plugin.name ?? page.id).font(Theme.Font.heading).lineLimit(1)
                    HStack(spacing: Theme.Space.sm) {
                        ExtensionStateDot(plugin: page.detail?.plugin)
                        Text(stateText).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
                Spacer(minLength: Theme.Space.s)
                if page.loading && page.detail == nil { ProgressView() }
            }
            .padding(.vertical, Theme.Space.xs)
            let text = page.detail?.plugin.description ?? ""
            let note = page.detail.flatMap { ExtensionText.runNote($0.plugin) }
            if !text.isEmpty || note != nil {
                // Whether it runs right now is small print under what it is, in the same row (#1684).
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    if !text.isEmpty { Text(text).font(Theme.Font.secondary).foregroundStyle(.secondary) }
                    if let note {
                        Text(note).font(Theme.Font.label).foregroundStyle(.secondary)
                            .accessibilityIdentifier("plugin-run-note")
                    }
                }
            }
            if let needs = page.detail?.plugin.needsSetup, !needs.isEmpty {
                Text("还缺：" + needs.joined(separator: "、")).font(Theme.Font.label).foregroundStyle(Theme.caution)
            }
            if let e = page.detail?.plugin.error ?? page.error, !e.isEmpty {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled)
            }
        }
    }

    private var stateText: String {
        guard let p = page.detail?.plugin else { return page.loading ? "正在读取…" : "读不到" }
        return [ExtensionText.state(p), p.version, ExtensionText.source(p.source)].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// A market plugin's upgrades (docs/extensions.md「自动升级」): its own switch, its newer version (opening the market's
    /// upgrade when it waits for the user) or what happened last.
    @ViewBuilder private func upgrades(_ p: HubExtension, auto: Bool) -> some View {
        Section {
            Toggle(isOn: Binding(get: { auto }, set: { on in Task { await page.setAutoUpgrade(on) } })) {
                Text("自动升级").lineLimit(1)
            }
            .accessibilityIdentifier("plugin-auto-upgrade")
            if let u = p.upgrade {
                if let action = ExtensionText.upgradeAction(u) {
                    NavigationLink(value: SettingsPage.marketExtension(market: u.market, id: u.id)) {
                        // The action stays at the end of the row however long what it asks for is.
                        HStack(spacing: Theme.Space.m) {
                            ExtensionUpgradeLine(title: "有新版本 \(u.to)", detail: ExtensionText.upgradeDetail(u),
                                              attention: u.state == "needs_permission" || u.state == "skipped")
                                .frame(maxWidth: .infinity, alignment: .leading)
                            Text(action).foregroundStyle(Theme.accent).lineLimit(1).fixedSize()
                        }
                    }
                    .accessibilityIdentifier("plugin-upgrade")
                } else {
                    ExtensionUpgradeLine(title: "有新版本 \(u.to)", detail: ExtensionText.upgradeDetail(u), attention: false)
                        .accessibilityIdentifier("plugin-upgrade")
                }
            } else if let n = p.upgradeNote, let text = ExtensionText.upgradeNote(n) {
                ExtensionUpgradeLine(title: text, detail: n.at.formatted(.relative(presentation: .named)), attention: n.kind == "rolled_back")
                    .accessibilityIdentifier("plugin-upgrade-note")
            }
        } header: {
            SettingsHeading("升级")
        } footer: {
            Text("扩展市场里出了新版本，扩展空闲时自动装上；新版本要的权限变多时先问你。装上后起不来的会退回原来的版本。")
        }
    }

    /// The pages it draws (docs/extensions.md「声明式 UI」), a row each.
    @ViewBuilder private func pages(_ d: HubExtensionDetail) -> some View {
        if !d.ui.isEmpty {
            Section {
                ForEach(d.ui) { place in
                    NavigationLink(value: SettingsPage.pluginPage(plugin: d.plugin.id, ui: place.id, title: place.title)) {
                        Label { Text(place.title).lineLimit(1) } icon: { Image(systemName: Theme.Icon.pluginPage) }
                    }
                    .accessibilityIdentifier("plugin-page-\(place.id)")
                }
            } header: {
                SettingsHeading("页面")
            }
        }
    }

    private func form(_ d: HubExtensionDetail) -> some View {
        Group {
            Section {
                ForEach(d.form.filter { $0.type != "object" }) { f in ExtensionFieldRow(field: f, path: [f.name], page: page, templates: declarations.templates) }
            } header: {
                SettingsHeading("设置")
            } footer: {
                if let footer = ExtensionText.saveFooter(d.form) { Text(footer) }
            }
            ForEach(d.form.filter { $0.type == "object" }) { group in
                Section {
                    ForEach(group.fields ?? []) { f in ExtensionFieldRow(field: f, path: [group.name, f.name], page: page, templates: declarations.templates) }
                } header: {
                    SettingsHeading(group.label)
                } footer: {
                    if let text = group.description, !text.isEmpty { Text(text) }
                }
            }
        }
    }

    private func secrets(_ d: HubExtensionDetail) -> some View {
        Section {
            ForEach(d.secrets) { s in
                Button { editingSecret = s } label: {
                    LabeledContent {
                        Text(s.set ? "已设置" : (s.required == true ? "未设置（必填）" : "未设置"))
                            .foregroundStyle(s.set || s.required != true ? Color.secondary : Theme.caution)
                            .lineLimit(1)
                    } label: {
                        Label { Text(s.title).lineLimit(1) } icon: { Image(systemName: Theme.Icon.credentials) }
                    }
                }
                .foregroundStyle(Color.primary)
                .accessibilityIdentifier("plugin-secret-\(s.name)")
            }
        } header: {
            SettingsHeading("密钥")
        } footer: {
            Text("密钥加密存在 hub 上，只交给这个扩展；存好后不再显示，只能换新的或清除。")
        }
    }

    @ViewBuilder private func contributions(_ d: HubExtensionDetail) -> some View {
        if let contributions = d.contributions {
            if !contributions.isEmpty {
                Section {
                    ForEach(contributions) { c in
                        Toggle(isOn: Binding(get: { c.enabled }, set: { on in
                            Task { await page.setContribution(c, enabled: on) }
                        })) {
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(c.label).lineLimit(1)
                                Text(ExtensionText.point(c.point) + (c.via == "connector" ? " · 跟着连接器" : ""))
                                    .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                        .disabled(d.readOnly || !c.canDisable || page.changingContribution != nil || page.changingEnabled || page.saving)
                        .accessibilityIdentifier("extension-contribution-\(c.id)")
                    }
                } header: {
                    SettingsHeading("提供")
                } footer: {
                    Text("开关即时生效。关掉一项只停止这项贡献，其他能力和设置保留。")
                }
            }
        } else if let c = d.plugin.contributes, !c.isEmpty {
            Section {
                ForEach(c.keys.sorted(), id: \.self) { point in
                    LabeledContent(ExtensionText.point(point)) {
                        Text((c[point] ?? []).joined(separator: "、")).lineLimit(2).multilineTextAlignment(.trailing)
                    }
                }
            } header: {
                SettingsHeading("提供")
            }
        }
    }

    @ViewBuilder private func dependencies(_ d: HubExtensionDetail) -> some View {
        if let dependencies = d.depends, !dependencies.isEmpty {
            Section {
                ForEach(dependencies) { dependency in
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(dependency.title).font(Theme.Font.body).lineLimit(1)
                        Text(dependency.description).font(Theme.Font.label)
                            .foregroundStyle(dependency.state == "ok" ? Color.secondary : Theme.cautionText)
                    }
                }
            } header: { SettingsHeading("依赖") }
        }
        if let members = d.extensionPack, !members.isEmpty {
            Section {
                ForEach(members, id: \.id) { member in
                    LabeledContent(member.id, value: member.version ?? "")
                }
            } header: { SettingsHeading("合集成员") }
        }
    }

    @ViewBuilder private func permissions(_ d: HubExtensionDetail) -> some View {
        if !d.permissions.isEmpty {
            Section {
                ForEach(d.permissions) { p in
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(p.title).font(Theme.Font.body)
                        if let detail = p.detail, !detail.isEmpty {
                            Text(detail).font(Theme.Font.label).foregroundStyle(.secondary)
                        }
                    }
                    .padding(.vertical, Theme.Space.xxs)
                }
            } header: {
                SettingsHeading("它可以")
            } footer: {
                Text("安装时你同意过这些；新版本要得更多时，升级前会再问一次。")
            }
        }
    }

    @ViewBuilder private func about(_ d: HubExtensionDetail) -> some View {
        Section {
            if let v = d.plugin.version, !v.isEmpty { LabeledContent("版本", value: v) }
            if let v = d.previousVersion, !v.isEmpty { LabeledContent("上一个版本", value: v) }
            if let s = ExtensionText.source(d.plugin.source) { LabeledContent("来源", value: s) }
            if let o = d.origin, !o.isEmpty {
                LabeledContent("安装自") { Text(o).lineLimit(2).truncationMode(.middle).textSelection(.enabled) }
            }
            if let p = d.publisher, !p.isEmpty { LabeledContent("发布者", value: p) }
            if let h = d.homepage, let url = URL(string: h) {
                Link(destination: url) { Label("主页", systemImage: Theme.Icon.openExternal).lineLimit(1) }
            }
            ForEach(d.plugin.warnings ?? [], id: \.self) { w in
                Label { Text(w).font(Theme.Font.label) } icon: { Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution) }
            }
        } header: {
            SettingsHeading("关于")
        }
    }
}

/// One field of a plugin's form, by its type: a text field, a number, a switch, a choice, a list of texts or of
/// choices. Its title above, its description and the hub's word on it below.
private struct ExtensionFieldRow: View {
    var field: ExtensionField
    var path: [String]
    @Bindable var page: ExtensionModel
    var templates: [ExtensionTextTemplate]

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            control
            if field.name == "triage_rules", field.type == "string" {
                ExtensionTriageTemplatePicker(text: Binding(
                    get: { page.value(path)?.string ?? "" },
                    set: { page.set($0.isEmpty ? nil : .string($0), at: path) }), templates: templates)
            }
            if let text = field.description, !text.isEmpty {
                Text(text).font(Theme.Font.label).foregroundStyle(.secondary)
            }
            if let e = page.fieldError(path) {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger)
            }
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityIdentifier("plugin-field-\(path.joined(separator: "."))")
    }

    private var title: String { field.label + (field.required == true && field.default == nil ? "（必填）" : "") }

    @ViewBuilder private var control: some View {
        switch field.type {
        case "boolean":
            Toggle(isOn: Binding(get: { page.value(path)?.bool ?? field.default?.bool ?? false },
                                 set: { page.set(.bool($0), at: path) })) {
                Text(title).lineLimit(2)
            }
        case "array":
            if field.items?.format == "machine" {
                ExtensionMachineSwitches(field: field, title: title, path: path, page: page)
            } else {
                ExtensionListField(field: field, title: title, path: path, page: page)
            }
        default:
            if let choices = field.enum {
                Picker(selection: Binding(get: { page.value(path) ?? .null }, set: { page.set($0 == .null ? nil : $0, at: path) })) {
                    Text(placeholder).tag(JSONValue.null)
                    ForEach(choices) { c in Text(c.label).tag(c.value) }
                } label: {
                    Text(title).lineLimit(2)
                }
            } else if field.type == "integer" || field.type == "number" {
                labeled {
                    TextField(placeholder, text: numberBinding)
                        .keyboardType(field.type == "integer" ? .numberPad : .decimalPad)
                }
            } else {
                labeled {
                    TextField(placeholder, text: Binding(get: { page.value(path)?.string ?? "" },
                                                         set: { page.set($0.isEmpty ? nil : .string($0), at: path) }))
                        .keyboardType(field.format == "uri" ? .URL : field.format == "email" ? .emailAddress : .default)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
            }
        }
    }

    /// The title above the field.
    private func labeled(@ViewBuilder _ content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text(title).font(Theme.Font.labelStrong).foregroundStyle(.secondary).lineLimit(2)
            #if os(macOS)
            // A Mac form draws a field's title as its label beside an empty box: the title is the prompt here (#1221).
            content().labelsHidden()
            #else
            content()
            #endif
        }
    }

    /// What shows when it is empty: the default it falls back to, or 未设置.
    private var placeholder: String {
        if let d = field.default, let s = field.enum?.first(where: { $0.value == d })?.label ?? d.string, !s.isEmpty {
            return "默认：" + s
        }
        return "未设置"
    }

    /// A number as typed: kept as text until it reads as one (ExtensionModel.numberProblems), then the value.
    private var numberBinding: Binding<String> {
        let key = path.joined(separator: ".")
        return Binding(get: { page.numberText[key] ?? page.value(path)?.string ?? "" }, set: { text in
            page.numberText[key] = text
            let t = text.trimmingCharacters(in: .whitespaces)
            if t.isEmpty { page.set(nil, at: path) } else if let n = Double(t) { page.set(.number(n), at: path) }
        })
    }
}

/// A list of the hub's machines (schema format machine, docs/extensions.md): a switch per machine of the systems the field
/// names, by its name and how it is, in effect at once (`ExtensionModel.saveNow`), the same setting as where else it
/// shows (the 操控电脑 switch on a Mac's page). A value that names no machine still shows, to be turned off.
private struct ExtensionMachineSwitches: View {
    @Environment(AppModel.self) private var model
    var field: ExtensionField
    var title: String
    var path: [String]
    @Bindable var page: ExtensionModel

    private var items: [String] { page.value(path)?.array?.compactMap(\.string) ?? [] }
    /// A machine waiting on the field's x-confirm before it is turned on, and its name (kept while the alert goes away).
    @State private var asking: String?
    @State private var askingName = ""
    /// The page's hub's machines (the app's first group), of the systems asked for.
    private var machines: [Machine] {
        let os = field.items?.os ?? []
        return model.machines.filter { $0.group.isEmpty && (os.isEmpty || os.contains($0.os ?? "")) }
    }
    private var unknown: [String] { items.filter { id in !model.machines.contains { $0.group.isEmpty && $0.id == id } } }
    /// A plugin for operating computers (extension point computer) or their browsers (browser): the rows say how Cua
    /// Driver or ego lite is there too.
    private var computers: Bool { page.detail?.plugin.contributes?["computer"] != nil }
    private var browsers: Bool { page.detail?.plugin.contributes?["browser"] != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Text(title).font(Theme.Font.labelStrong).foregroundStyle(.secondary).lineLimit(2)
            if machines.isEmpty && unknown.isEmpty {
                Text(field.items?.os == ["darwin"] ? "这个 hub 上还没有 Mac。" : "这个 hub 上还没有机器。")
                    .font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
            ForEach(machines) { m in
                row(m.id, name: m.name, status: nodeStatus(m) ?? (browsers ? Self.browserStatus(m) : Self.status(m, computers: computers)))
            }
            ForEach(unknown, id: \.self) { id in
                row(id, name: id, status: "找不到这台机器")
            }
        }
        // An alert, not a confirmation dialog: anchored to the row as a popover, that one drops its 取消.
        .alert("打开「\(askingName)」？", isPresented: Binding(get: { asking != nil }, set: { if !$0 { asking = nil } })) {
            Button("取消", role: .cancel) { asking = nil }
            Button("打开") {
                if let id = asking { save(id, on: true) }
                asking = nil
            }
        } message: {
            Text(field.confirm ?? "")
        }
    }

    /// The field picking where the plugin's program runs: a machine switched on says how the program is there.
    private func nodeStatus(_ m: Machine) -> String? {
        guard let detail = page.detail, detail.nodeMachines == path.last, items.contains(m.id) else { return nil }
        let state = detail.plugin.nodes?.first { $0.machineID == m.id }
        return (m.online ? "在线" : "离线") + " · " + ExtensionNodeState.status(state)
    }

    private func save(_ id: String, on: Bool) {
        var next = items.filter { $0 != id }
        if on { next.append(id) }
        Task { await page.saveNow(next.isEmpty ? nil : .array(next.map(JSONValue.string)), at: path) }
    }

    private func row(_ id: String, name: String, status: String) -> some View {
        Toggle(isOn: Binding(get: { items.contains(id) }, set: { on in
            if on, let ask = field.confirm, !ask.isEmpty { (asking, askingName) = (id, name) } else { save(id, on: on) }
        })) {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(name).lineLimit(1)
                Text(status).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .disabled(page.saving)
    }

    /// 在线 / 离线, then how ego lite is on it (就绪 is the switch worth turning on).
    static func browserStatus(_ m: Machine) -> String {
        var parts = [m.online ? "在线" : "离线"]
        if m.online, let b = m.browser, b.checked {
            if b.job?.running == true { parts.append(b.job?.kind == "install" ? "ego 安装中" : "ego 首次设置中") }
            else if !b.installed { parts.append("ego 没装") }
            else if !b.setUp { parts.append("ego 没做完首次设置") }
            else if b.ready { parts.append("ego 就绪") }
            else { parts.append("ego 没跑通") }
        }
        return parts.joined(separator: " · ")
    }

    /// 在线 / 离线, then for a computer plugin how Cua Driver is on it, once its node has said (every Mac's while the
    /// plugin is on, whether or not it is turned on for it).
    static func status(_ m: Machine, computers: Bool) -> String {
        var parts = [m.online ? "在线" : "离线"]
        if computers, m.online, let c = m.computer, c.checked {
            if !c.installed {
                parts.append("Cua 没装")
            } else if !c.daemon {
                parts.append("Cua 没在运行")
            } else if !c.missingPermissions.isEmpty {
                parts.append("Cua 缺权限")
            } else if c.desktop == "locked" {
                parts.append("锁屏了")
            } else if c.ready {
                parts.append("Cua 可以用")
            } else {
                parts.append("Cua 已装")
            }
        }
        return parts.joined(separator: " · ")
    }
}

/// A list of texts (each editable, added and removed), or of choices (a switch each).
private struct ExtensionListField: View {
    var field: ExtensionField
    var title: String
    var path: [String]
    @Bindable var page: ExtensionModel
    @State private var adding = ""

    private var items: [String] { page.value(path)?.array?.compactMap(\.string) ?? field.default?.array?.compactMap(\.string) ?? [] }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Text(title).font(Theme.Font.labelStrong).foregroundStyle(.secondary).lineLimit(2)
            if let choices = field.items?.enum {
                ForEach(choices) { c in
                    Toggle(isOn: Binding(get: { items.contains(c.value.string ?? "") }, set: { on in
                        let v = c.value.string ?? ""
                        var next = items.filter { $0 != v }
                        if on { next.append(v) }
                        store(next)
                    })) { Text(c.label).lineLimit(1) }
                }
            } else {
                ForEach(Array(items.enumerated()), id: \.offset) { i, item in
                    HStack(spacing: Theme.Space.s) {
                        Text(item).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
                        Button(role: .destructive) {
                            var next = items
                            next.remove(at: i)
                            store(next)
                        } label: {
                            Image(systemName: Theme.Icon.clear).foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("删除 \(item)")
                    }
                }
                HStack(spacing: Theme.Space.s) {
                    TextField("添加一项", text: $adding)
                        #if os(macOS)
                        .labelsHidden() // the title is the prompt (#1221)
                        #endif
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onSubmit(add)
                    Button(action: add) { Image(systemName: Theme.Icon.add) }
                        .buttonStyle(.plain)
                        .foregroundStyle(Theme.accent)
                        .disabled(adding.trimmingCharacters(in: .whitespaces).isEmpty)
                        .accessibilityLabel("添加")
                }
            }
        }
    }

    private func add() {
        let v = adding.trimmingCharacters(in: .whitespaces)
        guard !v.isEmpty else { return }
        store(items + [v])
        adding = ""
    }

    private func store(_ list: [String]) {
        page.set(list.isEmpty ? nil : .array(list.map(JSONValue.string)), at: path)
    }
}

/// Sets one of a plugin's secrets: typed in hidden, saved to the hub, never shown again; one set can be cleared.
private struct ExtensionSecretSheet: View {
    var secret: ExtensionSecret
    var save: (String) async -> Bool
    var clear: () async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var value = ""
    @State private var busy = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    SecretField(secret.set ? "输入新的值替换" : "输入", filled: "已填写，点保存", text: $value, identifier: "plugin-secret-value")
                } footer: {
                    Text(secret.description?.isEmpty == false ? secret.description! : "存在 hub 上，只交给这个扩展。")
                }
                if secret.set {
                    Section {
                        Button(role: .destructive) {
                            Task { busy = true; await clear(); busy = false; dismiss() }
                        } label: {
                            Text("清除").lineLimit(1)
                        }
                        .tint(Theme.danger) // a Mac form draws a button in the accent whatever its role (#1512)
                        .disabled(busy)
                    }
                }
            }
            .themedPage()
            .navigationTitle(secret.title)
            .navigationBarTitleDisplayMode(.inline)
            .sheetCancel(unsaved: !value.isEmpty) { dismiss() }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        Task {
                            busy = true
                            if await save(value) { dismiss() }
                            busy = false
                        }
                    } label: {
                        if busy { ProgressView() } else { Text("保存").lineLimit(1) }
                    }
                    .disabled(busy || value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    .accessibilityIdentifier("plugin-secret-save")
                }
            }
        }
        .presentationDetents([.medium])
        #if os(macOS)
        .macSheet()
        #endif
    }
}
