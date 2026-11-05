import SwiftUI

// The node menu of the Agents list (docs/terminal.md「节点菜单」, the web's #2329): what changes a computer's terminals from
// a space's ⋯ and an agent row's long press — split the agent's pane, close its tab, run an agent in a plain shell of the
// space, fetch / pull / push. Each is a hub route only a full device may call (server/internal/hub/panes.go); the
// request names a pane, tab or workspace the host lists and never a directory or a command.

// MARK: - The hub's answers

/// `GET /machines/{mid}/panes` (`proto.PanesResponse`): every workspace, tab and pane of each terminal host.
nonisolated struct TerminalPanes: Decodable, Sendable, Equatable {
    var hosts: [Host]

    nonisolated struct Host: Decodable, Sendable, Equatable {
        var kind: String
        /// The host can open a new shell tab in a workspace (`POST /panes`).
        var newTab: Bool
        /// The host can make a Git worktree of a workspace's repository (新建 worktree…).
        var worktree: Bool
        var tabs: [Tab]
        var panes: [Pane]

        enum CodingKeys: String, CodingKey { case kind, tabs, panes, newTab = "new_tab", worktree }

        init(kind: String, newTab: Bool = false, worktree: Bool = false, tabs: [Tab] = [], panes: [Pane] = []) {
            self.kind = kind
            self.newTab = newTab
            self.worktree = worktree
            self.tabs = tabs
            self.panes = panes
        }

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
            newTab = try c.decodeIfPresent(Bool.self, forKey: .newTab) ?? false
            worktree = try c.decodeIfPresent(Bool.self, forKey: .worktree) ?? false
            tabs = try c.decodeIfPresent([Tab].self, forKey: .tabs) ?? []
            panes = try c.decodeIfPresent([Pane].self, forKey: .panes) ?? []
        }
    }

    nonisolated struct Tab: Decodable, Sendable, Equatable {
        var id: String
        var workspaceID: String
        var label: String
        enum CodingKeys: String, CodingKey { case id, label, workspaceID = "workspace_id" }
    }

    nonisolated struct Pane: Decodable, Sendable, Equatable, Identifiable {
        var pane: String
        var tabID: String
        var workspaceID: String
        /// The agent running there ("claude", "codex"…); nil or "" for a plain shell.
        var agent: String? = nil
        var title: String? = nil
        var cwd: String? = nil
        var sessionID: String? = nil
        var id: String { pane }
        enum CodingKeys: String, CodingKey {
            case pane, agent, title, cwd
            case tabID = "tab_id", workspaceID = "workspace_id", sessionID = "session_id"
        }
    }
}

/// What `panes/git` did: a one-line summary for the toast.
nonisolated struct PaneGitResult: Decodable, Sendable {
    var op: String?
    var message: String?
}

extension APIClient {
    func terminalPanes(machine: String) async throws -> TerminalPanes {
        try await get("/api/v1/machines/\(machine)/panes", as: TerminalPanes.self)
    }

    /// One of the node menu's routes, `POST /machines/{mid}/<path>`; the body is only the named string fields.
    @discardableResult
    func nodeAction(machine: String, _ path: String, _ body: [String: String], timeout: TimeInterval = 40) async throws -> Data {
        try await post("/api/v1/machines/\(machine)/\(path)", body: body, timeout: timeout)
    }
}

// MARK: - What the menus offer

