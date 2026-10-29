import SwiftUI

struct MachineRoute: Hashable { var id: String }

/// One machine's projects (herdr spaces): the busy ones with their agents' counts and latest line, the idle ones
/// below; tap one for its page. Opened from a project page's machine name.
struct MachineSpacesView: View {
    @Environment(AppModel.self) private var model
    var machineID: String

    var body: some View {
        let machine = model.machine(machineID)
        let sessions = model.allSessions.filter { $0.machineID == machineID && $0.state.isLive && !$0.assistant }
        let groups = SpaceGroup.build(sessions)
        let pinned = groups.filter { model.isPinned($0) }
        let active = pinned + groups.filter { !$0.isQuiet && !model.isPinned($0) }
        let quiet = groups.filter { $0.isQuiet && !model.isPinned($0) }

        List {
            if let machine {
                Section {
                    summary(machine, sessions: sessions, spaces: groups.count)
                }
            }
            if !active.isEmpty {
                Section {
                    ForEach(active) { g in row(g, busy: true) }
                } header: {
                    SectionHeader(title: "进行中", count: active.count, tone: StatusPalette.working)
                }
            }
            if !quiet.isEmpty {
                Section {
                    ForEach(quiet) { g in row(g, busy: false) }
                } header: {
                    SectionHeader(title: "空闲", count: quiet.count, tone: StatusPalette.idle)
                }
            }
            if groups.isEmpty {
                Section {
                    EmptyStateView(symbol: machine?.symbol ?? "desktopcomputer", title: "没有在跑的 Agent",
                                   message: machine?.online == false ? "这台机器现在离线。" : "在这台机器的 herdr 里启动 Claude、Codex 或 TRAEX，就会出现在这里。")
                }
                .listRowBackground(Color.clear)
            }
        }
        .listStyle(.insetGrouped)
        .themedPage()
        .navigationTitle(machine?.name ?? machineID)
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                // By value, like the chats the details page opens (MachineDetailRoute).
                NavigationLink(value: MachineDetailRoute(id: machineID)) { Image(systemName: "info.circle") }
                    .accessibilityLabel("机器详情")
            }
        }
    }

    private func summary(_ m: Machine, sessions: [Session], spaces: Int) -> some View {
        HStack(spacing: 12) {
            Image(systemName: m.symbol)
                .font(.title2)
                .foregroundStyle(m.online ? Theme.accent : .secondary)
                .frame(width: 44, height: 44)
                .background((m.online ? Theme.accent : Color.secondary).opacity(0.12), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Circle().fill(m.online ? Theme.positive : Color.secondary).frame(width: 7, height: 7)
                    Text(m.online ? "在线" : "离线").font(.subheadline.weight(.semibold))
                    if let h = m.hostname, !h.isEmpty { Text(h).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1) }
                }
                Text("\(spaces) 个项目 · \(sessions.count) 个 Agent").font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 4)
            StateCounts(group: SpaceGroup(id: "m-" + m.id, machineID: m.id, spaceID: "-", name: m.name, index: 0, sessions: sessions))
        }
        .padding(.vertical, 2)
    }

    /// A project: opens its page. Agents outside any herdr space have no page: the row opens the agent itself.
    @ViewBuilder private func row(_ g: SpaceGroup, busy: Bool) -> some View {
        if g.spaceID == "-" {
            ForEach(g.sessions) { s in
                NavigationLink(value: s.key) { SlotRow(session: s, muted: model.isMuted(s.key)) }
            }
        } else {
            NavigationLink(value: SpaceRoute(id: g.id)) {
                if busy { BusySpaceRow(group: g, pinned: model.isPinned(g), muted: model.isMuted(g)) }
                else { QuietSpaceRow(group: g, machine: nil, showMachine: false) }
            }
        }
    }
}

/// A project with something going on: its name and counts, then the line of the agent that most wants attention.
private struct BusySpaceRow: View {
    var group: SpaceGroup
    var pinned: Bool
    var muted: Bool

    var body: some View {
        let lead = group.sessions.min { ($0.state.priority, -($0.updatedAt ?? .distantPast).timeIntervalSince1970) < ($1.state.priority, -($1.updatedAt ?? .distantPast).timeIntervalSince1970) }
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Image(systemName: "square.stack.3d.up.fill").foregroundStyle(muted ? Color.secondary : group.state.color)
                Text(group.name).font(.body.weight(.semibold)).lineLimit(1)
                if pinned { Image(systemName: "pin.fill").font(.caption2).foregroundStyle(Theme.accent).accessibilityLabel("已置顶") }
                if muted { Image(systemName: "bell.slash.fill").font(.caption2).foregroundStyle(.secondary).accessibilityLabel("已屏蔽") }
                Spacer(minLength: 4)
                StateCounts(group: group).opacity(muted ? 0.6 : 1)
            }
            if let lead {
                HStack(spacing: 6) {
                    Text(lead.slotLabel ?? lead.displayTitle).fontWeight(.medium).lineLimit(1).layoutPriority(1)
                    if let t = lead.lastText, !t.isEmpty { Text(t).foregroundStyle(.secondary).lineLimit(1) }
                    Spacer(minLength: 4)
                    Text(Fmt.relative(group.updatedAt)).foregroundStyle(.tertiary).lineLimit(1).fixedSize()
                }
                .font(.caption)
                .padding(.leading, 28)
            }
        }
        .padding(.vertical, 2)
        .opacity(muted ? 0.8 : 1)
    }
}
