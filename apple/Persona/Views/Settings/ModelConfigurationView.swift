import BeautifulUI
import SwiftUI

struct ModelPurposeRoute: Hashable, Identifiable { var id: String }

/// The hub supplies the catalog so newly added consumers appear without another client-side list.
struct ModelConfigurationView: View {
    /// In 模型配置's own sheet (`ModelConfigurationLink`). A Mac sheet does not push (it shrank and cut the form off,
    /// #1170): there a category opens over it in a sheet of its own (#1248). In 设置 it pushes, as on iOS.
    var inSheet = false
    @State private var page = ModelConfigurationModel()
    @State private var scopeExpanded = false
    @State private var editing: ModelPurposeRoute?

    var body: some View {
        SettingsForm {
            if let error = page.error {
                Section {
                    Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
                    Button("重新加载") { Task { await page.load() } }.lineLimit(1)
                }
            }
            if page.loading && !page.loaded {
                ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xl)
            }
            if !page.categories.isEmpty {
                Section {
                    ForEach(page.categories) { category in
                        categoryLink(category)
                            .accessibilityIdentifier("model-purpose-\(category.id)")
                    }
                }
                Section {
                    DisclosureGroup(isExpanded: $scopeExpanded) {
                        VStack(alignment: .leading, spacing: Theme.Space.l) {
                            ForEach(page.categories) { category in
                                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                                    Text(category.name).font(Theme.Font.labelStrong)
                                    Text((category.uses ?? []).map(\.name).joined(separator: "、"))
                                        .font(Theme.Font.label).foregroundStyle(.secondary)
                                }
                            }
                        }
                        .padding(.vertical, Theme.Space.s)
                    } label: {
                        Text("适用范围").font(Theme.Font.body).lineLimit(1)
                    }
                    .accessibilityIdentifier("model-config-scope")
                } footer: {
                    Text("修改一个分类，将统一用于该分类的所有用途；从下一次运行生效，不打断当前任务。")
                        .font(Theme.Font.caption)
                }
            } else if page.loaded && page.error == nil {
                EmptyStateView(symbol: Theme.Icon.model, title: String(localized: "暂无模型配置"), message: String(localized: "连接支持四类模型配置的 hub 后，可在这里统一设置。"))
                    .listRowBackground(Color.clear)
            }
        }
        .themedPage()
        .detailTitle(String(localized: "模型配置"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await page.load() }
        .refreshable { await page.load() }
        #if os(macOS)
        .sheet(item: $editing, onDismiss: { Task { await page.load() } }) { route in
            NavigationStack { ModelPurposeEditorView(purposeID: route.id, inSheet: true) }
                .macSheet()
        }
        #endif
    }

    @ViewBuilder private func categoryLink(_ category: ModelPurpose) -> some View {
        #if os(macOS)
        if inSheet {
            Button { editing = ModelPurposeRoute(id: category.id) } label: {
                HStack(spacing: Theme.Space.s) {
                    categoryLabel(category)
                    Spacer(minLength: Theme.Space.s)
                    Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
        } else {
            NavigationLink(value: ModelPurposeRoute(id: category.id)) { categoryLabel(category) }
        }
        #else
        NavigationLink(value: ModelPurposeRoute(id: category.id)) { categoryLabel(category) }
        #endif
    }

    private func categoryLabel(_ category: ModelPurpose) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            Text(category.name).font(Theme.Font.body).lineLimit(1)
            Text(page.modelSummary(category))
                .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            if let limit = category.limit {
                ModelLimitLine(limit: limit, font: Theme.Font.caption)
            } else if let status = category.status, !status.isEmpty, status != "未验证" { // i18n-ignore: status value from the hub
                Text(status).font(Theme.Font.caption).foregroundStyle(Theme.cautionText)
            }
        }
        .padding(.vertical, Theme.Space.xxs)
    }
}

/// A use whose connection the hub has paused (quota used up, busy): a warning, counted down while it shows.
private struct ModelLimitLine: View {
    var limit: ModelLimit
    var font: Font