nonisolated enum NodeMenu {
    /// Closing, splitting, running an agent in a shell and git are herdr's: Orca, herdr-core and botmux are not asked.
    static func canChange(hostKind: String?) -> Bool { hostKind == "herdr" }

    /// The pane items of an agent row: a live herdr pane the app may drive.
    static func offersPaneActions(_ s: Session) -> Bool {
        s.state.isLive && s.canTerminal && canChange(hostKind: s.hostKind) && !(s.paneID ?? "").isEmpty
    }

    /// The space items: a herdr workspace with a real id (not the "-" of agents without one).
    static func offersSpaceActions(_ g: SpaceGroup) -> Bool {
        g.spaceID != "-" && workspaceID(g) != nil && g.sessions.contains { canChange(hostKind: $0.hostKind) }
    }

    /// The workspace a space's agents run in, as the host names it.
    static func workspaceID(_ g: SpaceGroup) -> String? {
        let id = g.sessions.lazy.compactMap(\.workspaceID).first { !$0.isEmpty } ?? g.spaceID
        return id.isEmpty || id == "-" ? nil : id
    }

    /// The host's kind as the hub names it in a body (`kind`), when the session says.
    static func kind(_ g: SpaceGroup) -> String? { g.sessions.lazy.compactMap(\.spaceKind).first { !$0.isEmpty } }

    enum GitOp: String, CaseIterable, Identifiable, Sendable {
        case pull, push, fetch
        var id: String { rawValue }
        var title: String {
            switch self {
            case .pull: "拉取"
            case .push: "推送"
            case .fetch: "获取"
            }
        }
        var symbol: String {
            switch self {
            case .pull: Theme.Icon.gitPull
            case .push: Theme.Icon.gitPush
            case .fetch: Theme.Icon.gitFetch
            }
        }
        /// The toast after it ran: the node's one-line summary ("已经是最新"), or 完成.
        func done(_ message: String?) -> String {
            let m = message?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            return "\(title)：\(m.isEmpty ? "完成" : m)"
        }
    }

    /// `panes/git` in an agent's pane: its own directory.
    static func gitBody(_ op: GitOp, pane: String) -> [String: String] { ["op": op.rawValue, "pane": pane] }

    /// `panes/git` in a workspace's directory.
    static func gitBody(_ op: GitOp, workspaceID: String, kind: String?) -> [String: String] {
        var body = ["op": op.rawValue, "workspace_id": workspaceID]
        if let kind, !kind.isEmpty { body["kind"] = kind }
        return body
    }

    enum Direction: String, CaseIterable, Identifiable, Sendable {
        case right, down
        var id: String { rawValue }
        var title: String { self == .right ? "向右拆分" : "向下拆分" }
        var symbol: String { self == .right ? Theme.Icon.splitRight : Theme.Icon.splitDown }
    }

    static func splitBody(pane: String, _ direction: Direction) -> [String: String] {
        ["pane": pane, "direction": direction.rawValue]
    }

    static func closeTabBody(tabID: String, kind: String?) -> [String: String] {
        var body = ["tab_id": tabID]
        if let kind, !kind.isEmpty { body["kind"] = kind }
        return body
    }

    /// The herdr host of a machine's layout ("" is herdr too, from a node that does not say).
    static func herdrHost(_ panes: TerminalPanes) -> TerminalPanes.Host? {
        panes.hosts.first { $0.kind == "herdr" || $0.kind.isEmpty }
    }

    /// The plain shells of a workspace, where an agent can be started: no agent, no session bound to them.
    static func shells(in panes: TerminalPanes, workspaceID: String) -> [TerminalPanes.Pane] {
        guard let host = herdrHost(panes) else { return [] }
        return host.panes.filter { p in
            p.workspaceID == workspaceID && (p.agent ?? "").isEmpty && (p.sessionID ?? "").isEmpty
        }
    }

    /// How a shell reads in the picker: its tab's name, then what the terminal calls itself or its folder.
    static func shellTitle(_ p: TerminalPanes.Pane, in panes: TerminalPanes) -> String {
        let tab = herdrHost(panes)?.tabs.first { $0.id == p.tabID }?.label ?? ""
        let detail = [p.title, p.cwd.map { ($0 as NSString).lastPathComponent }].compactMap { $0 }.first { !$0.isEmpty } ?? p.pane
        return tab.isEmpty ? detail : "\(tab) · \(detail)"
    }

    /// Only the agents a launcher starts without a first message: Claude Code (a Codex start needs one).
    static func launchableHere(_ l: Launcher) -> Bool { (l.agent ?? "").isEmpty || l.agent == "claude" }
}

// MARK: - Doing it

/// Runs the node menu's items and holds what they ask for (the close confirmation, the run-here sheet). One for the app:
/// the menus are equatable and read nothing that changes with a push; `nodeActionPrompts()` on the Agents list shows it.
@MainActor @Observable
final class NodeActions {
    static let shared = NodeActions()

    /// A tab waiting for 关闭标签页's confirmation.
    struct CloseTab: Identifiable, Equatable {
        var machine: MachineRef
        var tabID: String
        var kind: String?
        var name: String
        var id: String { machine.path + "/" + tabID }
    }

    var closingTab: CloseTab?
    /// The space whose 在这里运行 Agent… sheet is open.
    var runningHere: SpaceGroup?

    private var model: AppModel { AppModel.shared }

    func git(_ op: NodeMenu.GitOp, session s: Session) {
        guard let pane = s.paneID, !pane.isEmpty else { return }
        git(op, machine: s.machineRef, body: NodeMenu.gitBody(op, pane: pane))
    }

    func git(_ op: NodeMenu.GitOp, space g: SpaceGroup) {
        guard let ws = NodeMenu.workspaceID(g) else { return }
        git(op, machine: g.machineRef, body: NodeMenu.gitBody(op, workspaceID: ws, kind: NodeMenu.kind(g)))
    }

