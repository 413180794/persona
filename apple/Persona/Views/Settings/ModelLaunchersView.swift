import SwiftUI

/// Pushes 设置 › 模型 (value-based, like the other settings pages).
struct ModelLaunchersRoute: Hashable {}
/// One launcher's page.
struct ModelLauncherRoute: Hashable { var name: String }

/// 设置 › 模型 (docs/launchers.md): every launcher the machines can start — added here (Persona keeps the gateway, model
/// and key) or found in a machine's ~/.zshrc (synced each time this opens) — and the hidden ones.
struct ModelLaunchersView: View {
    @Environment(AppModel.self) private var model
    @State private var launchers: [LauncherView]?
    @State private var error: String?
    @State private var adding = false
    @State private var query = ""
    /// 已隐藏 starts folded (2026-11-01): they are hidden for a reason. A search shows them anyway.
    @State private var showHidden = false
    /// A Persona launcher swiped to 删除, waiting for the confirmation.
    @State private var pendingDelete: LauncherView?

    /// Name, model, gateway host, or a machine's name.
    private func matches(_ l: LauncherView) -> Bool {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return true }
        let machines = (l.machines ?? []).map { model.machine(model.hubRef($0))?.name ?? $0 }
        return ([l.name, l.model ?? "", l.smallModel ?? "", l.baseURL ?? ""] + machines).contains { $0.localizedCaseInsensitiveContains(q) }
    }

    var body: some View {
        SettingsForm {
            if let all = launchers {
                let launchers = all.filter(matches)
                let managed = launchers.filter(\.managed)
                let zshrc = launchers.filter { !$0.managed && $0.hidden != true }
                let hidden = launchers.filter { !$0.managed && $0.hidden == true }
                if !query.isEmpty, launchers.isEmpty {
                    ContentUnavailableView.search(text: query).listRowBackground(Color.clear)
                }
                if query.isEmpty || !managed.isEmpty { Section {
                    if managed.isEmpty {
                        #if os(macOS)
                        Text("还没有。用下面的「新增供应商…」添加接入方式、网关、Key 和默认模型，再在「模型配置」里按用途选择。")
                            .font(Theme.Font.label).foregroundStyle(.secondary)
                        #else
                        Text("还没有。点右上角 + 添加接入方式、网关、Key 和默认模型，再在「模型配置」里按用途选择。")
                            .font(Theme.Font.label).foregroundStyle(.secondary)
                        #endif
                    }
                    ForEach(managed) { row($0) }
                    #if os(macOS)
                    // In the group it adds to, as a Mac settings pane has its add button (#1223): in the window's toolbar
                    // the search field took the trailing end and the + went to the window's middle, over the settings list.
                    HStack {
                        Spacer(minLength: 0)
                        Button("新增供应商…") { adding = true }.lineLimit(1).formButton()
                    }
                    #endif
                } header: { header("Persona 管理", managed.count) } footer: {
                    if !managed.isEmpty { Text("Key 加密存在 hub，启动时才交给那台电脑，不写进文件。") }
                } }
                if !zshrc.isEmpty {
                    Section {
                        ForEach(zshrc) { row($0) }
                    } header: { header("来自 ~/.zshrc", zshrc.count) } footer: {
                        Text("各台电脑 ~/.zshrc 里调用 claude 的函数，每次打开这页时同步；新加的函数会自动出现。")
                    }
                }
                if !hidden.isEmpty {
                    let open = showHidden || !query.isEmpty
                    Section {
                        if open { ForEach(hidden) { row($0) } }
                    } header: {
                        Button {
                            withAnimation(Theme.Motion.standard) { showHidden.toggle() }
                        } label: {
                            HStack(spacing: Theme.Space.xs) {
                                header("已隐藏", hidden.count)
                                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong)
                                    .rotationEffect(.degrees(open ? 90 : 0))
                                Spacer(minLength: 0)
                            }
                            .foregroundStyle(Color.secondary)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .disabled(!query.isEmpty)
                        .accessibilityLabel("已隐藏 \(hidden.count) 个，" + (open ? "已展开" : "已折叠"))
                        .accessibilityAddTraits(.isButton)
                    } footer: {
                        if open { Text("只是在 Persona 里不显示，~/.zshrc 没有改。点进去可以恢复。") }
                    }
                }
            } else if let error {
                Section { LoadProblem(text: error) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xl) }.listRowBackground(Color.clear)
            }
        }
        .detailTitle("模型供应商")
        .navigationBarTitleDisplayMode(.inline)
        .pageSearchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "名字、模型、网关或电脑")
        #if os(iOS) || os(visionOS)
        .pageToolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { adding = true } label: { Image(systemName: Theme.Icon.add) }.accessibilityLabel("新增供应商")
            }
        }
        #endif
        .sheet(isPresented: $adding) {
            // Saving moves `launchersRevision`, which reloads this list below.
            LauncherEditor(existing: nil) {}
        }
        #if os(macOS) && DEBUG
        .onAppear { if UserDefaults.standard.bool(forKey: "macLauncherEditor") { adding = true } } // `-macLauncherEditor YES`: 新增启动器
        #endif
        #if DEBUG
        // `-openLauncherEditor YES` opens 新增供应商, `-openLauncherEditor preset` on 从扩展添加.
        .onAppear { if UserDefaults.standard.string(forKey: "openLauncherEditor") != nil { adding = true } }
        #endif
        .confirmationDialog("删除启动器「\(pendingDelete?.name ?? "")」？", isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } }),
                            titleVisibility: .visible, presenting: pendingDelete) { l in
            Button("删除", role: .destructive) { Task { await delete(l) } }
        } message: { _ in Text("hub 里存的 Key 也一起删掉，不能恢复。") }
        // Again whenever a launcher changes anywhere, its own page included (hide, restore, delete): this page stays
        // built under the one pushed on it, so without this it showed the old list when you came back (#1433).
        .task(id: model.launchersRevision) { await load() }
        .refreshable { await load() }
        #if DEBUG
        .onAppear { if let q = UserDefaults.standard.string(forKey: "showModelSearch") { query = q } } // `-showModelSearch glm`
        #endif
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
        // Swipe left to hide a ~/.zshrc launcher or bring a hidden one back (2026-11-01 user request). No
        // confirmation: it only changes what Persona lists and is undone the same way. A Persona one is deleted, key
        // and all, so that one asks first.
        #if os(macOS)
        // A Mac form has no swipe: the same actions in the row's menu (#1221).
        .contextMenu {
            if l.managed {
                Button("删除…", role: .destructive) { pendingDelete = l }
            } else if l.hidden == true {
                Button("恢复") { Task { await setHidden(l, false) } }
            } else {
                Button("隐藏") { Task { await setHidden(l, true) } }
            }
        }
        #endif
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            if l.managed {
                Button { pendingDelete = l } label: { Label("删除", systemImage: Theme.Icon.delete) }.tint(Theme.danger)
            } else {
                if l.hidden == true {
                    Button { Task { await setHidden(l, false) } } label: { Label("恢复", systemImage: Theme.Icon.view) }
                        .tint(Theme.accent)
                } else {
                    Button { Task { await setHidden(l, true) } } label: { Label("隐藏", systemImage: Theme.Icon.hide) }
                        .tint(Theme.caution)
                }
            }
        }
    }

    /// Takes the row out at once, then asks the hub; a failure puts it back and says why.
    private func delete(_ l: LauncherView) async {
        guard let api = model.api, let i = launchers?.firstIndex(where: { $0.name == l.name }) else { return }
        _ = withAnimation(Theme.Motion.standard) { launchers?.remove(at: i) }
        do {
            try await api.deleteLauncher(l.name)
            model.launchersChanged()
            Haptics.success()
        } catch {
            withAnimation(Theme.Motion.standard) { launchers?.insert(l, at: min(i, launchers?.count ?? 0)) }
            model.show(error)
        }
    }

    /// Moves the row at once, then asks the hub; a failure puts it back and says why.
    private func setHidden(_ l: LauncherView, _ hide: Bool) async {
        guard let api = model.api, let i = launchers?.firstIndex(where: { $0.name == l.name }) else { return }
        withAnimation(Theme.Motion.standard) { launchers?[i].hidden = hide }
        do {
            if hide { try await api.deleteLauncher(l.name) } else { try await api.restoreLauncher(l.name) }
            model.launchersChanged()
            Haptics.success()
        } catch {
            withAnimation(Theme.Motion.standard) { launchers?[i].hidden = l.hidden }
            model.show(error)
        }
    }

    private func machineNames(_ l: LauncherView) -> String? {
        guard let ids = l.machines, !ids.isEmpty else { return nil }
        return ids.map { model.machine(model.hubRef($0))?.name ?? $0 }.joined(separator: "、")
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
                if launcher.managed { Chip(text: "Persona", look: .tint(Theme.accent)) }
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
        .padding(.vertical, Theme.Space.xxs)
    }
}

