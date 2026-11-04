import SwiftUI

/// Start a session inside an existing space (a new herdr tab next to the other agents).
struct SpacePreset {
    /// The machine id, or "group|id" for another group of hubs (`MachineRef.path`).
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
    var todoID: String? = nil
    /// A todo handed to a new Agent (TodoHandoffSheet): the todo's context block goes ahead of the first message, the
    /// session is named after the todo, and only the todo's hub's machines are offered.
    var contextBlock: String? = nil
    var nameHint: String? = nil
    var onlyGroup: String? = nil
    var onStarted: (Session) -> Void

    @AppStorage("newChat.machine") private var machineID = ""
    @State private var cwd = ""
    @State private var launcher = ""
    @State private var unifiedModels = false
    @State private var defaultLauncher = ""
    @State private var mode = ""   // "" = whatever the launcher uses
    @State private var effort = ""
    @State private var prompt = ""
    @State private var tabName = ""
    @State private var launchers: [Launcher] = []
    @State private var recent: [RecentDir] = []
    @State private var loading = false
    /// Machine whose saved defaults were applied; `.task` re-runs when a pushed picker pops, which must not clobber picks.
    @State private var defaultsFor = ""
    @State private var starting = false
    @State private var error: String?
    @FocusState private var promptFocused: Bool

    /// `machineID` is what is remembered: the machine id, "group|id" for another group of hubs (`MachineRef.path`).
    private var ref: MachineRef { MachineRef(path: machineID) }
    private var machine: Machine? { model.machine(ref) }

    /// The launcher runs Codex: it starts with its first message (Codex makes the thread then) and the start waits for it.
    private var codex: Bool { launchers.first(where: { $0.name == (launcher.isEmpty && unifiedModels ? defaultLauncher : launcher) })?.isCodex == true }

    private var promptHint: String { codex ? "想让 Codex 做什么？（必填：Codex 收到第一条消息才建会话）" : "想让 Claude 做什么？（可以留空）" }

    /// Nothing missing for 开始对话.
    private var ready: Bool {
        machine?.online == true && !cwd.isEmpty && (!launcher.isEmpty || unifiedModels)
            && !(codex && contextBlock == nil && prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
    }

    private var startingLabel: String { "正在 \(machine?.name ?? "") 上启动\(codex ? " Codex" : "")…" }

    @ViewBuilder private var promptField: some View {
        #if os(macOS)
        // A grouped Mac form shows a titled field as label + trailing value: the prompt took the right half.
        TextField(promptHint, text: $prompt, prompt: Text(promptHint), axis: .vertical)
            .labelsHidden()
            .lineLimit(3...10)
        #else
        TextField(promptHint, text: $prompt, axis: .vertical)
            .lineLimit(3...10)
        #endif
    }

    var body: some View {
        NavigationStack {
            Form {
                if let preset {
                    Section {
                        Label(preset.spaceName, systemImage: Theme.Icon.spaceFilled)
                        Text(Fmt.path(preset.cwd, home: machine?.home)).font(Theme.Font.code).foregroundStyle(.secondary)
                    } header: { Text("加入空间") } footer: {
                        Text("会在 \(machine?.name ?? "") 的 herdr 这个 workspace 里新开一个标签。")
                    }
                } else {
                Section {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: Theme.Space.m) {
                            ForEach(machinesShown, id: \.ref) { m in machineCard(m) }
                        }
                        .padding(.vertical, Theme.Space.xs)
                    }
                    .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                } header: { Text("机器") }

                Section {
                    NavigationLink {
                        DirectoryBrowser(ref: ref, start: cwd.isEmpty ? "" : cwd) { picked in cwd = picked }
                    } label: {
                        HStack {
                            Image(systemName: Theme.Icon.folderFilled).foregroundStyle(Theme.accent)
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(cwd.isEmpty ? "选择目录" : (cwd as NSString).lastPathComponent).font(Theme.Font.heading)
                                if !cwd.isEmpty {
                                    Text(Fmt.path(cwd, home: machine?.home)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head)
                                }
                            }
                        }
                    }
                    .accessibilityIdentifier("newChat.directory")
                    if !recent.isEmpty {
                        ForEach(recent.prefix(6)) { r in
                            Button {
                                cwd = r.path
                                Haptics.tap()
                            } label: {
                                HStack {
                                    Image(systemName: cwd == r.path ? Theme.Icon.successFilled : Theme.Icon.recent)
                                        .foregroundStyle(cwd == r.path ? Theme.accent : .secondary)
                                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                        Text(r.name).foregroundStyle(Color.primary)
                                        Text(Fmt.path((r.path as NSString).deletingLastPathComponent, home: machine?.home))
                                            .font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(1).truncationMode(.head)
                                    }
                                    Spacer()
                                    Text(Fmt.relative(r.lastUsed)).font(Theme.Font.label).foregroundStyle(Color.secondary)
                                }
                            }
                        }
                    }
                } header: { Text("目录") }
                }

