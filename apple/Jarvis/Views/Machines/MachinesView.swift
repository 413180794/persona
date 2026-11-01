import SwiftUI

/// The machine pages are pushed by value (MachineListRoute, MachineDetailRoute) and so are the chats they open
/// (SessionKey): a page pushed with `NavigationLink { … }` that pushes by value puts the new page *under* itself, so a
/// tap seemed to do nothing (设置 › 机器 › a card; 机器详情 › an agent). Every stack that shows them registers both.
struct MachineListRoute: Hashable {}
struct MachineDetailRoute: Hashable { var id: String }

/// Settings › 机器.
struct MachinesView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ScrollView {
                VStack(spacing: Theme.Space.l) {
                    ForEach(model.machines) { m in
                        NavigationLink(value: MachineDetailRoute(id: m.id)) { MachineCard(machine: m) }
                            .buttonStyle(.plain)
                    }
                    if model.machines.isEmpty {
                        EmptyStateView(symbol: Theme.Icon.machines, title: "还没有机器", message: "hub 连上后，这里会显示 hub 所在的机器和它接入的各台电脑。")
                    }
                }
                .padding(Theme.Space.l)
            }
        .themedGroupedPage()
        .navigationTitle("机器")
        // A large title stays below; the bar's middle is only for 连接中… and the like.
        .toolbar {
            ToolbarItem(placement: .principal) { ConnectionTitle() }
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink { MachineDashboardView() } label: { Label("机器监控", systemImage: Theme.Icon.context) }
            }
        }
        .refreshable { await model.refresh() }
    }
}

struct MachineCard: View {
    @Environment(AppModel.self) private var model
    var machine: Machine

    var body: some View {
        let live = MachineDetailView.agents(model, machine.id)
        let working = live.filter { $0.state == .working }.count
        let waiting = live.filter { $0.state == .needsYou }.count
        VStack(alignment: .leading, spacing: Theme.Space.l) {
            HStack(alignment: .top) {
                OSBadge(system: machine.system, fallback: machine.symbol, size: 52, online: machine.online)
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    // Color.primary / Color.secondary, not .primary / .secondary: as the details page's section header the
                    // list moves the hierarchical styles a step lighter (the name came out grey, the labels faint).
                    Text(machine.name).font(Theme.Font.title).foregroundStyle(Color.primary)
                    Text(machine.hostname ?? machine.sshHost ?? machine.id).font(Theme.Font.code).foregroundStyle(Color.secondary)
                }
                Spacer()
                Chip(text: machine.online ? "在线" : "离线", symbol: Theme.Icon.dot, look: machine.online ? .tint(Theme.positive) : .neutral)
            }
            if machine.online {
                HStack(spacing: 0) {
                    stat("\(live.count)", "运行中")
                    Divider().scaledFrame(height: Theme.Size.wellXS)
                    stat("\(working)", "工作中", tint: working > 0 ? StatusPalette.working.dot : nil) // the orange the working dots are
                    Divider().scaledFrame(height: Theme.Size.wellXS)
                    stat("\(waiting)", "需要你", tint: waiting > 0 ? StatusPalette.needsYou.text : nil)
                }
                // Wraps: OS, herdr and claude side by side are wider than a phone, and Chip does not shrink, so one row
                // pushed the whole card (and the page) past both edges of the screen.
                FlowLayoutRow {
                    if let sys = machine.system { Chip(text: sys.name, symbol: Theme.Icon.cpu, shrinks: true) }
                    else if let os = machine.os { Chip(text: "\(os)/\(machine.arch ?? "")", symbol: Theme.Icon.cpu, shrinks: true) }
                    if let h = machine.herdrVersion, !h.isEmpty { Chip(text: "herdr \(h)") }
                    if let c = machine.claudeVersion, !c.isEmpty { Chip(text: "claude \(c)") }
                }
                if machine.nodeMismatch == true || machine.deploying == true {
                    DeployRow(machine: machine)
                }
            } else if let err = machine.error, !err.isEmpty {
                Text(err).font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(3)
            }
        }
        .card(padding: 16)
    }

    private func stat(_ value: String, _ label: String, tint: Color? = nil) -> some View {
        VStack(spacing: Theme.Space.xxs) {
            Text(value).font(Theme.Font.title.monospacedDigit()).foregroundStyle(tint ?? Color.primary)
            Text(label).font(Theme.Font.label).foregroundStyle(Color.secondary)
        }
        .frame(maxWidth: .infinity)
    }
}

/// A machine whose jarvisd is not the hub's build: says so, and 重新部署 has the hub install its own build there.
private struct DeployRow: View {
    @Environment(AppModel.self) private var model
    var machine: Machine
    @State private var starting = false

    var body: some View {
        let busy = machine.deploying == true || starting
        HStack(spacing: Theme.Space.m) {
            Label(text(busy), systemImage: busy ? Theme.Icon.retry : Theme.Icon.errorFilled)
                .font(Theme.Font.label)
                .foregroundStyle(Theme.cautionText)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button { Task { await deploy() } } label: {
                if busy {
                    ProgressView().controlSize(.small)
                } else {
                    Text("重新部署").font(Theme.Font.labelStrong).lineLimit(1)
                }
            }
            .themedButton()
            .disabled(busy)
            .accessibilityLabel(busy ? "正在部署" : "重新部署")
        }
    }