/// One launcher: its settings; a Persona one can be changed or deleted, a ~/.zshrc one shows its function (keys
/// masked) on each machine and can be hidden or brought back.
struct ModelLauncherDetailView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var name: String
    @State private var launcher: LauncherView?
    @State private var error: String?
    @State private var editing = false
    @State private var confirmDelete = false
    @State private var showDefinition = false
    /// 测试可用性: per machine, running (nil) or done.
    @State private var checks: [String: LauncherCheck?] = [:]
    /// What plugins draw on this page (docs/extensions.md「声明式 UI」), and the link one of them opens.
    @State private var pluginSections: ExtensionLauncherModel?
    @State private var pluginLink: LinkTarget?

    /// Machines whose launcher sets the same, with that detail.
    private struct DetailGroup { var machines: [String]; var names: String; var detail: LauncherDetail }

    private func detailGroups(_ l: LauncherView) -> [DetailGroup] {
        var out: [DetailGroup] = []
        for (id, d) in (l.details ?? [:]).sorted(by: { $0.key < $1.key }) {
            let name = model.machine(model.hubRef(id))?.name ?? id
            if let i = out.firstIndex(where: { $0.detail == d }) {
                out[i].machines.append(id)
                out[i].names += "、" + name
            } else {
                out.append(DetailGroup(machines: [id], names: name, detail: d))
            }
        }
        return out
    }

    private func groups(_ l: LauncherView) -> Int { detailGroups(l).count }

    var body: some View {
        SettingsForm {
            if let l = launcher {
                Section {
                    LabeledContent("来源", value: l.managed ? "Persona 管理" : "~/.zshrc 里的函数")
                    if l.kind == "api" { LabeledContent("接入方式", value: "直接 API · \(l.api == "responses" ? "Responses" : "Messages")") }
                    // A ~/.zshrc launcher's gateway and models are in the sections it was probed into below.
                    if l.managed || (l.details ?? [:]).isEmpty {
                        if let u = l.baseURL, !u.isEmpty { mono("Base URL", u) }
                        if let m = l.model, !m.isEmpty { mono("模型", m) }
                    }
                    if let s = l.smallModel, !s.isEmpty { mono("小模型", s) }
                    if let s = l.subagentModel, !s.isEmpty { mono("子 Agent 模型", s) }
                    if let c = l.customModel, !c.isEmpty {
                        mono("自定义模型", c)
                        if let n = l.customModelName, !n.isEmpty { LabeledContent("自定义模型名", value: n) }
                        if let d = l.customModelDescription, !d.isEmpty { LabeledContent("自定义模型说明", value: d) }
                    }
                    if l.managed { LabeledContent("API Key", value: l.hasKey == true ? "已设置（不显示）" : "未设置") }
                    if l.managed, l.api != "responses" { LabeledContent("认证方式", value: l.bearerAuth ? "Bearer" : "x-api-key") }
                    if let a = l.args, !a.isEmpty { mono("额外参数", a.joined(separator: " ")) }
                    if let ids = l.machines, !ids.isEmpty {
                        LabeledContent("电脑", value: ids.map { model.machine(model.hubRef($0))?.name ?? $0 }.joined(separator: "、"))
                    }
                }
                if l.managed {
                    Section {
                        Text(l.kind == "api" ? "hub 直接请求此 API，不在电脑上启动 Agent。各用途在「模型配置」中选择它。" : "在任何电脑上都以 `personad launch \(l.name)` 运行：从本机的 node 取到这些设置再启动 claude。")
                            .font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                }
                // How much is left comes before trying it: it is why the page is opened more often.
                if l.kind != "api" {
                    LauncherQuotaSection(name: l.name)
                    availability(l)
                }
                if let pluginSections { ExtensionLauncherSections(page: pluginSections, link: $pluginLink) }
                // What the launcher really sets, once for the machines where it is the same.
                ForEach(detailGroups(l), id: \.machines) { g in
                    LauncherDetailSections(detail: g.detail, place: groups(l) > 1 ? g.names : nil)
                }
                if let defs = l.definitions, !defs.isEmpty {
                    Section {
                        DisclosureGroup(isExpanded: $showDefinition) {
                            ForEach(defs.sorted(by: { $0.key < $1.key }), id: \.key) { id, def in
                                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                                    if defs.count > 1 { Text(model.machine(model.hubRef(id))?.name ?? id).font(Theme.Font.labelStrong).foregroundStyle(.secondary) }
                                    Text(def).font(Theme.Font.code).textSelection(.enabled)
                                }
                            }
                        } label: { Label("函数原文", systemImage: Theme.Icon.agents).lineLimit(1) }
                    } footer: { Text("看起来像 Key、Token 的值已经换成 •••。") }
                }
                Section {
                    if l.hidden == true {
                        Button { Task { await restore() } } label: { Label("恢复显示", systemImage: Theme.Icon.view).lineLimit(1) }
                    } else {
                        // The confirmation comes up right where the button is (2026-10-31 user request), not as a sheet.
                        Button(role: .destructive) { confirmDelete = true } label: {
                            Label(l.managed ? "删除启动器" : "在 Persona 里隐藏", systemImage: l.managed ? Theme.Icon.delete : Theme.Icon.hide).lineLimit(1)
                        }
                        .popover(isPresented: $confirmDelete, arrowEdge: .top) {
                            Button(role: .destructive) {
                                confirmDelete = false
                                Task { await delete() }
                            } label: {
                                Text(l.managed ? "确认删除" : "确认隐藏").font(Theme.Font.heading).lineLimit(1)
                                    .padding(.horizontal, Theme.Space.l).padding(.vertical, Theme.Space.m)
                            }
                            .foregroundStyle(Theme.danger)
                            .presentationCompactAdaptation(.popover)
                        }
                    }
                }
            } else if let error {
                Section { LoadProblem(text: error) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xl) }.listRowBackground(Color.clear)
            }
        }
        .detailTitle(name)
        .navigationBarTitleDisplayMode(.inline)
        .pageToolbar {
            if launcher?.managed == true {
                ToolbarItem(placement: .topBarTrailing) { Button("编辑") { editing = true }.lineLimit(1) }
            }
        }
        .sheet(isPresented: $editing) {
            LauncherEditor(existing: launcher) { await load() }
        }
        .pluginLinks($pluginLink)
        .task(id: model.hubFeatures.hasExtensionFeature("manager")) {
            guard model.hubFeatures.hasExtensionFeature("manager") else { return }
            let sections = pluginSections ?? ExtensionLauncherModel(launcher: name)
            pluginSections = sections
            await sections.load()
        }
        .task {
            await load()
            #if DEBUG
            if UserDefaults.standard.bool(forKey: "showDeleteConfirm") { try? await Task.sleep(for: .seconds(1)); confirmDelete = true }
            if UserDefaults.standard.bool(forKey: "showLauncherCheck"), let l = launcher { await check(testMachines(l)) }
            #endif
        }
    }

    private func mono(_ label: String, _ value: String) -> some View {
        LabeledContent(label) {
            Text(value).font(Theme.Font.body.monospaced()).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
        }
    }

    /// The machines to test on: a ~/.zshrc launcher's own (online ones), a Persona one on the hub's machine.
    private func testMachines(_ l: LauncherView) -> [String] {
        if l.managed { return [""] } // the hub picks (the assistant's machine)
        return (l.machines ?? []).filter { model.machine(model.hubRef($0))?.online != false }
    }

    @ViewBuilder private func availability(_ l: LauncherView) -> some View {
        let machines = testMachines(l)
        let running = checks.values.contains { $0 == nil }
        Section {
            ForEach(machines, id: \.self) { id in
                if let entry = checks[id] {
                    LauncherCheckRow(machine: id.isEmpty ? nil : (model.machine(model.hubRef(id))?.name ?? id), check: entry)
                }
            }
            Button { Task { await check(machines) } } label: {
                HStack {
                    Label(checks.isEmpty ? "测试可用性" : "再测一次", systemImage: Theme.Icon.tryOut).lineLimit(1)
                    if running { Spacer(); ProgressView() }
                }
            }
            .disabled(running || machines.isEmpty)
        } header: { Text("可用性") } footer: {
            Text(machines.isEmpty ? "有这个启动器的电脑都不在线。" : "真的发一个只回答一个词的请求（花几个 token），看能不能回答、要多久、是哪个模型答的。")
        }
    }

    private func check(_ machines: [String]) async {
        guard let api = model.api else { return }
        for id in machines { checks[id] = .some(nil) }
        await withTaskGroup(of: (String, LauncherCheck).self) { group in
            for id in machines {
                group.addTask {
                    let c = (try? await api.checkLauncher(name, machine: id.isEmpty ? nil : id))
                        ?? LauncherCheck(ok: false, millis: 0, error: "请求没有完成")
                    return (id, c)
                }
            }
            for await (id, c) in group { checks[id] = .some(c) }
        }
        let allOK = checks.values.allSatisfy { $0?.ok == true }
        if allOK { Haptics.success() } else { Haptics.error() }
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
            model.launchersChanged()
            Haptics.success()
            dismiss()
        } catch { model.show(error) }
    }

    private func restore() async {
        guard let api = model.api else { return }
        do {
            try await api.restoreLauncher(name)
            model.launchersChanged()
            Haptics.success()
            await load()
        } catch { model.show(error) }
    }
}

