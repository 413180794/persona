import SwiftUI

/// Settings › 机器. Lives inside the settings NavigationStack (which registers the String / SessionKey destinations).
struct MachinesView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ScrollView {
                VStack(spacing: 14) {
                    ConnectionBanner()
                    ForEach(model.machines) { m in
                        NavigationLink(value: m.id) { MachineCard(machine: m) }
                            .buttonStyle(.plain)
                    }
                    if model.machines.isEmpty {
                        EmptyStateView(symbol: "macbook.and.iphone", title: "还没有机器", message: "hub 连上后，这里会显示 hub 所在的机器和它接入的各台电脑。")
                    }
                }
                .padding(16)
            }
        .background(Color(.systemGroupedBackground))
        .navigationTitle("机器")
        .refreshable { await model.refresh() }
    }
}

struct MachineCard: View {
    @Environment(AppModel.self) private var model
    var machine: Machine

    var body: some View {
        let sessions = model.sessionsByMachine[machine.id] ?? []
        let live = sessions.filter { $0.status.isLive }
        let working = live.filter { $0.state == .working }.count
        let waiting = live.filter { $0.state == .needsYou }.count
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top) {
                ZStack {
                    RoundedRectangle(cornerRadius: 14, style: .continuous)
                        .fill((machine.online ? Theme.accent : Color.secondary).opacity(0.12))
                        .frame(width: 52, height: 52)
                    Image(systemName: machine.symbol)
                        .font(.system(size: 24, weight: .medium))
                        .foregroundStyle(machine.online ? Theme.accent : .secondary)
                }
                VStack(alignment: .leading, spacing: 3) {
                    Text(machine.name).font(.title3.weight(.semibold))
                    Text(machine.hostname ?? machine.sshHost ?? machine.id).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer()
                HStack(spacing: 5) {
                    Circle().fill(machine.online ? Theme.idle : Theme.danger.opacity(0.8)).frame(width: 8, height: 8)
                    Text(machine.online ? "在线" : "离线").font(.caption.weight(.semibold))
                        .foregroundStyle(machine.online ? Theme.idle : .secondary)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(Color.primary.opacity(0.05), in: .capsule)
            }
            if machine.online {
                HStack(spacing: 0) {
                    stat("\(live.count)", "运行中")
                    Divider().frame(height: 28)
                    stat("\(working)", "工作中", tint: working > 0 ? Theme.busy : nil)
                    Divider().frame(height: 28)
                    stat("\(waiting)", "需要你", tint: waiting > 0 ? Theme.needsYou : nil)
                }
                HStack(spacing: 6) {
                    if let os = machine.os { Chip(text: "\(os)/\(machine.arch ?? "")", symbol: "cpu") }
                    if let h = machine.herdrVersion, !h.isEmpty { Chip(text: "herdr \(h)") }
                    if let c = machine.claudeVersion, !c.isEmpty { Chip(text: "claude \(c)", tint: Theme.accent) }
                }
                if machine.nodeMismatch == true {
                    Label("这台电脑上的 Jarvis 和 hub 不是同一次部署，重新部署一下", systemImage: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(Theme.needsYou)
                }
            } else if let err = machine.error, !err.isEmpty {
                Text(err).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
        }
        .card(padding: 16, radius: 22)
    }

    private func stat(_ value: String, _ label: String, tint: Color? = nil) -> some View {
        VStack(spacing: 2) {
            Text(value).font(.title2.weight(.semibold).monospacedDigit()).foregroundStyle(tint ?? .primary)
            Text(label).font(.caption).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
    }
}

struct MachineDetailView: View {
    @Environment(AppModel.self) private var model
    var machineID: String
    @State private var launchers: [Launcher] = []
    @State private var reconnecting = false

    var body: some View {
        let machine = model.machine(machineID)
        let sessions = (model.sessionsByMachine[machineID] ?? []).filter { $0.status.isLive }
        List {
            if let machine {
                Section {
                    MachineCard(machine: machine).listRowInsets(EdgeInsets()).listRowBackground(Color.clear)
                }
                Section("运行中的 Claude") {
                    if sessions.isEmpty {
                        Text("没有运行中的会话").foregroundStyle(.secondary)
                    }
                    ForEach(sessions) { s in
                        NavigationLink(value: s.key) { SessionRow(session: s, machine: machine, showMachine: false) }
                    }
                }
                Section {
                    ForEach(launchers) { l in
                        HStack {
                            Text(l.name).font(.callout.monospaced())
                            Spacer()
                            Text(Fmt.model(l.model) ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                } header: { Text("启动器 \(launchers.count)") }
                Section {
                    Button {
                        Task {
                            reconnecting = true
                            try? await model.api?.reconnect(machine: machineID)
                            try? await Task.sleep(for: .seconds(2))
                            await model.refresh()
                            reconnecting = false
                        }
                    } label: {
                        HStack {
                            Label("重新连接", systemImage: "arrow.clockwise")
                            if reconnecting { Spacer(); ProgressView() }
                        }
                    }
                } footer: {
                    Text(machine.kind == "local" ? "hub 所在机器，直接运行 node。" : "hub 通过 `ssh \(machine.sshHost ?? machine.id)` 接入（example-machine 反向隧道）。")
                }
            }
        }
        .navigationTitle(machine?.name ?? machineID)
        .navigationBarTitleDisplayMode(.inline)
        .task { launchers = (try? await model.launchers(machineID)) ?? [] }
    }
}