    private func text(_ busy: Bool) -> String {
        if busy { return "正在把 hub 这一版的 Jarvis 装到这台电脑…" }
        if let e = machine.deployError, !e.isEmpty { return "上次部署没成：\(e)" }
        return "这台电脑上的 Jarvis 和 hub 不是同一次部署"
    }

    private func deploy() async {
        starting = true
        defer { starting = false }
        do {
            try await model.api?.deploy(machine: machine.id)
            Haptics.tap()
            await model.refresh()
        } catch {
            if let m = error.shown { model.toast = Toast("部署失败：\(m)", style: .error) }
        }
    }
}

struct MachineDetailView: View {
    @Environment(AppModel.self) private var model
    var machineID: String
    @State private var launchers: [Launcher] = []
    @State private var launcherError: String?
    @State private var reconnecting = false

    /// A machine's agents: live ones, not the 对话 tab's assistant conversations (the Agents tab leaves those out too).
    static func agents(_ model: AppModel, _ machineID: String) -> [Session] {
        (model.sessionsByMachine[machineID] ?? []).filter { $0.status.isLive && !$0.assistant }
    }

    var body: some View {
        let machine = model.machine(machineID)
        let sessions = Self.agents(model, machineID)
        List {
            if let machine {
                Section {
                    if sessions.isEmpty {
                        Text(machine.online ? "没有运行中的会话" : "机器离线，连上后显示它的会话").foregroundStyle(.secondary)
                    }
                    ForEach(sessions) { s in
                        NavigationLink(value: s.key) { SessionRow(session: s, machine: machine, showMachine: false) }
                    }
                } header: {
                    // The card is the header, not a row: as a row it was tappable and pushed this page again.
                    VStack(alignment: .leading, spacing: Theme.Space.l) {
                        MachineCard(machine: machine)
                        Text("运行中的 Claude").padding(.leading, Theme.Space.l) // where the header text sits by default
                    }
                    .textCase(nil)
                    .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 8, trailing: 0))
                }
                Section {
                    NavigationLink {
                        EngineSelectionView(machineID: machineID)
                    } label: {
                        HStack {
                            Text("引擎").lineLimit(1)
                            Spacer()
                            Text(machine.engineName).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                    .accessibilityIdentifier("machine.engine")
                } footer: {
                    Text("只显示所选引擎里的会话。")
                }
                Section {
                    NavigationLink { MachineMonitorView(machineID: machineID) } label: {
                        Label("系统与监控", systemImage: Theme.Icon.context)
                    }
                }
                Section {
                    if !machine.online {
                        Text("机器离线，连上后显示").foregroundStyle(.secondary)
                    } else if let launcherError, launchers.isEmpty {
                        Label(launcherError, systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(Theme.danger)
                    }
                    ForEach(launchers) { l in
                        HStack {
                            Text(l.name).font(Theme.Font.body.monospaced())
                            Spacer()
                            Text(Fmt.model(l.model) ?? "").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                } header: { Text(machine.online ? "启动器 \(launchers.count)" : "启动器") }
                Section {
                    Button {
                        Task { await reconnect() }
                    } label: {
                        HStack {
                            Label(reconnecting ? "正在重新连接…" : "重新连接", systemImage: Theme.Icon.retry).lineLimit(1)
                            if reconnecting { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(reconnecting)
                } footer: {
                    Text(machine.kind == "local" ? "hub 所在机器，直接运行 node。" : "hub 通过 `ssh \(machine.sshHost ?? machine.id)` 接入。")
                }
            }
        }
        .themedPage()
        .navigationTitle(machine?.name ?? machineID)
        .navigationBarTitleDisplayMode(.inline)
        // Again when it comes online (after 重新连接): an offline machine has no launchers to list.
        .task(id: machine?.online == true) { await loadLaunchers(refresh: false) }
    }

    private func loadLaunchers(refresh: Bool) async {
        guard model.machine(machineID)?.online == true else { launchers = []; return }
        do {
            launchers = try await model.launchers(machineID, refresh: refresh)
            launcherError = nil
        } catch {
            launcherError = error.shown.map { "读不到启动器：\($0)" }
        }
    }

    /// Asks the hub to reconnect, then waits (up to 15 s) for the machine to come online; says so if it does not.
    private func reconnect() async {
        reconnecting = true
        defer { reconnecting = false }
        do {
            try await model.api?.reconnect(machine: machineID)
        } catch {
            if let m = error.shown { model.toast = Toast("重新连接失败：\(m)", style: .error) }
            return
        }
        for _ in 0..<10 {
            try? await Task.sleep(for: .seconds(1.5))
            await model.refresh()
            if model.machine(machineID)?.online == true {
                Haptics.success()
                await loadLaunchers(refresh: true)
                return
            }
        }
        let err = model.machine(machineID)?.error
        model.toast = Toast("还没连上\(err.map { "：\($0)" } ?? "")", style: .error)
    }
}