/// Add a launcher, or change one Persona keeps.
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
    @State private var subagentModel = ""
    @State private var customModel = ""
    @State private var customModelName = ""
    @State private var customModelDescription = ""
    @State private var args = ""
    @State private var kind = "launcher"
    @State private var protocolName = "messages"
    @State private var auth = "api_key"
    @State private var saving = false
    @State private var error: String?
    /// The fields as the sheet opened: 取消 asks first once they differ (Mac).
    @State private var opened: [String]?
    /// 从扩展添加: the providers the hub's plugins offer (only when adding, and only when there are some).
    @State private var presets = LauncherPresetModel()
    @State private var fromExtension = false

    private var isNew: Bool { existing == nil }
    private var usesPreset: Bool { isNew && fromExtension && !presets.presets.isEmpty }
    /// The Responses API always takes the key as Bearer: nothing to choose.
    private var usesResponses: Bool { kind == "api" && protocolName == "responses" }
    /// What was typed in either form (a preset's model is filled in as it loads, so it does not count).
    private var fields: [String] {
        [name, baseURL, apiKey, modelName, smallModel, subagentModel, customModel, customModelName, customModelDescription, args, kind,
         protocolName, auth, presets.name, presets.apiKey]
    }

    var body: some View {
        NavigationStack {
            Form {
                if isNew, !presets.presets.isEmpty {
                    Section {
                        Picker("来源", selection: $fromExtension) {
                            Text("自己填写").tag(false)
                            Text("从扩展添加").tag(true)
                        }
                        .pickerStyle(.segmented)
                        .labelsHidden()
                        .accessibilityIdentifier("launcher.source")
                    }
                }
                if usesPreset {
                    LauncherPresetSections(model: presets)
                } else {
                    manualSections
                }
            }
            .navigationTitle(isNew ? "新增供应商" : "编辑 \(existing?.name ?? "")")
            .navigationBarTitleDisplayMode(.inline)
            .sheetCancel(unsaved: opened.map { $0 != fields } ?? false) { dismiss() }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") { Task { usesPreset ? await savePreset() : await save() } }.lineLimit(1)
                        .disabled(usesPreset ? !presets.canSave
                                  : saving || name.isEmpty || baseURL.isEmpty || modelName.isEmpty || (isNew && apiKey.isEmpty))
                }
            }
            .task {
                guard isNew else { return }
                await presets.load()
                #if DEBUG
                if UserDefaults.standard.string(forKey: "openLauncherEditor") == "preset" { fromExtension = true }
                #endif
            }
            .onAppear {
                defer { if opened == nil { opened = fields } }
                guard let e = existing else { return }
                name = e.name
                baseURL = e.baseURL ?? ""
                modelName = e.model ?? ""
                smallModel = e.smallModel ?? ""
                subagentModel = e.subagentModel ?? ""
                customModel = e.customModel ?? ""
                customModelName = e.customModelName ?? ""
                customModelDescription = e.customModelDescription ?? ""
                args = (e.args ?? []).joined(separator: " ")
                kind = e.kind ?? "launcher"
                protocolName = e.api ?? "messages"
                auth = e.bearerAuth ? "bearer" : "api_key"
            }
        }
        #if os(macOS)
        .macSheet()
        #endif
    }

    /// 自己填写: the name, the kind, the gateway and key, the models, the flags.
    @ViewBuilder private var manualSections: some View {
        Section {
            TextField("名字，比如 jv_strong", text: $name)
                .font(Theme.Font.body.monospaced())
                .textInputAutocapitalization(.never).autocorrectionDisabled()
                .disabled(!isNew)
        } header: { Text("名字") } footer: { Text("字母、数字、_ 和 -；按这个名字选择供应商。") }
        Section {
            Picker("接入方式", selection: $kind) {
                Text("Claude Code 启动器").tag("launcher")
                Text("直接 API").tag("api")
            }
            if kind == "api" {
                Picker("API 协议", selection: $protocolName) {
                    Text("Messages").tag("messages")
                    Text("Responses").tag("responses")
                }
            }
        } footer: {
            Text(kind == "api" ? "hub 直接请求模型，供分拣、翻译和知识处理等用途选择。" : "在你的电脑上启动 Claude Code，供对话和 Agent 等用途选择。")
        }
        Section {
            TextField("https://…", text: $baseURL)
                .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
            SecretField(isNew || existing?.hasKey != true ? "API Key" : "API Key（不改就留空）", filled: "已填写，点保存",
                        text: $apiKey, identifier: "launcher.editor.key")
            if !usesResponses {
                Picker("认证方式", selection: $auth) {
                    Text("x-api-key").tag("api_key")
                    Text("Bearer").tag("bearer")
                }
            }
        } header: { Text("网关") } footer: {
            Text(usesResponses ? "填写所选协议兼容的地址。Key 加密存在 hub，不会返回到客户端。"
                : "填写所选协议兼容的地址。Key 加密存在 hub，不会返回到客户端。网关只认 Authorization: Bearer（ANTHROPIC_AUTH_TOKEN）时选 Bearer。")
        }
        Section {
            TextField("默认模型", text: $modelName).textInputAutocapitalization(.never).autocorrectionDisabled()
            if kind == "launcher" {
                TextField("小模型（可选，默认同上）", text: $smallModel).textInputAutocapitalization(.never).autocorrectionDisabled()
                TextField("子 Agent 模型（可选，默认同上）", text: $subagentModel)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
            }
        } header: { Text("默认模型") } footer: { Text("各用途的具体选择在「模型配置」里统一管理；未单独指定时使用这里的默认。子 Agent 模型用于 workflow 等多 Agent 运行。") }
        if kind == "launcher" {
            Section {
                TextField("自定义模型（可选）", text: $customModel)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                TextField("显示名（可选）", text: $customModelName)
                TextField("说明（可选）", text: $customModelDescription)
            } header: { Text("自定义模型项（可选）") } footer: { Text("在 /model 里多一个可选项，模型 ID 以网关为准。") }
        }
        if kind == "launcher" {
            Section {
                TextField("比如 --dangerously-skip-permissions", text: $args)
                    .font(Theme.Font.body.monospaced()).textInputAutocapitalization(.never).autocorrectionDisabled()
            } header: { Text("额外参数（可选）") } footer: { Text("加在 claude 后面，空格分隔，每个都以 - 开头。") }
        }
        if let error {
            Section { LoadProblem(text: error) }
        }
    }

    private func savePreset() async {
        if await presets.save() {
            model.launchersChanged()
            Haptics.success()
            await saved()
            dismiss()
        } else {
            Haptics.error()
        }
    }

    private func save() async {
        guard let api = model.api else { return }
        saving = true
        defer { saving = false }
        let input = LauncherInput(name: name.trimmingCharacters(in: .whitespaces), baseURL: baseURL.trimmingCharacters(in: .whitespaces),
                                  model: modelName.trimmingCharacters(in: .whitespaces),
                                  smallModel: kind == "api" ? "" : smallModel.trimmingCharacters(in: .whitespaces),
                                  subagentModel: kind == "api" ? "" : subagentModel.trimmingCharacters(in: .whitespaces),
                                  customModel: kind == "api" ? "" : customModel.trimmingCharacters(in: .whitespaces),
                                  customModelName: kind == "api" ? "" : customModelName.trimmingCharacters(in: .whitespaces),
                                  customModelDescription: kind == "api" ? "" : customModelDescription.trimmingCharacters(in: .whitespaces),
                                  args: kind == "api" ? [] : args.split(whereSeparator: \.isWhitespace).map(String.init),
                                  apiKey: apiKey.isEmpty ? nil : apiKey, kind: kind,
                                  api: kind == "api" ? protocolName : "messages", auth: usesResponses ? "api_key" : auth)
        do {
            try await api.saveLauncher(input, isNew: isNew)
            model.launchersChanged()
            Haptics.success()
            await saved()
            dismiss()
        } catch {
            self.error = error.shown
            Haptics.error()
        }
    }
}

