import BeautifulUI
import SwiftUI

struct ModelPurposeRoute: Hashable { var id: String }

/// The hub supplies the catalog so newly added consumers appear without another client-side list.
struct ModelConfigurationView: View {
    @State private var page = ModelConfigurationModel()
    @State private var query = ""

    private var groups: [String] {
        page.filtered(query).reduce(into: []) { if !$0.contains($1.group) { $0.append($1.group) } }
    }

    var body: some View {
        List {
            if let error = page.error {
                Section {
                    Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
                    Button("重新加载") { Task { await page.load() } }.lineLimit(1)
                }
            }
            if page.loading && !page.loaded {
                ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xl)
            }
            ForEach(groups, id: \.self) { group in
                Section(group) {
                    ForEach(page.filtered(query).filter { $0.group == group }) { purpose in
                        NavigationLink(value: ModelPurposeRoute(id: purpose.id)) {
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(purpose.name).font(Theme.Font.body).lineLimit(1)
                                Text(page.summary(purpose).isEmpty ? "未配置" : page.summary(purpose))
                                    .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                                if let status = purpose.status, !status.isEmpty {
                                    Text(status).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2)
                                }
                            }
                            .padding(.vertical, Theme.Space.xxs)
                        }
                        .accessibilityIdentifier("model-purpose-\(purpose.id)")
                    }
                }
            }
            if page.loaded && page.error == nil && page.filtered(query).isEmpty {
                EmptyStateView(symbol: Theme.Icon.model, title: query.isEmpty ? "暂无模型用途" : "没有找到", message: "应用内的 AI 功能会在这里统一配置。")
                    .listRowBackground(Color.clear)
            }
            if !page.purposes.isEmpty {
                Section {
                    Text("供应商管理连接和凭据；这里按用途选择模型。未单独设置的项目保留现有默认，修改从下一次任务或对话开始生效。")
                        .font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
        }
        .themedPage()
        .navigationTitle("模型配置")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $query, prompt: "搜索用途、供应商或模型")
        .task { await page.load() }
        .refreshable { await page.load() }
    }
}

struct ModelPurposeEditorView: View {
    var purposeID: String
    @State private var page = ModelConfigurationModel()
    @State private var confirmReload = false

