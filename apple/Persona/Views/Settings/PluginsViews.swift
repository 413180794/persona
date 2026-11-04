import SwiftUI

/// 设置 › 插件: every plugin of the hub's (built in, from hub.json, installed on the hub's machine), its state, and
/// the way to its page. Plugins are installed on the hub's side only (docs/plugins.md「安装」): with `personad plugin` on
/// its machine or in its admin UI (/admin); the footer says how.
struct PluginsView: View {
    @State private var page = PluginsModel()

    var body: some View {
        SettingsForm {
            if let plugins = page.plugins, plugins.isEmpty {
                Section {
                    EmptyStateView(symbol: Theme.Icon.plugins, title: "没有插件",
                                   message: "插件在 hub 的管理界面（/admin）或 hub 所在的机器上用 personad plugin install 安装，装好后在这里开关和设置。")
                        .listRowBackground(Color.clear)
                }
            } else if let plugins = page.plugins {
                Section {
                    Group {
                        ForEach(plugins) { p in
                            NavigationLink(value: SettingsPage.plugin(p.id)) { PluginRowLabel(plugin: p) }
                                .accessibilityIdentifier("plugin-\(p.id)")
                        }
                    }
                    .bareListRow()
                } footer: {
                    Text("插件在 hub 的管理界面（/admin）或 hub 所在的机器上（personad plugin）安装、升级和卸载，这里可以开关、改设置、填密钥。")
                }
            }
            if let error = page.error {
                Section { Text(error).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled) }
            }
        }
        .themedPage()
        .overlay { if page.plugins == nil && page.loading { ProgressView() } }
        .navigationTitle("插件")
        .navigationBarTitleDisplayMode(.inline)
        .task { await page.load() }
        .refreshable { await page.load() }
    }
}

/// A plugin's row: its name, and its state (or version) on the right with a dot that says whether it needs the user.
private struct PluginRowLabel: View {
    var plugin: HubPlugin