/// A launcher's settings as it really runs them: agent and gateway, each model tier, context and effort, flags,
/// and every other setting it makes (secrets as •••).
struct LauncherDetailSections: View {
    var detail: LauncherDetail
    /// The machines it is about, when they differ.
    var place: String?

    var body: some View {
        if let e = detail.error {
            Section { Label(e, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(.secondary) } header: { title("运行") }
        } else {
            Section {
                LabeledContent("Agent", value: detail.agentName)
                if let u = detail.baseURL, !u.isEmpty { mono("网关", u) }
                if let a = detail.auth { mono("Key", "已设置 · " + a) }
            } header: { title("运行") }
            if let models = detail.models, !models.isEmpty {
                Section {
                    ForEach(models, id: \.self) { m in
                        LabeledContent {
                            Text(m.model).font(Theme.Font.body.monospaced()).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
                        } label: {
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(m.role)
                                Text(m.env).font(Theme.Font.code).foregroundStyle(.tertiary)
                            }
                        }
                    }
                } header: { title("模型") }
            }
            if detail.contextWindow != nil || detail.compactWindow != nil || detail.effort != nil || detail.timeoutMS != nil {
                Section {
                    if let w = detail.contextWindow, w > 0 { LabeledContent("上下文窗口", value: LauncherDetail.tokens(w) + " tokens") }
                    if let c = detail.compactWindow, c > 0 { LabeledContent("自动压缩窗口", value: LauncherDetail.tokens(c) + " tokens") }
                    if let e = detail.effort, !e.isEmpty { LabeledContent("思考强度", value: e) }
                    if let t = detail.timeoutMS, t > 0 { LabeledContent("请求超时", value: t >= 60_000 ? "\(t / 60_000) 分钟" : "\(t / 1000) 秒") }
                } header: { title("上下文") }
            }
            if let args = detail.args, !args.isEmpty {
                Section {
                    Text(args.joined(separator: " ")).font(Theme.Font.code).textSelection(.enabled)
                } header: { title("启动参数") }
            }
            if let env = detail.env, !env.isEmpty {
                Section {
                    ForEach(env, id: \.key) { e in
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text(e.key).font(Theme.Font.code).foregroundStyle(.secondary)
                            Text(e.value).font(Theme.Font.body.monospaced()).lineLimit(3).textSelection(.enabled)
                        }
                    }
                } header: { title("其他设置 \(env.count)") }
            }
        }
    }

    private func title(_ s: String) -> Text { Text(place.map { "\(s) · \($0)" } ?? s) }

    private func mono(_ label: String, _ value: String) -> some View {
        LabeledContent(label) {
            Text(value).font(Theme.Font.body.monospaced()).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
        }
    }
}