    private func git(_ op: NodeMenu.GitOp, machine: MachineRef, body: [String: String]) {
        guard let api = model.api(machine.group) else { return }
        model.toast = Toast("正在\(op.title)…")
        Task {
            do {
                let data = try await api.nodeAction(machine: machine.id, "panes/git", body, timeout: 100)
                let result = try? JSON.decoder.decode(PaneGitResult.self, from: data)
                model.toast = Toast(op.done(result?.message), style: .success)
                Haptics.success()
            } catch {
                fail("\(op.title)失败", error)
            }
        }
    }

    func split(_ s: Session, _ direction: NodeMenu.Direction) {
        guard let pane = s.paneID, !pane.isEmpty, let api = model.api(s.group) else { return }
        Task {
            do {
                try await api.nodeAction(machine: s.machineID, "panes/split", NodeMenu.splitBody(pane: pane, direction))
                model.toast = Toast("已新建窗格", style: .success)
                await model.refresh()
            } catch {
                fail("新建窗格失败", error)
            }
        }
    }

    /// 关闭标签页…: asks first (the tab's every pane goes, and the agents in them).
    func askCloseTab(_ s: Session) {
        guard let tab = s.tabID ?? s.slotID, !tab.isEmpty else { return }
        let name = s.slotName.map { $0.allSatisfy(\.isNumber) ? "#" + $0 : $0 } ?? ""
        closingTab = CloseTab(machine: s.machineRef, tabID: tab, kind: s.spaceKind, name: name)
    }

    func closeTab(_ c: CloseTab) {
        guard let api = model.api(c.machine.group) else { return }
        Task {
            do {
                try await api.nodeAction(machine: c.machine.id, "tabs/close", NodeMenu.closeTabBody(tabID: c.tabID, kind: c.kind))
                model.toast = Toast("已关闭标签页", style: .success)
                await model.refresh()
            } catch {
                fail("关闭失败", error)
            }
        }
    }

    func fail(_ what: String, _ error: Error) {
        if error.isOffline || error.isQuiet { model.show(error); return }
        model.toast = Toast("\(what)：\(error.shown ?? error.localizedDescription)", style: .error)
        Haptics.error()
    }
}

extension View {
    /// The node menu's confirmation and sheets, once on the Agents list.
    func nodeActionPrompts() -> some View { modifier(NodeActionPrompts()) }
}

private struct NodeActionPrompts: ViewModifier {
    @State private var actions = NodeActions.shared

    func body(content: Content) -> some View {
        content
            .confirmationDialog(actions.closingTab.map { $0.name.isEmpty ? "关闭这个标签页？" : "关闭标签页「\($0.name)」？" } ?? "",
                                isPresented: Binding(get: { actions.closingTab != nil }, set: { if !$0 { actions.closingTab = nil } }),
                                titleVisibility: .visible, presenting: actions.closingTab) { c in
                Button("关闭标签页", role: .destructive) { actions.closeTab(c) }
                Button("取消", role: .cancel) {}
            } message: { _ in
                Text("标签页里的所有窗格都会关掉，里面运行的 Agent 也会结束。")
            }
            .sheet(item: Binding(get: { actions.runningHere }, set: { actions.runningHere = $0 })) { g in
                RunAgentHereSheet(group: g)
            }
    }
}

// MARK: - Menu items

/// The node items of an agent row's long-press menu (`AgentRowMenu`): split its pane, Git in its folder, close its tab.
struct AgentNodeMenuItems: View {
    var now: () -> Session?

    var body: some View {
        let actions = NodeActions.shared
        Menu {
            ForEach(NodeMenu.Direction.allCases) { d in
                Button { if let s = now() { actions.split(s, d) } } label: { Label(d.title, systemImage: d.symbol) }
            }
        } label: { Label("拆分窗格", systemImage: Theme.Icon.splitRight).lineLimit(1) }
        Menu {
            ForEach(NodeMenu.GitOp.allCases) { op in
                Button { if let s = now() { actions.git(op, session: s) } } label: { Label(op.title, systemImage: op.symbol) }
            }
        } label: { Label("Git", systemImage: Theme.Icon.gitBranch) }
        Button(role: .destructive) { if let s = now() { actions.askCloseTab(s) } } label: {
            Label("关闭标签页…", systemImage: Theme.Icon.closeTab).lineLimit(1)
        }
    }
}

/// The node items of a space's ⋯ in Agents: run an agent in one of its shells, Git in its folder.
struct SpaceNodeMenuItems: View {
    var group: SpaceGroup

    var body: some View {
        let actions = NodeActions.shared
        Button { actions.runningHere = group } label: {
            Label("在这里运行 Agent…", systemImage: Theme.Icon.run).lineLimit(1)
        }
        Menu {
            ForEach(NodeMenu.GitOp.allCases) { op in
                Button { actions.git(op, space: group) } label: { Label(op.title, systemImage: op.symbol) }
            }
        } label: { Label("Git", systemImage: Theme.Icon.gitBranch) }
    }
}

