import SwiftUI

/// Pushes 设置 › 模型 (value-based, like the other settings pages).
struct ModelLaunchersRoute: Hashable {}
/// One launcher's page.
struct ModelLauncherRoute: Hashable { var name: String }

/// 设置 › 模型 (docs/launchers.md): every launcher the machines can start — added here (Jarvis keeps the gateway, model
/// and key) or found in a machine's ~/.zshrc (synced each time this opens) — and the hidden ones.
struct ModelLaunchersView: View {
    @Environment(AppModel.self) private var model
    @State private var launchers: [LauncherView]?
    @State private var error: String?
    @State private var adding = false

    var body: some View {
        List {
            if let launchers {
                let managed = launchers.filter(\.managed)
                let zshrc = launchers.filter { !$0.managed && $0.hidden != true }
                let hidden = launchers.filter { !$0.managed && $0.hidden == true }
                Section {
                    if managed.isEmpty {
                        Text("还没有。点右上角 + 加一个：填网关地址、Key 和模型，所有电脑都能用。")
                            .font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                    ForEach(managed) { row($0) }
                } header: { header("Jarvis 管理", managed.count) } footer: {
                    if !managed.isEmpty { Text("Key 加密存在 hub，启动时才交给那台电脑，不写进文件。") }
                }
                if !zshrc.isEmpty {
                    Section {
                        ForEach(zshrc) { row($0) }
                    } header: { header("来自 ~/.zshrc", zshrc.count) } footer: {
                        Text("各台电脑 ~/.zshrc 里调用 claude 的函数，每次打开这页时同步；新加的函数会自动出现。")
                    }
                }
                if !hidden.isEmpty {
                    Section {
                        ForEach(hidden) { row($0) }
                    } header: { header("已隐藏", hidden.count) } footer: {
                        Text("只是在 Jarvis 里不显示，~/.zshrc 没有改。点进去可以恢复。")
                    }
                }
            } else if let error {
                Section { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity).padding(.vertical, 20) }.listRowBackground(Color.clear)
            }
        }
        .navigationTitle("模型")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { adding = true } label: { Image(systemName: "plus") }.accessibilityLabel("新增启动器")
            }
        }
        .sheet(isPresented: $adding) {
            LauncherEditor(existing: nil) { await load() }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private func header(_ title: String, _ count: Int) -> some View {
        HStack(spacing: Theme.Space.xs) {
            Text(title)
            Text("\(count)").monospacedDigit().foregroundStyle(Color.secondary)
        }
    }

    private func row(_ l: LauncherView) -> some View {
        NavigationLink(value: ModelLauncherRoute(name: l.name)) {
            LauncherRow(launcher: l, machines: machineNames(l))
        }
    }

    private func machineNames(_ l: LauncherView) -> String? {
        guard let ids = l.machines, !ids.isEmpty else { return nil }
        return ids.map { model.machine($0)?.name ?? $0 }.joined(separator: "、")
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            launchers = try await api.allLaunchers()
            error = nil
        } catch {
            self.error = error.shown
        }
    }
}

/// A launcher in the list: its name, the model, where it goes, which machines have it.
struct LauncherRow: View {
    var launcher: LauncherView
    var machines: String?

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.sm) {
                Text(launcher.name).font(Theme.Font.heading.monospaced()).lineLimit(1)
                if launcher.managed { Chip(text: "Jarvis", look: .tint(Theme.accent)) }
            }
            HStack(spacing: Theme.Space.sm) {
                if let m = Fmt.model(launcher.model) ?? launcher.model, !m.isEmpty { Chip(text: m) }
                if let host = launcher.baseURL.flatMap({ URL(string: $0)?.host() }) {
                    Text(host).font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(1)
                }
            }
            if let machines {
                Label(machines, systemImage: Theme.Icon.machines).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .padding(.vertical, 2)
    }
}