/// One machine's test: ✓ with how long and which model, ✗ with why; a spinner while it runs.
struct LauncherCheckRow: View {
    var machine: String?
    var check: LauncherCheck?

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.m) {
            Group {
                if let c = check {
                    Image(systemName: c.ok ? Theme.Icon.successFilled : Theme.Icon.errorFilled).foregroundStyle(c.ok ? Theme.positive : Theme.danger)
                } else {
                    ProgressView().controlSize(.small)
                }
            }
            .frame(width: Theme.Size.iconS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                HStack(spacing: Theme.Space.sm) {
                    Text(machine ?? "hub 所在的机器").font(Theme.Font.heading).lineLimit(1)
                    if let c = check { Text(Self.seconds(c.millis)).font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary) }
                }
                if let c = check {
                    if c.ok {
                        Text([c.model, c.answer.map { "回答「\($0)」" }].compactMap { $0 }.joined(separator: " · "))
                            .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                    } else if let e = c.error {
                        Text(e).font(Theme.Font.code).foregroundStyle(Theme.danger).lineLimit(6).textSelection(.enabled)
                    }
                } else {
                    Text("正在请求…").font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, Theme.Space.xxs)
    }

    static func seconds(_ ms: Int) -> String { ms >= 10_000 ? "\(ms / 1000) 秒" : String(format: "%.1f 秒", Double(ms) / 1000) }
}