                Section {
                    NavigationLink {
                        LauncherPicker(launchers: launchers, selection: $launcher, allowDefault: unifiedModels)
                    } label: {
                        HStack {
                            Image(systemName: Theme.Icon.model).foregroundStyle(Theme.accent)
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(launcher.isEmpty ? (unifiedModels ? "跟随模型配置" : "选择启动器") : launcher).font(Theme.Font.heading.monospaced()).lineLimit(1)
                                if let l = launchers.first(where: { $0.name == launcher }), let m = l.model, !m.isEmpty {
                                    Text(m).font(Theme.Font.label).foregroundStyle(.secondary)
                                }
                            }
                            Spacer()
                            if loading { ProgressView().controlSize(.small) }
                        }
                    }
                    .accessibilityIdentifier("newChat.launcher")
                } header: { Text("启动器（本次覆盖）") } footer: {
                    Text(unifiedModels ? "默认使用「模型配置 › 新建 Agent」的设置。手动选择只影响这次会话，不改变全局默认。" : "来自这台机器的 Claude Code / Codex 启动器，会在终端引擎里新开一个标签页运行。")
                }

                if !codex { // Claude Code's modes; a Codex keeps its own
                    Section {
                        Picker(selection: $mode) {
                            Text("跟随启动器").tag("")
                            ForEach(SessionModes.permissionModes) { o in Label(o.title, systemImage: o.symbol).tag(o.id) }
                        } label: { Label { Text("权限模式") } icon: { Image(systemName: Theme.Icon.permissions).foregroundStyle(Theme.accent) } }
                        Picker(selection: $effort) {
                            Text("跟随启动器").tag("")
                            ForEach(SessionModes.effortLevels) { o in Label(o.title, systemImage: o.symbol).tag(o.id) }
                        } label: { Label { Text("思考强度") } icon: { Image(systemName: Theme.Icon.effort).foregroundStyle(Theme.accent) } }
                    } header: { Text("权限与思考") } footer: {
                        Text(SessionModes.mode(mode)?.detail ?? "权限模式和思考强度在对话里也能随时改。")
                    }
                    .pickerStyle(.menu)
                    .tint(Theme.accent)
                }

                Section {
                    promptField
                        .focused($promptFocused)
                        .accessibilityIdentifier("newChat.prompt")
                } header: { Text("第一条消息") }

                Section {
                    TextField("留空就按第一条消息起名", text: $tabName)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .submitLabel(.done)
                        .accessibilityIdentifier("newChat.tabName")
                } header: { Text("标签名（可选）") } footer: {
                    Text("herdr 里这个标签页的名字。不填时按第一条消息起一个短英文名（比如 fix-login），没有第一条消息就叫 agent-1、agent-2…")
                }

            }
            .themedPage()
            .navigationTitle("新对话")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                #if os(macOS)
                // A Mac sheet's primary button sits beside 取消 and answers ⏎ (docs/mac.md「和 iOS 的对应」).
                ToolbarItem(placement: .confirmationAction) {
                    Button(starting ? startingLabel : "开始对话") { Task { await start() } }
                        .lineLimit(1)
                        .disabled(starting || loading || !ready)
                }
                #endif
            }
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: Theme.Space.s) {
                // Next to the button, not as the form's last section: below 第一条消息 and under this button it went
                // unseen, the button just turned back to 开始对话 (and a retry started a second agent).
                if let error {
                    Label(error, systemImage: Theme.Icon.errorFilled)
                        .font(Theme.Font.caption).foregroundStyle(Theme.danger).lineLimit(4)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(Theme.Space.m)
                        .background(.regularMaterial, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
                        .accessibilityIdentifier("newChat.error")
                }
                #if os(iOS)
                Button {
                    Task { await start() }
                } label: {
                    HStack(spacing: Theme.Space.s) {
                        if starting { ProgressView().tint(.white) }
                        Text(starting ? startingLabel : "开始对话").font(Theme.Font.heading)
                            .lineLimit(1).minimumScaleFactor(0.8)
                    }
                    .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.wellS)
                }
                .themedButton(prominent: true)
                .controlSize(.large)
                .disabled(starting || loading || !ready)
                #endif
                }
                .padding(.horizontal, Theme.Space.xl)
                .padding(.bottom, Theme.Space.s)
            }
            .task(id: machineID) { await loadMachine() }
            .onAppear {
                if prompt.isEmpty { prompt = initialPrompt }
                if let preset { machineID = preset.machineID; cwd = preset.cwd } else { pickDefaultMachine() }
            }
            .onChange(of: model.machines) { _, _ in if preset == nil { pickDefaultMachine() } }
            .interactiveDismissDisabled(starting)
        }
        #if os(macOS)
        // On the stack, not its root: with the minimum size on the form, pushing 启动器 / 目录 shrank a sheet opened
        // from Agents' 新建会话 to the pushed list's own height, zero (#303). Also gives the grouped form to sheets
        // that miss macLook's (知识库 › 开 Agent).
        .macSheet()
        #endif
    }

    /// The machines offered: this hub's when a todo is handed over (`onlyGroup`), else all of them.
    private var machinesShown: [Machine] {
        onlyGroup.map { g in model.machines.filter { $0.group == g } } ?? model.machines
    }

    /// The remembered machine belongs to another hub than the one the todo is on.
    private var machineOutsideGroup: Bool { onlyGroup.map { machine?.group != $0 } ?? false }

    /// Remember the last machine, but fall back to the first online one (machines may arrive after the sheet opens).
    private func pickDefaultMachine() {
        if machine == nil || machine?.online == false || machineOutsideGroup,
           let m = machinesShown.first(where: \.online) ?? machinesShown.first {
            machineID = m.ref.path
        }
    }

    private func machineCard(_ m: Machine) -> some View {
        let selected = m.ref.path == machineID
        return Button {
            Haptics.tap()
            machineID = m.ref.path
        } label: {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                HStack {
                    Image(systemName: m.symbol).font(Theme.Font.title)
                    Spacer()
                    Circle().fill(m.online ? Theme.positive : Color.secondary.opacity(0.4)).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
                }
                Text(m.name).font(Theme.Font.secondaryStrong).lineLimit(1)
                // Several groups of hubs: which group the machine is in, where the host name was.
                Text(m.online ? (m.groupLabel ?? m.hostname ?? "在线") : "离线").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            .scaledFrame(width: Theme.Size.box120, alignment: .leading)
            .padding(Theme.Space.m)
            .background(selected ? Theme.accent.opacity(0.14) : Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(selected ? Theme.accent : .clear, lineWidth: 1.5))
            .opacity(m.online ? 1 : 0.55)
        }
        .buttonStyle(.plain)
        .disabled(!m.online)
    }

    private func loadMachine() async {
        guard !machineID.isEmpty, let api = model.api(ref.group), machine?.online == true else { return }
        loading = true
        defer { loading = false }
        error = nil
        do {
            do {
                let purpose = try await api.modelPurpose("agent.new").purpose
                defaultLauncher = purpose.effective.provider.map { $0.hasPrefix("launcher.") ? String($0.dropFirst("launcher.".count)) : $0 } ?? ""
                unifiedModels = true
            } catch let e as APIError where e.status == 404 {
                unifiedModels = false
            }
            async let l = model.launchers(ref)
            async let d = api.dirs(machine: ref.id, path: "")
            let (ls, dirs) = try await (l, d)
            launchers = ls
            recent = dirs.recent ?? []
            if defaultsFor != machineID {
                defaultsFor = machineID
                let saved = UserDefaults.standard.string(forKey: "launcher.\(machineID)")
                mode = UserDefaults.standard.string(forKey: "mode.\(machineID)") ?? ""
                effort = UserDefaults.standard.string(forKey: "effort.\(machineID)") ?? ""
                if unifiedModels { launcher = ""; effort = "" }
                else if let saved, ls.contains(where: { $0.name == saved }) { launcher = saved }
                if let preset { cwd = preset.cwd } else {
                    cwd = UserDefaults.standard.string(forKey: "cwd.\(machineID)") ?? recent.first?.path ?? ""
                }
            }
            if !ls.contains(where: { $0.name == launcher }), !(unifiedModels && launcher.isEmpty) {
                launcher = unifiedModels ? "" : ls.first?.name ?? ""
            }
        } catch {
            self.error = error.shown
        }
    }

    private func start() async {
        guard let api = model.api(ref.group) else {
            error = "连不上这台电脑所在的 hub，请稍后再试。"
            return
        }
        starting = true
        error = nil
        defer { starting = false }
        do {
            let p = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
            let name = tabName.trimmingCharacters(in: .whitespacesAndNewlines)
            // The first message: a todo's context block (when a todo is handed over), then what was typed.
            let outgoing = contextBlock.map { p.isEmpty ? $0 : "\($0)\n\n\(p)" } ?? p
            if codex {
                // Codex picks its thread's id once it has the first message: it goes with the start, which answers
                // with that thread (one session, already working on it).
                let s = try await api.start(.init(machineID: ref.id, cwd: cwd, launcher: launcher, prompt: contextBlock == nil ? prompt : outgoing,
                                                  workspaceID: preset?.workspaceID, tabName: name.isEmpty ? nil : name,
                                                  nameHint: nameHint ?? (p.isEmpty ? nil : p), todoID: todoID))
                UserDefaults.standard.set(launcher, forKey: "launcher.\(machineID)")
                UserDefaults.standard.set(cwd, forKey: "cwd.\(machineID)")
                Haptics.success()
                model.adopt(s)
                onStarted(s)
                return
            }
            // The hub answers at once and starts claude in the background; the first message goes out as an ordinary
            // message (on screen right away, typed in once claude is ready).
            let s = try await api.start(.init(machineID: ref.id, cwd: cwd, launcher: launcher, prompt: nil,
                                              workspaceID: preset?.workspaceID, permissionMode: mode.isEmpty ? nil : mode,
                                              effort: effort.isEmpty ? nil : effort, tabName: name.isEmpty ? nil : name,
                                              async: true, nameHint: nameHint ?? (p.isEmpty ? nil : p), todoID: todoID))
            if !unifiedModels {
                UserDefaults.standard.set(launcher, forKey: "launcher.\(machineID)")
                UserDefaults.standard.set(effort, forKey: "effort.\(machineID)")
            }
            UserDefaults.standard.set(mode, forKey: "mode.\(machineID)")
            UserDefaults.standard.set(cwd, forKey: "cwd.\(machineID)")
            Haptics.success()
            model.adopt(s)
            if !outgoing.isEmpty { model.sendNow(s.key, text: outgoing) }
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
    var allowDefault = false
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    var body: some View {
        List {
            if allowDefault {
                Button {
                    selection = ""
                    dismiss()
                } label: {
                    Label("跟随模型配置", systemImage: Theme.Icon.model).lineLimit(1)
                }
            }
            ForEach(launchers.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) || ($0.model ?? "").localizedCaseInsensitiveContains(query) }) { l in
                Button {
                    selection = l.name
                    Haptics.tap()
                    dismiss()
                } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text(l.name).font(Theme.Font.heading.monospaced()).foregroundStyle(.primary)
                            HStack(spacing: Theme.Space.sm) {
                                if l.isCodex { Chip(text: "Codex") }
                                if let m = l.model, !m.isEmpty { Chip(text: m) }
                                if let host = l.baseURL.flatMap({ URL(string: $0)?.host() }) {
                                    Text(host).font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(1)
                                }
                            }
                        }
                        Spacer()
                        if l.name == selection { Image(systemName: Theme.Icon.done).foregroundStyle(Theme.accent).fontWeight(.semibold) }
                    }
                }
            }
        }
        .searchable(text: $query, prompt: "函数名或模型")
        .themedPage()
        .navigationTitle("启动器")
    }
}