/// One launcher: its settings; a Jarvis one can be changed or deleted, a ~/.zshrc one shows its function (keys
/// masked) on each machine and can be hidden or brought back.
struct ModelLauncherDetailView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var name: String
    @State private var launcher: LauncherView?
    @State private var error: String?
    @State private var editing = false
    @State private var confirmDelete = false

    var body: some View {
        List {
            if let l = launcher {
                Section {
                    LabeledContent("来源", value: l.managed ? "Jarvis 管理" : "~/.zshrc 里的函数")
                    if let u = l.baseURL, !u.isEmpty { mono("Base URL", u) }
                    if let m = l.model, !m.isEmpty { mono("模型", m) }
                    if let s = l.smallModel, !s.isEmpty { mono("小模型", s) }
                    if l.managed { LabeledContent("API Key", value: l.hasKey == true ? "已设置（不显示）" : "未设置") }
                    if let a = l.args, !a.isEmpty { mono("额外参数", a.joined(separator: " ")) }
                    if let ids = l.machines, !ids.isEmpty {
                        LabeledContent("电脑", value: ids.map { model.machine($0)?.name ?? $0 }.joined(separator: "、"))
                    }
                }
                if l.managed {
                    Section {
                        Text("在任何电脑上都以 `jarvisd launch \(l.name)` 运行：从本机的 node 取到这些设置再启动 claude。")
                            .font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                }
                ForEach((l.definitions ?? [:]).sorted(by: { $0.key < $1.key }), id: \.key) { id, def in
                    Section {
                        Text(def).font(.caption.monospaced()).textSelection(.enabled)
                    } header: { Text("\(model.machine(id)?.name ?? id) 上的定义") } footer: {
                        Text("看起来像 Key、Token 的值已经换成 •••。")
                    }
                }
                Section {
                    if l.hidden == true {
                        Button { Task { await restore() } } label: { Label("恢复显示", systemImage: "eye").lineLimit(1) }
                    } else {
                        Button(role: .destructive) { confirmDelete = true } label: {
                            Label(l.managed ? "删除启动器" : "在 Jarvis 里隐藏", systemImage: l.managed ? "trash" : "eye.slash").lineLimit(1)
                        }
                    }
                }
            } else if let error {
                Section { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity).padding(.vertical, 20) }.listRowBackground(Color.clear)
            }
        }
        .navigationTitle(name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if launcher?.managed == true {
                ToolbarItem(placement: .topBarTrailing) { Button("编辑") { editing = true }.lineLimit(1) }
            }
        }
        .sheet(isPresented: $editing) {
            LauncherEditor(existing: launcher) { await load() }
        }
        .confirmationDialog(launcher?.managed == true ? "删除 \(name)？" : "在 Jarvis 里隐藏 \(name)？", isPresented: $confirmDelete,
                            titleVisibility: .visible) {
            Button(launcher?.managed == true ? "删除" : "隐藏", role: .destructive) { Task { await delete() } }
        } message: {
            Text(launcher?.managed == true ? "用它的会话下次启动会失败，要换一个启动器。" : "~/.zshrc 不会改；新对话、助手的选择器里不再出现，随时可以恢复。")
        }
        .task { await load() }
    }

    private func mono(_ label: String, _ value: String) -> some View {
        LabeledContent(label) {
            Text(value).font(Theme.Font.body.monospaced()).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
        }
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            launcher = try await api.launcher(name)
            error = nil
        } catch {
            self.error = error.shown
        }
    }

    private func delete() async {
        guard let api = model.api else { return }
        do {
            try await api.deleteLauncher(name)
            model.forgetLaunchers()
            Haptics.success()
            dismiss()
        } catch { model.show(error) }
    }

    private func restore() async {
        guard let api = model.api else { return }
        do {
            try await api.restoreLauncher(name)
            model.forgetLaunchers()
            Haptics.success()
            await load()
        } catch { model.show(error) }
    }
}

/// Add a launcher, or change one Jarvis keeps.
struct LauncherEditor: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var existing: LauncherView?
    var saved: () async -> Void
    @State private var name = ""
    @State private var baseURL = ""
    @State private var apiKey = ""
    @State private var modelName = ""
    @State private var smallModel = ""
    @State private var args = ""
    @State private var saving = false
    @State private var error: String?

    private var isNew: Bool { existing == nil }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("名字，比如 jv_strong", text: $name)
                        .font(Theme.Font.body.monospaced())
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                        .disabled(!isNew)
                } header: { Text("名字") } footer: { Text("字母、数字、_ 和 -；新对话、助手里按这个名字选。") }
                Section {
                    TextField("https://…", text: $baseURL)
                        .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                    SecureField(isNew || existing?.hasKey != true ? "API Key" : "API Key（不改就留空）", text: $apiKey)
                } header: { Text("网关") } footer: { Text("Anthropic Messages 兼容的地址（会设成 ANTHROPIC_BASE_URL）。Key 加密存在 hub。") }
                Section {
                    TextField("模型", text: $modelName).textInputAutocapitalization(.never).autocorrectionDisabled()
                    TextField("小模型（可选，默认同上）", text: $smallModel).textInputAutocapitalization(.never).autocorrectionDisabled()
                } header: { Text("模型") } footer: { Text("模型用于主对话和子 Agent；小模型用于标题、摘要这类轻活。") }
                Section {
                    TextField("比如 --dangerously-skip-permissions", text: $args)
                        .font(Theme.Font.body.monospaced()).textInputAutocapitalization(.never).autocorrectionDisabled()
                } header: { Text("额外参数（可选）") } footer: { Text("加在 claude 后面，空格分隔，每个都以 - 开头。") }
                if let error {
                    Section { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger) }
                }
            }
            .navigationTitle(isNew ? "新增启动器" : "编辑 \(existing?.name ?? "")")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() }.lineLimit(1) }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") { Task { await save() } }.lineLimit(1)
                        .disabled(saving || name.isEmpty || baseURL.isEmpty || modelName.isEmpty || (isNew && apiKey.isEmpty))
                }
            }
            .onAppear {
                guard let e = existing else { return }
                name = e.name
                baseURL = e.baseURL ?? ""
                modelName = e.model ?? ""
                smallModel = e.smallModel ?? ""
                args = (e.args ?? []).joined(separator: " ")
            }
        }
    }

    private func save() async {
        guard let api = model.api else { return }
        saving = true
        defer { saving = false }
        let input = LauncherInput(name: name.trimmingCharacters(in: .whitespaces), baseURL: baseURL.trimmingCharacters(in: .whitespaces),
                                  model: modelName.trimmingCharacters(in: .whitespaces),
                                  smallModel: smallModel.trimmingCharacters(in: .whitespaces),
                                  args: args.split(whereSeparator: \.isWhitespace).map(String.init),
                                  apiKey: apiKey.isEmpty ? nil : apiKey)
        do {
            try await api.saveLauncher(input, isNew: isNew)
            model.forgetLaunchers()
            Haptics.success()
            await saved()
            dismiss()
        } catch {
            self.error = error.shown
            Haptics.error()
        }
    }
}
