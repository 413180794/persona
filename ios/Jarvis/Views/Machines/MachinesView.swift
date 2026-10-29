import SwiftUI

/// Settings › 机器. Lives inside the settings NavigationStack (which registers the String / SessionKey destinations).
struct MachinesView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ScrollView {
                VStack(spacing: 14) {
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
        .themedGroupedPage()
        .navigationTitle("机器")
        // A large title stays below; the bar's middle is only for 连接中… and the like.
        .toolbar { ToolbarItem(placement: .principal) { ConnectionTitle() } }
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
                    RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                        .fill((machine.online ? Theme.accent : Color.secondary).opacity(0.12))
                        .frame(width: 52, height: 52)
                    Image(systemName: machine.symbol)
                        .font(.title2.weight(.medium)).dynamicTypeSize(...DynamicTypeSize.xxLarge)
                        .foregroundStyle(machine.online ? Theme.accent : .secondary)
                }
                VStack(alignment: .leading, spacing: 3) {
                    Text(machine.name).font(.title3.weight(.semibold))
                    Text(machine.hostname ?? machine.sshHost ?? machine.id).font(.caption.monospaced()).foregroundStyle(.secondary)
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
                    if let os = machine.os { Chip(text: "\(os)/\(machine.arch ?? "")", symbol: "cpu") }
                    if let h = machine.herdrVersion, !h.isEmpty { Chip(text: "herdr \(h)") }
                    if let c = machine.claudeVersion, !c.isEmpty { Chip(text: "claude \(c)") }
                }
                if machine.nodeMismatch == true {
                    Label("这台电脑上的 Jarvis 和 hub 不是同一次部署，重新部署一下", systemImage: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(Theme.cautionText)
                }
            } else if let err = machine.error, !err.isEmpty {
                Text(err).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
        }
        .card(padding: 16)
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
                    if sessions.isEmpty {
                        Text("没有运行中的会话").foregroundStyle(.secondary)
                    }
                    ForEach(sessions) { s in
                        NavigationLink(value: s.key) { SessionRow(session: s, machine: machine, showMachine: false) }
                    }
                } header: {
                    // The card is the header, not a row: as a row it was tappable and pushed this page again.
                    VStack(alignment: .leading, spacing: 16) {
                        MachineCard(machine: machine).foregroundStyle(.primary)
                        Text("运行中的 Claude").padding(.leading, 16) // where the header text sits by default
                    }
                    .textCase(nil)
                    .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 8, trailing: 0))
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
                    Text(machine.kind == "local" ? "hub 所在机器，直接运行 node。" : "hub 通过 `ssh \(machine.sshHost ?? machine.id)` 接入。")
                }
            }
        }
        .themedPage()
        .navigationTitle(machine?.name ?? machineID)
        .navigationBarTitleDisplayMode(.inline)
        .task { launchers = (try? await model.launchers(machineID)) ?? [] }
    }
}