    var body: some View {
        List {
            if let purpose = page.purpose {
                Section("当前生效") {
                    LabeledContent("模型", value: page.summary(purpose).isEmpty ? "未配置" : page.summary(purpose))
                    if let source = purpose.inheritedFrom, !source.isEmpty {
                        LabeledContent("继承来源", value: source)
                    }
                    if let status = purpose.status, !status.isEmpty {
                        Text(status).font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                }
                Section {
                    if purpose.supports("provider") { providerPicker }
                    if purpose.supports("model") {
                        if purpose.kind == "plugin" { pluginModelPicker }
                        else {
                            TextField("具体模型（留空跟随默认）", text: optional(\.model))
                                .font(Theme.Font.body.monospaced())
                                .textInputAutocapitalization(.never).autocorrectionDisabled()
                                .accessibilityIdentifier("model-config-model")
                        }
                    }
                    if purpose.supports("effort") && allowsEffort {
                        Picker("思考强度", selection: optional(\.effort)) {
                            Text("跟随默认").tag("")
                            ForEach(SessionModes.effortLevels) { Text($0.title).tag($0.id) }
                            if let effort = page.draft.effort, !SessionModes.effortLevels.contains(where: { $0.id == effort }) {
                                Text(effort).tag(effort)
                            }
                        }
                    }
                } header: {
                    Text("此用途的设置")
                } footer: {
                    Text("留空表示继承。更换供应商会清除旧模型和思考强度，避免把不同供应商的参数混在一起。自定义模型 ID 需要供应商支持。")
                }
                .disabled(page.saving || page.loading)
                Section {
                    Button {
                        page.restoreInheritance()
                    } label: {
                        Label("恢复继承", systemImage: Theme.Icon.undo).lineLimit(1)
                    }
                    .disabled(page.saving || page.loading || page.draft.isEmpty)
                } footer: {
                    Text("保存后从下一次运行生效，不会打断当前任务，也不会重新处理历史数据。权限设置不会随模型改变。")
                }
            } else if page.loading {
                ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xl)
            }
            if let error = page.error {
                Section {
                    Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
                    Button("重新加载") { reload() }.lineLimit(1)
                        .disabled(page.saving || page.loading)
                }
            }
        }
        .themedPage()
        .navigationTitle(page.purpose?.name ?? "模型配置")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    Task { if await page.save() { Haptics.success() } else { Haptics.error() } }
                } label: {
                    if page.saving { ProgressView() } else { Text("保存").lineLimit(1) }
                }
                .disabled(page.purpose == nil || !page.dirty || page.saving || page.loading || page.conflict)
                .accessibilityIdentifier("model-config-save")
            }
        }
        .task(id: purposeID) { await page.load(purposeID: purposeID) }
        .confirmationDialog("重新加载会丢弃未保存的编辑。", isPresented: $confirmReload, titleVisibility: .visible) {
            Button("重新加载", role: .destructive) { Task { await page.load(purposeID: purposeID) } }
        }
    }

    private var allowsEffort: Bool {
        let id = page.draft.provider ?? page.purpose?.effective.provider
        guard let provider = page.providers.first(where: { $0.id == id }) else { return true }
        return provider.kind != "api" || provider.api == "responses"
    }

    private var providerPicker: some View {
        Picker("模型供应商", selection: Binding(get: { page.draft.provider ?? "" }, set: { page.chooseProvider($0) })) {
            Text("跟随默认").tag("")
            ForEach(page.candidates) { provider in
                Text(provider.name).tag(provider.id)
            }
            if let id = page.draft.provider, !page.candidates.contains(where: { $0.id == id }) {
                Text("\(id)（当前配置）").tag(id)
            }
        }
        .accessibilityIdentifier("model-config-provider")
    }

    private var pluginModelPicker: some View {
        let id = page.draft.provider ?? page.purpose?.effective.provider
        let models = page.providers.first { $0.id == id }?.models ?? []
        return Picker("具体模型", selection: optional(\.model)) {
            Text("跟随插件默认").tag("")
            ForEach(models) { Text($0.name ?? $0.id).tag($0.id) }
            if let model = page.draft.model, !models.contains(where: { $0.id == model }) {
                Text("\(model)（当前配置）").tag(model)
            }
        }
        .lineLimit(1)
    }

    private func optional(_ key: WritableKeyPath<ModelBinding, String?>) -> Binding<String> {
        Binding(get: { page.draft[keyPath: key] ?? "" }, set: { page.draft[keyPath: key] = $0.isEmpty ? nil : $0 })
    }

    private func reload() {
        if page.dirty { confirmReload = true } else { Task { await page.load(purposeID: purposeID) } }
    }
}

/// A sheet avoids mixing value and view navigation in the existing assistant/task detail stacks.
struct ModelConfigurationLink: View {
    var title = "模型配置"
    var purposeID: String?
    var summary: String?
    @State private var showing = false

    var body: some View {
        Button { showing = true } label: {
            if AppStyle.current.usesBeautifulUI {
                BUIListRow(title) { BUIIconTile(Theme.Icon.model, tint: Theme.accent) } trailing: {
                    Text(summary ?? "统一管理").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
            } else {
                LabeledContent {
                    Text(summary ?? "统一管理").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                } label: {
                    Label(title, systemImage: Theme.Icon.model).lineLimit(1)
                }
                .lineLimit(1)
            }
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("model-config-link")
        .sheet(isPresented: $showing) {
            NavigationStack {
                Group {
                    if let purposeID { ModelPurposeEditorView(purposeID: purposeID) } else { ModelConfigurationView() }
                }
                .navigationDestination(for: ModelPurposeRoute.self) { ModelPurposeEditorView(purposeID: $0.id) }
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("关闭") { showing = false }.lineLimit(1)
                    }
                }
            }
            #if os(macOS)
            .macSheet()
            #endif
        }
    }
}