    var body: some View {
        if AppStyle.current.usesBeautifulUI {
            SettingsRow(title: plugin.name, symbol: Theme.Icon.plugins, tint: PluginStateDot.tone(plugin), value: PluginText.summary(plugin))
        } else {
            LabeledContent {
                HStack(spacing: Theme.Space.sm) {
                    PluginStateDot(plugin: plugin)
                    Text(PluginText.summary(plugin)).lineLimit(1)
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

/// The dot beside a plugin's state.
private struct PluginStateDot: View {
    var plugin: HubPlugin?

    var body: some View {
        Circle().fill(Self.tone(plugin)).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
    }

    static func tone(_ p: HubPlugin?) -> Color {
        guard let p else { return .secondary }
        if PluginText.isProblem(p) { return Theme.danger }
        switch p.status {
        case "running": return Theme.positive
        case "needs_setup": return Theme.caution
        default: return .secondary
        }
    }
}

/// 设置 › 插件 › one: what it is and how it is doing, its switch, its settings as a form drawn from its schema, its
/// secrets (set here, never shown again), what it contributes and what it may do. A plugin written in hub.json is only
/// looked at here.
struct PluginDetailView: View {
    @State private var page: PluginModel
    @State private var editingSecret: PluginSecret?

    init(id: String) { _page = State(initialValue: PluginModel(id: id)) }

    var body: some View {
        SettingsForm {
            header
            if let d = page.detail {
                if d.readOnly {
                    Section {
                        Text("这个插件写在 hub.json 里：开关和设置在 hub 所在的机器上改。").font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                } else {
                    Section {
                        Toggle(isOn: Binding(get: { d.plugin.enabled ?? true }, set: { on in Task { await page.setEnabled(on) } })) {
                            Text("启用").lineLimit(1)
                        }
                        .accessibilityIdentifier("plugin-enabled")
                    } footer: {
                        Text("停用后它不再运行，提供的连接器等也一起关掉；设置和密钥留着。")
                    }
                }
                if !d.readOnly && !d.form.isEmpty { form(d) }
                if !d.readOnly && !d.secrets.isEmpty { secrets(d) }
                contributions(d)
                permissions(d)
                about(d)
            }
        }
        .themedPage()
        .navigationTitle(page.detail?.plugin.name ?? "插件")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
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
        .sheet(item: $editingSecret) { secret in
            PluginSecretSheet(secret: secret, save: { await page.setSecret(secret.name, value: $0) },
                              clear: { await page.clearSecret(secret.name) })
        }
        .task { await page.load() }
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
                        PluginStateDot(plugin: page.detail?.plugin)
                        Text(stateText).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
                Spacer(minLength: Theme.Space.s)
                if page.loading && page.detail == nil { ProgressView() }
            }
            .padding(.vertical, Theme.Space.xs)
            if let text = page.detail?.plugin.description, !text.isEmpty {
                Text(text).font(Theme.Font.secondary).foregroundStyle(.secondary)
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
        return [PluginText.state(p), p.version, PluginText.source(p.source)].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func form(_ d: HubPluginDetail) -> some View {
        Group {
            Section {
                ForEach(d.form.filter { $0.type != "object" }) { f in PluginFieldRow(field: f, path: [f.name], page: page) }
            } header: {
                SettingsHeading("设置")
            } footer: {
                Text("改完点右上角「保存」；保存后插件会重新启动。留空的项用插件的默认值。")
            }
            ForEach(d.form.filter { $0.type == "object" }) { group in
                Section {
                    ForEach(group.fields ?? []) { f in PluginFieldRow(field: f, path: [group.name, f.name], page: page) }
                } header: {
                    SettingsHeading(group.label)
                } footer: {
                    if let text = group.description, !text.isEmpty { Text(text) }
                }
            }
        }
    }

    private func secrets(_ d: HubPluginDetail) -> some View {
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
            Text("密钥加密存在 hub 上，只交给这个插件；存好后不再显示，只能换新的或清除。")
        }
    }

    @ViewBuilder private func contributions(_ d: HubPluginDetail) -> some View {
        if let c = d.plugin.contributes, !c.isEmpty {
            Section {
                ForEach(c.keys.sorted(), id: \.self) { point in
                    LabeledContent(PluginText.point(point)) {
                        Text((c[point] ?? []).joined(separator: "、")).lineLimit(2).multilineTextAlignment(.trailing)
                    }
                }
            } header: {
                SettingsHeading("提供")
            }
        }
    }

    @ViewBuilder private func permissions(_ d: HubPluginDetail) -> some View {
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

    @ViewBuilder private func about(_ d: HubPluginDetail) -> some View {
        Section {
            if let v = d.plugin.version, !v.isEmpty { LabeledContent("版本", value: v) }
            if let v = d.previousVersion, !v.isEmpty { LabeledContent("上一个版本", value: v) }
            if let s = PluginText.source(d.plugin.source) { LabeledContent("来源", value: s) }
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
private struct PluginFieldRow: View {
    var field: PluginField
    var path: [String]
    @Bindable var page: PluginModel

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            control
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
            PluginListField(field: field, title: title, path: path, page: page)
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

    /// A number as typed: kept as text until it reads as one (PluginModel.numberProblems), then the value.
    private var numberBinding: Binding<String> {
        let key = path.joined(separator: ".")
        return Binding(get: { page.numberText[key] ?? page.value(path)?.string ?? "" }, set: { text in
            page.numberText[key] = text
            let t = text.trimmingCharacters(in: .whitespaces)
            if t.isEmpty { page.set(nil, at: path) } else if let n = Double(t) { page.set(.number(n), at: path) }
        })
    }
}

/// A list of texts (each editable, added and removed), or of choices (a switch each).
private struct PluginListField: View {
    var field: PluginField
    var title: String
    var path: [String]
    @Bindable var page: PluginModel
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
private struct PluginSecretSheet: View {
    var secret: PluginSecret
    var save: (String) async -> Bool
    var clear: () async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var value = ""
    @State private var busy = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    SecureField(secret.set ? "输入新的值替换" : "输入", text: $value)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .accessibilityIdentifier("plugin-secret-value")
                } footer: {
                    Text(secret.description?.isEmpty == false ? secret.description! : "存在 hub 上，只交给这个插件。")
                }
                if secret.set {
                    Section {
                        Button(role: .destructive) {
                            Task { busy = true; await clear(); busy = false; dismiss() }
                        } label: {
                            Text("清除").lineLimit(1)
                        }
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
