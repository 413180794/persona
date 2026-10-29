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
                VStack(spacing: 14) {
                    ForEach(model.machines) { m in
                        NavigationLink(value: MachineDetailRoute(id: m.id)) { MachineCard(machine: m) }
                            .buttonStyle(.plain)
                    }
                    if model.machines.isEmpty {
                        EmptyStateView(symbol: "macbook.and.iphone", title: "还没有机器", message: "hub 连上后，这里会显示 hub 所在的机器和它接入的各台电脑。")
                    }
                }
                .padding(16)
            }
        .themedGroupedPage()
        .navigationTitle("机器")
        // A large title stays below; the bar's middle is only for 连接中… and the like.
        .toolbar {
            ToolbarItem(placement: .principal) { ConnectionTitle() }
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink { MachineDashboardView() } label: { Image(systemName: "gauge.with.dots.needle.33percent") }
                    .accessibilityLabel("机器监控")
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
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top) {
                OSBadge(system: machine.system, fallback: machine.symbol, size: 52, online: machine.online)
                VStack(alignment: .leading, spacing: 3) {
                    // Color.primary / Color.secondary, not .primary / .secondary: as the details page's section header the
                    // list moves the hierarchical styles a step lighter (the name came out grey, the labels faint).
                    Text(machine.name).font(.title3.weight(.semibold)).foregroundStyle(Color.primary)
                    Text(machine.hostname ?? machine.sshHost ?? machine.id).font(.caption.monospaced()).foregroundStyle(Color.secondary)
                }
                Spacer()
                Chip(text: machine.online ? "在线" : "离线", symbol: "circle.fill", look: machine.online ? .tint(Theme.positive) : .neutral)
            }
            if machine.online {
                HStack(spacing: 0) {
                    stat("\(live.count)", "运行中")
                    Divider().frame(height: 28)
                    stat("\(working)", "工作中", tint: working > 0 ? StatusPalette.working.text : nil)
                    Divider().frame(height: 28)
                    stat("\(waiting)", "需要你", tint: waiting > 0 ? StatusPalette.needsYou.text : nil)
                }
                HStack(spacing: 6) {
                    if let sys = machine.system { Chip(text: sys.name, symbol: "cpu") }
                    else if let os = machine.os { Chip(text: "\(os)/\(machine.arch ?? "")", symbol: "cpu") }
                    if let h = machine.herdrVersion, !h.isEmpty { Chip(text: "herdr \(h)") }
                    if let c = machine.claudeVersion, !c.isEmpty { Chip(text: "claude \(c)") }
                }
                if machine.nodeMismatch == true {
                    Label("这台电脑上的 Jarvis 和 hub 不是同一次部署，重新部署一下", systemImage: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(Theme.cautionText)
                }
            } else if let err = machine.error, !err.isEmpty {
                Text(err).font(.caption).foregroundStyle(Color.secondary).lineLimit(3)
            }
        }
        .card(padding: 16)
    }

    private func stat(_ value: String, _ label: String, tint: Color? = nil) -> some View {
        VStack(spacing: 2) {
            Text(value).font(.title2.weight(.semibold).monospacedDigit()).foregroundStyle(tint ?? Color.primary)
            Text(label).font(.caption).foregroundStyle(Color.secondary)
        }
        .frame(maxWidth: .infinity)
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
                    VStack(alignment: .leading, spacing: 16) {
                        MachineCard(machine: machine)
                        Text("运行中的 Claude").padding(.leading, 16) // where the header text sits by default
                    }
                    .textCase(nil)
                    .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 8, trailing: 0))
                }
                Section {
                    NavigationLink { MachineMonitorView(machineID: machineID) } label: {
                        Label("系统与监控", systemImage: "gauge.with.dots.needle.33percent")
                    }
                }
                Section {
                    if !machine.online {
                        Text("机器离线，连上后显示").foregroundStyle(.secondary)
                    } else if let launcherError, launchers.isEmpty {
                        Label(launcherError, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
                    }
                    ForEach(launchers) { l in
                        HStack {
                            Text(l.name).font(.callout.monospaced())
                            Spacer()
                            Text(Fmt.model(l.model) ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                } header: { Text(machine.online ? "启动器 \(launchers.count)" : "启动器") }
                Section {
                    Button {
                        Task { await reconnect() }
                    } label: {
                        HStack {
                            Label(reconnecting ? "正在重新连接…" : "重新连接", systemImage: "arrow.clockwise").lineLimit(1)
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