// MARK: - 在这里运行 Agent…

/// Picks a plain shell of the space and a launcher, and starts the agent in that pane (`panes/agent`); with no shell,
/// opens a new terminal tab in the workspace first.
struct RunAgentHereSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var group: SpaceGroup

    private struct Loaded: Equatable {
        var panes: TerminalPanes
        var launchers: [Launcher]
    }

    @State private var state: Loadable<Loaded> = .loading
    @State private var pane = ""
    @State private var launcher = ""
    @State private var busy = false
    @State private var failure: String?

    private var workspaceID: String { NodeMenu.workspaceID(group) ?? "" }

    var body: some View {
        NavigationStack {
            LoadableContent(state: state, retry: { Task { await load() } }) { loaded in
                content(loaded)
            }
            .themedPage()
            .navigationTitle("在这里运行 Agent")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { dismiss() }.lineLimit(1).disabled(busy)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if busy {
                        ProgressView()
                    } else {
                        Button("运行") { run() }
                            .lineLimit(1)
                            .disabled(pane.isEmpty || launcher.isEmpty)
                    }
                }
            }
        }
        .task { await load() }
        .presentationDetents([.medium, .large])
        .interactiveDismissDisabled(busy)
    }

    @ViewBuilder private func content(_ loaded: Loaded) -> some View {
        let shells = NodeMenu.shells(in: loaded.panes, workspaceID: workspaceID)
        let launchers = loaded.launchers.filter(NodeMenu.launchableHere)
        if shells.isEmpty {
            let canOpen = NodeMenu.herdrHost(loaded.panes)?.newTab == true
            EmptyStateView(symbol: Theme.Icon.terminal, title: "没有空闲的终端",
                           message: "「\(group.name)」里的窗格都在运行 Agent。新开一个终端标签页，在那里运行。",
                           action: canOpen && !busy ? (title: "新开终端标签页", run: { openShell() }) : nil)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .padding(Theme.Space.page)
        } else {
            Form {
                Section("窗格") {
                    Picker("窗格", selection: $pane) {
                        ForEach(shells) { p in Text(NodeMenu.shellTitle(p, in: loaded.panes)).lineLimit(1).tag(p.pane) }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
                Section {
                    if launchers.isEmpty {
                        Text("这台电脑上没有能直接启动的 Claude Code 启动器").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    } else {
                        Picker("启动器", selection: $launcher) {
                            ForEach(launchers) { l in Text(l.name).lineLimit(1).tag(l.name) }
                        }
                    }
                } header: {
                    Text("启动器")
                } footer: {
                    if let failure {
                        Text(failure).foregroundStyle(Theme.danger)
                    } else {
                        Text("在选中的 shell 里启动，不新开标签页。Codex 需要第一条消息，请用「新建 Agent」。")
                    }
                }
            }
            .disabled(busy)
        }
    }

    private func load() async {
        guard let api = model.api(group.group) else { state = .failed("尚未连接 hub"); return }
        state = .loading
        do {
            async let panes = api.terminalPanes(machine: group.machineID)
            async let launchers = model.launchers(group.machineRef)
            let loaded = Loaded(panes: try await panes, launchers: try await launchers)
            let shells = NodeMenu.shells(in: loaded.panes, workspaceID: workspaceID)
            if !shells.contains(where: { $0.pane == pane }) { pane = shells.first?.pane ?? "" }
            let usable = loaded.launchers.filter(NodeMenu.launchableHere)
            if !usable.contains(where: { $0.name == launcher }) { launcher = usable.first?.name ?? "" }
            state = .loaded(loaded)
        } catch {
            state = .failed(error.shown ?? error.localizedDescription)
        }
    }

    private func openShell() {
        guard let api = model.api(group.group) else { return }
        busy = true
        Task {
            defer { busy = false }
            do {
                var body = ["workspace_id": workspaceID]
                if let kind = NodeMenu.kind(group) { body["kind"] = kind }
                try await api.nodeAction(machine: group.machineID, "panes", body)
                await load()
            } catch {
                NodeActions.shared.fail("没能新开终端", error)
            }
        }
    }

    private func run() {
        guard let api = model.api(group.group), !pane.isEmpty, !launcher.isEmpty else { return }
        busy = true
        failure = nil
        let name = launcher
        Task {
            defer { busy = false }
            do {
                try await api.nodeAction(machine: group.machineID, "panes/agent", ["pane": pane, "launcher": name], timeout: 130)
                model.toast = Toast("已在窗格里启动 \(name)", style: .success)
                Haptics.success()
                dismiss()
                await model.refresh()
            } catch {
                failure = "启动失败：\(error.shown ?? error.localizedDescription)"
                Haptics.error()
            }
        }
    }
}