struct DirectoryBrowser: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var ref: MachineRef
    var start: String
    var onPick: (String) -> Void
    @State private var list: DirList?
    @State private var path = ""
    @State private var draft = ""
    @State private var error: String?
    @State private var loading = false
    @State private var attempt = 0
    @FocusState private var editing: Bool

    var body: some View {
        List {
            if let list {
                Section {
                    HStack(spacing: Theme.Space.s) {
                        TextField("输入路径", text: $draft)
                            .font(Theme.Font.code)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.asciiCapable)
                            .submitLabel(.go)
                            .focused($editing)
                            .onSubmit { go() }
                            .accessibilityIdentifier("dirBrowser.path")
                        if loading {
                            ProgressView()
                        } else if editing, DirectoryPath.resolve(draft, from: list.path) != list.path {
                            Button { go() } label: { Image(systemName: Theme.Icon.enter) }
                                .accessibilityLabel("前往")
                        }
                    }
                    Button {
                        onPick(list.path)
                        dismiss()
                    } label: {
                        Label("使用「\((list.path as NSString).lastPathComponent)」", systemImage: Theme.Icon.successFilled)
                            .font(Theme.Font.heading)
                            .lineLimit(1)
                    }
                } footer: { Text("可以直接改上面的路径，回车前往；~ 是主目录") }

                if let error {
                    Section {
                        Label(error, systemImage: Theme.Icon.error)
                            .foregroundStyle(Theme.danger)
                            .accessibilityIdentifier("dirBrowser.error")
                    }
                }

                Section {
                    if let parent = list.parent, !parent.isEmpty {
                        Button { Task { await load(parent) } } label: { Label("上一级", systemImage: "arrow.turn.left.up") }
                    }
                    ForEach(list.entries) { e in
                        Button { Task { await load(e.path) } } label: {
                            HStack {
                                Image(systemName: e.isGit ? "folder.badge.gearshape" : Theme.Icon.folder).foregroundStyle(e.isGit ? Theme.accent : .secondary)
                                Text(e.name).foregroundStyle(.primary)
                                Spacer()
                                if e.isGit { Chip(text: "git") }
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
        .themedPage()
        .navigationTitle((path as NSString).lastPathComponent.isEmpty ? "目录" : (path as NSString).lastPathComponent)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load(start) }
    }

    private func go() {
        guard let target = DirectoryPath.resolve(draft, from: list?.path ?? "") else { return }
        editing = false
        Task { await load(target) }
    }

    /// The last tap wins; a directory the hub can't list keeps the current one on screen and says why. A start that
    /// can't be listed (a saved directory gone or unreadable) falls back to the home directory, the reason above it.
    private func load(_ p: String) async {
        guard let api = model.api(ref.group) else { return }
        attempt += 1
        let mine = attempt
        loading = true
        defer { if mine == attempt { loading = false } }
        do {
            let l = try await api.dirs(machine: ref.id, path: p)
            guard mine == attempt else { return }
            withAnimation(Theme.Motion.standard) {
                list = l
                path = l.path
                draft = l.path
                error = nil
            }
        } catch {
            guard mine == attempt else { return }
            let reason = DirectoryPath.problem((error as? APIError)?.message ?? error.shown ?? "", path: p)
            if list == nil, !p.isEmpty {
                await load("")
                self.error = reason
                return
            }
            withAnimation(Theme.Motion.standard) {
                self.error = reason
                if let list { draft = list.path }
            }
        }
    }
}

/// The directory browser's typed paths and failures, apart from the view (DirectoryPathTests).
nonisolated enum DirectoryPath {
    /// What to ask the hub for: `~`, `~/…` and absolute paths as typed (the node expands `~`); anything else is
    /// under `current` (under the home directory when there is none yet). nil for nothing typed.
    static func resolve(_ typed: String, from current: String) -> String? {
        let t = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return nil }
        if t.hasPrefix("/") || t == "~" || t.hasPrefix("~/") { return t }
        guard !current.isEmpty else { return "~/" + t }
        return (current as NSString).appendingPathComponent(t)
    }

    /// Why the hub couldn't list `path`, in words: its errors are Go's os errors ("open /x: permission denied").
    static func problem(_ message: String, path: String) -> String {
        let m = message.lowercased()
        let name = (path as NSString).lastPathComponent
        let shown = name.isEmpty ? path : name
        if m.contains("permission denied") { return "没有权限列出「\(shown)」里的目录。知道下一级的名字，可以在上面直接输入完整路径" }
        if m.contains("no such file") { return "「\(path)」不存在" }
        if m.contains("not a directory") { return "「\(path)」不是目录" }
        return message.isEmpty ? "打不开「\(path)」" : message
    }
}
