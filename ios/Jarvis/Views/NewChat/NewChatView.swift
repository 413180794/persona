import SwiftUI

/// Start a session inside an existing space (a new herdr tab next to the other agents).
struct SpacePreset {
    var machineID: String
    var cwd: String
    var workspaceID: String
    var spaceName: String
}

/// Pick a machine, a project directory and a launcher (model), optionally with a first message.
struct NewChatView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var initialPrompt = ""
    var preset: SpacePreset?
    var onStarted: (Session) -> Void

    @AppStorage("newChat.machine") private var machineID = ""
    @State private var cwd = ""
    @State private var launcher = ""
    @State private var mode = ""   // "" = whatever the launcher uses
    @State private var effort = ""
    @State private var prompt = ""
    @State private var launchers: [Launcher] = []
    @State private var recent: [RecentDir] = []
    @State private var loading = false
    /// Machine whose saved defaults were applied; `.task` re-runs when a pushed picker pops, which must not clobber picks.
    @State private var defaultsFor = ""
    @State private var starting = false
    @State private var error: String?
    @FocusState private var promptFocused: Bool

    private var machine: Machine? { model.machine(machineID) }

    var body: some View {
        NavigationStack {
            Form {
                if let preset {
                    Section {
                        Label(preset.spaceName, systemImage: "square.stack.3d.up.fill")
                        Text(Fmt.path(preset.cwd, home: machine?.home)).font(.caption.monospaced()).foregroundStyle(.secondary)
                    } header: { Text("加入空间") } footer: {
                        Text("会在 \(machine?.name ?? "") 的 herdr 这个 workspace 里新开一个标签。")
                    }
                } else {
                Section {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 10) {
                            ForEach(model.machines) { m in machineCard(m) }
                        }
                        .padding(.vertical, 4)
                    }
                    .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                } header: { Text("机器") }

                Section {
                    NavigationLink {
                        DirectoryBrowser(machineID: machineID, start: cwd.isEmpty ? "" : cwd) { picked in cwd = picked }
                    } label: {
                        HStack {
                            Image(systemName: "folder.fill").foregroundStyle(Theme.accent)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(cwd.isEmpty ? "选择目录" : (cwd as NSString).lastPathComponent).font(.body.weight(.medium))
                                if !cwd.isEmpty {
                                    Text(Fmt.path(cwd, home: machine?.home)).font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head)
                                }
                            }
                        }
                    }
                    if !recent.isEmpty {
                        ForEach(recent.prefix(6)) { r in
                            Button {
                                cwd = r.path
                                Haptics.tap()
                            } label: {
                                HStack {
                                    Image(systemName: cwd == r.path ? "checkmark.circle.fill" : "clock.arrow.circlepath")
                                        .foregroundStyle(cwd == r.path ? Theme.accent : .secondary)
                                    VStack(alignment: .leading, spacing: 1) {
                                        Text(r.name).foregroundStyle(Color.primary)
                                        Text(Fmt.path((r.path as NSString).deletingLastPathComponent, home: machine?.home))
                                            .font(.caption2).foregroundStyle(Color.secondary).lineLimit(1).truncationMode(.head)
                                    }
                                    Spacer()
                                    Text(Fmt.relative(r.lastUsed)).font(.caption).foregroundStyle(Color.secondary)
                                }
                            }
                        }
                    }
                } header: { Text("目录") }
                }

                Section {
                    NavigationLink {
                        LauncherPicker(launchers: launchers, selection: $launcher)
                    } label: {
                        HStack {
                            Image(systemName: "cpu").foregroundStyle(Theme.accent)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(launcher.isEmpty ? "选择启动器" : launcher).font(.body.weight(.medium).monospaced())
                                if let l = launchers.first(where: { $0.name == launcher }), let m = l.model, !m.isEmpty {
                                    Text(m).font(.caption).foregroundStyle(.secondary)
                                }
                            }
                            Spacer()
                            if loading { ProgressView().controlSize(.small) }
                        }
                    }
                } header: { Text("启动器（模型）") } footer: {
                    Text("来自这台机器 ~/.zshrc 里的 claude 启动函数，会在 herdr 里新开一个标签页运行。")
                }

                Section {
                    Picker(selection: $mode) {
                        Text("跟随启动器").tag("")
                        ForEach(SessionModes.permissionModes) { o in Label(o.title, systemImage: o.symbol).tag(o.id) }
                    } label: { Label { Text("权限模式") } icon: { Image(systemName: "lock.shield").foregroundStyle(Theme.accent) } }
                    Picker(selection: $effort) {
                        Text("跟随启动器").tag("")
                        ForEach(SessionModes.effortLevels) { o in Label(o.title, systemImage: o.symbol).tag(o.id) }
                    } label: { Label { Text("思考强度") } icon: { Image(systemName: "gauge.with.dots.needle.50percent").foregroundStyle(Theme.accent) } }
                } header: { Text("权限与思考") } footer: {
                    Text(SessionModes.mode(mode)?.detail ?? "权限模式和思考强度在对话里也能随时改。")
                }
                .pickerStyle(.menu)
                .tint(Theme.accent)

                Section {
                    TextField("想让 Claude 做什么？（可以留空）", text: $prompt, axis: .vertical)
                        .lineLimit(3...10)
                        .focused($promptFocused)
                } header: { Text("第一条消息") }

                if let error {
                    Section { Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(Theme.danger).font(.footnote) }
                }
            }
            .navigationTitle("新对话")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
            }
            .safeAreaInset(edge: .bottom) {
                Button {
                    Task { await start() }
                } label: {
                    HStack(spacing: 8) {
                        if starting { ProgressView().tint(.white) }
                        Text(starting ? "正在 \(machine?.name ?? "") 上启动…" : "开始对话").font(.headline)
                            .lineLimit(1).minimumScaleFactor(0.8)
                    }
                    .frame(maxWidth: .infinity, minHeight: 30)
                }
                .themedButton(prominent: true)
                .controlSize(.large)
                .disabled(starting || machine?.online != true || cwd.isEmpty || launcher.isEmpty)
                .padding(.horizontal, 20)
                .padding(.bottom, 8)
            }
            .task(id: machineID) { await loadMachine() }
            .onAppear {
                if prompt.isEmpty { prompt = initialPrompt }
                if let preset { machineID = preset.machineID; cwd = preset.cwd } else { pickDefaultMachine() }
            }
            .onChange(of: model.machines) { _, _ in if preset == nil { pickDefaultMachine() } }
            .interactiveDismissDisabled(starting)
        }
    }

    /// Remember the last machine, but fall back to the first online one (machines may arrive after the sheet opens).
    private func pickDefaultMachine() {
        if machine == nil || machine?.online == false, let m = model.machines.first(where: \.online) ?? model.machines.first {
            machineID = m.id
        }
    }

    private func machineCard(_ m: Machine) -> some View {
        let selected = m.id == machineID
        return Button {
            Haptics.tap()
            machineID = m.id
        } label: {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Image(systemName: m.symbol).font(.title3)
                    Spacer()
                    Circle().fill(m.online ? Theme.positive : Color.secondary.opacity(0.4)).frame(width: 8, height: 8)
                }
                Text(m.name).font(.subheadline.weight(.semibold)).lineLimit(1)
                Text(m.online ? (m.hostname ?? "在线") : "离线").font(.caption2).foregroundStyle(.secondary).lineLimit(1)
            }
            .frame(width: 120, alignment: .leading)
            .padding(12)
            .background(selected ? Theme.accent.opacity(0.14) : Color.primary.opacity(0.04), in: .rect(cornerRadius: 16, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).stroke(selected ? Theme.accent : .clear, lineWidth: 1.5))
            .opacity(m.online ? 1 : 0.55)
        }
        .buttonStyle(.plain)
        .disabled(!m.online)
    }

    private func loadMachine() async {
        guard !machineID.isEmpty, let api = model.api, machine?.online == true else { return }
        loading = true
        defer { loading = false }
        error = nil
        do {
            async let l = model.launchers(machineID)
            async let d = api.dirs(machine: machineID, path: "")
            let (ls, dirs) = try await (l, d)
            launchers = ls
            recent = dirs.recent ?? []
            if defaultsFor != machineID {
                defaultsFor = machineID
                let saved = UserDefaults.standard.string(forKey: "launcher.\(machineID)")
                mode = UserDefaults.standard.string(forKey: "mode.\(machineID)") ?? ""
                effort = UserDefaults.standard.string(forKey: "effort.\(machineID)") ?? ""
                if let saved, ls.contains(where: { $0.name == saved }) { launcher = saved }
                if let preset { cwd = preset.cwd } else {
                    cwd = UserDefaults.standard.string(forKey: "cwd.\(machineID)") ?? recent.first?.path ?? ""
                }
            }
            if !ls.contains(where: { $0.name == launcher }) { launcher = ls.first?.name ?? "" }
        } catch {
            self.error = error.shown
        }
    }

    private func start() async {
        guard let api = model.api else { return }
        starting = true
        error = nil
        defer { starting = false }
        do {
            let p = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
            let s = try await api.start(.init(machineID: machineID, cwd: cwd, launcher: launcher, prompt: p.isEmpty ? nil : p,
                                              workspaceID: preset?.workspaceID, permissionMode: mode.isEmpty ? nil : mode,
                                              effort: effort.isEmpty ? nil : effort))
            UserDefaults.standard.set(launcher, forKey: "launcher.\(machineID)")
            UserDefaults.standard.set(mode, forKey: "mode.\(machineID)")
            UserDefaults.standard.set(effort, forKey: "effort.\(machineID)")
            UserDefaults.standard.set(cwd, forKey: "cwd.\(machineID)")
            Haptics.success()
            onStarted(s)
        } catch {
            self.error = error.shown
            Haptics.error()
        }
    }
}