    var body: some View {
        TimelineView(.periodic(from: .now, by: limit.until.timeIntervalSinceNow < 90 ? 1 : 30)) { context in
            Label {
                Text(limit.text(now: context.date)).foregroundStyle(Theme.cautionText)
            } icon: {
                Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution)
            }
            .font(font).lineLimit(2)
        }
        .accessibilityIdentifier("model-limit")
    }
}

struct ModelPurposeEditorView: View {
    var purposeID: String
    /// In a sheet of its own. On a Mac that sheet's bottom row is the way out: 取消 (asking first when there are edits)
    /// and 保存, which closes it once saved (#1248).
    var inSheet = false
    @Environment(\.dismiss) private var dismiss
    @State private var page = ModelConfigurationModel()
    @State private var confirmReload = false
    /// A launcher provider's /model list (#943) for `listFor`; nil: none to pick from (not loaded, older hub, none).
    @State private var launcherList: LauncherModels?
    @State private var listFor = ""
    @State private var listLoading = false
    @State private var listError: String?

    var body: some View {
        SettingsForm {
            if let purpose = page.purpose {
                Section("当前生效") {
                    LabeledContent("模型", value: page.summary(purpose).isEmpty ? String(localized: "未配置") : page.summary(purpose))
                    if let source = purpose.inheritedFrom, !source.isEmpty {
                        LabeledContent("继承来源", value: source)
                    }
                    if let limit = purpose.limit {
                        ModelLimitLine(limit: limit, font: Theme.Font.label)
                    } else if let status = purpose.status, !status.isEmpty {
                        Text(status).font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                }
                Section {
                    if purpose.supports("provider") { providerPicker }
                    if purpose.supports("model") {
                        if purpose.kind == "plugin" { pluginModelPicker }
                        else if let choice = launcherChoice {
                            launcherModelRow(choice)
                        } else if listLoading && listFor == listProvider {
                            HStack {
                                Text("具体模型")
                                Spacer()
                                Text("正在读取模型列表…").foregroundStyle(.secondary).lineLimit(1)
                                ProgressView().controlSize(.small)
                            }
                        } else {
                            #if os(macOS)
                            // A Mac form: the field's name on the left, what to type in the box (#1221).
                            TextField("具体模型", text: optional(\.model), prompt: Text("留空跟随默认"))
                                .font(Theme.Font.body.monospaced())
                                .accessibilityIdentifier("model-config-model")
                            #else
                            TextField("具体模型（留空跟随默认）", text: optional(\.model))
                                .font(Theme.Font.body.monospaced())
                                .textInputAutocapitalization(.never).autocorrectionDisabled()
                                .accessibilityIdentifier("model-config-model")
                            #endif
                            if let listError, listFor == listProvider {
                                HStack {
                                    Text("模型列表没读到：\(listError)").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                                    Spacer(minLength: Theme.Space.s)
                                    Button("重试") { Task { await loadList(refresh: true) } }.buttonStyle(.borderless).lineLimit(1)
                                }
                            }
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
                        .lineLimit(1)
                    }
                    if !page.runtimes.isEmpty { runtimePicker }
                } header: {
                    Text("模型设置")
                } footer: {
                    Text("此分类的所有用途统一使用这项配置。留空使用现有默认；更换供应商会清除旧模型和思考强度。自定义模型 ID 需要供应商支持。")
                }
                .disabled(page.saving || page.loading)
                Section {
                    Button {
                        page.restoreInheritance()
                    } label: {
                        Label("恢复默认", systemImage: Theme.Icon.undo).lineLimit(1)
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
        .detailTitle(page.purpose?.name ?? String(localized: "模型配置"))
        .navigationBarTitleDisplayMode(.inline)
        .pageToolbar {
            #if os(macOS)
            if inSheet {
                ToolbarItem(placement: .confirmationAction) { saveButton }
            } else {
                ToolbarItem(placement: .topBarTrailing) { saveButton }
            }
            #else
            ToolbarItem(placement: .topBarTrailing) { saveButton }
            #endif
        }
        #if os(macOS)
        .sheetCancel(unsaved: page.dirty, when: inSheet) { dismiss() }
        #endif
        .task(id: purposeID) { await page.load(purposeID: purposeID) }
        .task(id: listProvider) { await loadList() }
        .confirmationDialog("重新加载会丢弃未保存的编辑。", isPresented: $confirmReload, titleVisibility: .visible) {
            Button("重新加载", role: .destructive) { Task { await page.load(purposeID: purposeID) } }
        }
    }

    private var saveButton: some View {
        Button {
            Task {
                if await page.save() {
                    Haptics.success()
                    #if os(macOS)
                    if inSheet { dismiss() }
                    #endif
                } else {
                    Haptics.error()
                }
            }
        } label: {
            if page.saving { ProgressView() } else { Text("保存").lineLimit(1) }
        }
        .disabled(page.purpose == nil || !page.dirty || page.saving || page.loading || page.conflict)
        .accessibilityIdentifier("model-config-save")
    }

    /// The launcher provider whose /model list 具体模型 picks from; "": a plugin's, an API's, none.
    private var listProvider: String {
        guard let purpose = page.purpose, purpose.kind != "plugin", purpose.supports("model") else { return "" }
        let id = page.draft.provider ?? purpose.effective.provider
        return page.providers.first(where: { $0.id == id && $0.kind == "launcher" })?.id ?? ""
    }

    /// The list as a choice: 默认 is leaving it empty, which inherits while the provider does too.
    private var launcherChoice: LauncherModelChoice? {
        guard let list = launcherList, listFor == listProvider, !list.models.isEmpty, let purpose = page.purpose else { return nil }
        let inherited = page.draft.provider == nil && (purpose.override.model ?? "").isEmpty ? purpose.effective.model ?? "" : ""
        return LauncherModelChoice(list: list, picked: page.draft.model ?? "", configured: inherited, configuredLabel: String(localized: "继承"))
    }

    @ViewBuilder private func launcherModelRow(_ choice: LauncherModelChoice) -> some View {
        if let locked = choice.list.locked {
            // The launcher passes its own --model: a pick would not reach it (saving one fails the hub's check).
            LabeledContent("具体模型") {
                VStack(alignment: .trailing, spacing: Theme.Space.xxs) {
                    HStack(spacing: Theme.Space.xs) {
                        Text(choice.unlisted ?? String(localized: "默认")).lineLimit(1)
                        Image(systemName: Theme.Icon.locked).foregroundStyle(.secondary)
                    }
                    Text(locked).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                }
            }
            .accessibilityIdentifier("model-config-model-locked")
        } else {
            unlockedModelRow(choice)
        }
    }

    /// Footer under the model list: what leaving it empty does, and which computer the list came from.
    private var defaultModelFooter: String {
        let source = launcherList?.machine.map { String(localized: "（来自 \($0)）") } ?? ""
        return page.draft.provider == nil
            ? String(localized: "留空：继承上一级的设置。列表和在终端里输入 /model 看到的一样\(source)。")
            : String(localized: "留空：用这个供应商的默认模型。列表和在终端里输入 /model 看到的一样\(source)。")
    }

    private func unlockedModelRow(_ choice: LauncherModelChoice) -> some View {
        NavigationLink {
            LauncherModelPicker(choice: choice, picked: optional(\.model), refresh: { await loadList(refresh: true) }, allowsCustom: true,
                                defaultFooter: defaultModelFooter)
        } label: {
            LabeledContent("具体模型") {
                VStack(alignment: .trailing, spacing: Theme.Space.xxs) {
                    Text(choice.selected?.displayName ?? choice.unlisted.map { SessionModes.gptModelName($0) ?? $0 } ?? String(localized: "默认")).lineLimit(1)
                    if let detail = choice.unlisted != nil ? String(localized: "列表里没有") : choice.detail {
                        Text(detail).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                    }
                }
            }
        }
        .accessibilityIdentifier("model-config-model-list")
    }

    private func loadList(refresh: Bool = false) async {
        let provider = listProvider
        if !refresh, listFor == provider, launcherList != nil || listLoading { return }
        if listFor != provider { launcherList = nil }
        listFor = provider
        listError = nil
        guard !provider.isEmpty, let api = AppModel.shared.api else { return }
        listLoading = true
        defer { if listFor == provider { listLoading = false } }
        do {
            let list = try await api.providerModels(provider, refresh: refresh, machine: page.purpose?.machineID)
            guard listFor == provider else { return }
            launcherList = list
        } catch {
            guard listFor == provider, !Task.isCancelled else { return }
            listError = error.shown
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
        .lineLimit(1)
        .accessibilityIdentifier("model-config-provider")
    }

    /// 主对话's 运行方式 (#2455): the Claude Code CLI or the Agent SDK; "" follows the default (the CLI).
    private var runtimePicker: some View {
        Picker("运行方式", selection: optional(\.runtime)) {
            Text("跟随默认").tag("")
            ForEach(page.runtimes, id: \.self) { Text(AssistantRuntime.title($0)).tag($0) }
        }
        .lineLimit(1)
        .accessibilityIdentifier("model-config-runtime")
    }

    private var pluginModelPicker: some View {
        let id = page.draft.provider ?? page.purpose?.effective.provider
        let models = page.providers.first { $0.id == id }?.models ?? []
        return Picker("具体模型", selection: optional(\.model)) {
            Text("跟随扩展默认").tag("")
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

extension View {
    /// 模型配置 over this page as a sheet: one purpose's editor (`purposeID`), or the categories (nil). A sheet avoids
    /// mixing value and view navigation in the stack below (the assistant's and tasks' details, the inbox).
    func modelConfigurationSheet(isPresented: Binding<Bool>, purposeID: String?) -> some View {
        modifier(ModelConfigurationSheet(showing: isPresented, purposeID: purposeID))
    }
}

private struct ModelConfigurationSheet: ViewModifier {
    @Binding var showing: Bool
    var purposeID: String?

    func body(content: Content) -> some View {
        content.sheet(isPresented: $showing) {
            #if os(macOS)
            // No leading 关闭 and no pushing in a Mac sheet (#1248): the editor brings its own 取消 and 保存 for the
            // sheet's bottom row, and the categories open each one over this sheet.
            NavigationStack {
                if let purposeID {
                    ModelPurposeEditorView(purposeID: purposeID, inSheet: true)
                } else {
                    ModelConfigurationView(inSheet: true).macSheetDone { showing = false }
                }
            }
            .macSheet()
            #else
            NavigationStack {
                Group {
                    if let purposeID { ModelPurposeEditorView(purposeID: purposeID) } else { ModelConfigurationView() }
                }
                .navigationDestination(for: ModelPurposeRoute.self) { ModelPurposeEditorView(purposeID: $0.id) }
                .pageToolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("关闭") { showing = false }.lineLimit(1)
                    }
                }
            }
            #endif
        }
    }
}

/// A row that opens 模型配置 (or one purpose's editor) as a sheet (`modelConfigurationSheet`).
struct ModelConfigurationLink: View {
    var title = String(localized: "模型配置")
    var purposeID: String?
    var summary: String?
    @State private var showing = false

    var body: some View {
        Button { showing = true } label: {
            if AppStyle.current.usesBeautifulUI {
                BUIListRow(title) { BUIIconTile(Theme.Icon.model, tint: Theme.accent) } trailing: {
                    Text(summary ?? String(localized: "统一管理")).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
            } else {
                LabeledContent {
                    Text(summary ?? String(localized: "统一管理")).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                } label: {
                    Label(title, systemImage: Theme.Icon.model).lineLimit(1)
                }
                .lineLimit(1)
            }
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("model-config-link")
        .modelConfigurationSheet(isPresented: $showing, purposeID: purposeID)
        #if DEBUG
        // `-showModelConfigSheet <purpose id>|all`: this sheet open, for screenshots.
        .task {
            guard let want = UserDefaults.standard.string(forKey: "showModelConfigSheet"), want == (purposeID ?? "all") else { return }
            try? await Task.sleep(for: .seconds(1))
            showing = true
        }
        #endif
    }
}