struct LauncherPicker: View {
    var launchers: [Launcher]
    @Binding var selection: String
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    var body: some View {
        List {
            ForEach(launchers.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) || ($0.model ?? "").localizedCaseInsensitiveContains(query) }) { l in
                Button {
                    selection = l.name
                    Haptics.tap()
                    dismiss()
                } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(l.name).font(.body.weight(.medium).monospaced()).foregroundStyle(.primary)
                            HStack(spacing: 6) {
                                if let m = l.model, !m.isEmpty { Chip(text: m, tint: Theme.accent) }
                                if let host = l.baseURL.flatMap({ URL(string: $0)?.host() }) {
                                    Text(host).font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                                }
                            }
                        }
                        Spacer()
                        if l.name == selection { Image(systemName: "checkmark").foregroundStyle(Theme.accent).fontWeight(.semibold) }
                    }
                }
            }
        }
        .searchable(text: $query, prompt: "函数名或模型")
        .navigationTitle("启动器")
    }
}

struct DirectoryBrowser: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var machineID: String
    var start: String
    var onPick: (String) -> Void
    @State private var list: DirList?
    @State private var path = ""
    @State private var error: String?

    var body: some View {
        List {
            if let list {
                Section {
                    Button {
                        onPick(list.path)
                        dismiss()
                    } label: {
                        Label("使用「\((list.path as NSString).lastPathComponent)」", systemImage: "checkmark.circle.fill")
                            .font(.body.weight(.semibold))
                    }
                } footer: { Text(list.path).font(.caption.monospaced()) }

                Section {
                    if let parent = list.parent, !parent.isEmpty {
                        Button { Task { await load(parent) } } label: { Label("上一级", systemImage: "arrow.turn.left.up") }
                    }
                    ForEach(list.entries) { e in
                        Button { Task { await load(e.path) } } label: {
                            HStack {
                                Image(systemName: e.isGit ? "folder.badge.gearshape" : "folder").foregroundStyle(e.isGit ? Theme.accent : .secondary)
                                Text(e.name).foregroundStyle(.primary)
                                Spacer()
                                if e.isGit { Chip(text: "git", tint: Theme.accent) }
                            }
                        }
                    }
                }
            } else if let error {
                Text(error).foregroundStyle(Theme.danger)
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .navigationTitle((path as NSString).lastPathComponent.isEmpty ? "目录" : (path as NSString).lastPathComponent)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load(start) }
    }

    private func load(_ p: String) async {
        guard let api = model.api else { return }
        do {
            let l = try await api.dirs(machine: machineID, path: p)
            withAnimation(.snappy) {
                list = l
                path = l.path
            }
        } catch {
            self.error = error.shown
        }
    }
}
