import SwiftUI

extension EnvironmentValues {
    /// Opens a space's page (its agents) from a chat; set on the dev tab's NavigationStack.
    @Entry var openSpace: ((String) -> Void)? = nil
}

/// The other agents of the space, from the chat's header: a button under the title (how many; a red dot when another
/// one needs you) whose menu switches to any of them or opens the space's page. It used to be a strip on a row of its own.
struct SiblingMenu: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openSpace) private var openSpace
    var current: SessionKey
    var select: (SessionKey) -> Void

    var body: some View {
        let siblings = model.siblings(of: current)
        if siblings.count > 1 {
            let waiting = siblings.contains { $0.key != current && $0.state == .needsYou }
            Menu {
                if let s = model.session(current), let name = s.spaceName, let k = s.spaceKey, let openSpace {
                    Button { openSpace(k) } label: { Label("空间 \(name) 的所有 Agent", systemImage: "square.stack.3d.up") }
                    Divider()
                }
                ForEach(siblings) { s in
                    Button { if s.key != current { select(s.key) } } label: {
                        Label {
                            Text(s.slotLabel ?? s.displayTitle)
                            Text(s.state.label)
                        } icon: {
                            if s.key == current { Image(systemName: "checkmark") }
                        }
                    }
                }
            } label: {
                HStack(spacing: 4) {
                    Image(systemName: "square.stack.3d.up").font(.footnote.weight(.semibold)).dynamicTypeSize(...DynamicTypeSize.xxLarge)
                    Text("\(siblings.count)").font(.footnote.weight(.semibold).monospacedDigit())
                }
                .foregroundStyle(.secondary)
                .padding(.horizontal, 10)
                .frame(height: 36)
                .background(Theme.Fill.well, in: .capsule)
                .overlay(alignment: .topTrailing) {
                    if waiting { StatusDot(state: .needsYou, size: 8).offset(x: 1, y: -1) }
                }
                .contentShape(.capsule)
            }
            .tint(.secondary) // neutral like the chips beside it, not the accent a menu label takes
            .accessibilityLabel("同一空间的 \(siblings.count) 个 Agent" + (waiting ? "，有 Agent 需要你" : ""))
        }
    }
}

/// A space (herdr workspace) at a glance: every agent with its state and latest line; start another tab in it.
struct SpaceView: View {
    @Environment(AppModel.self) private var model
    var spaceKey: String
    @State private var showNew = false
    @State private var started: SessionKey?
    @State private var renaming: SpaceGroup?

    private var sessions: [Session] {
        model.allSessions.filter { $0.spaceKey == spaceKey && $0.state.isLive }
            .sorted { ($0.state.priority, $0.slotIndex ?? 0, $0.sessionID) < ($1.state.priority, $1.slotIndex ?? 0, $1.sessionID) }
    }

    var body: some View {
        let sessions = sessions
        let first = sessions.first
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let first {
                    HStack(spacing: 8) {
                        // The machine opens its own list of projects.
                        NavigationLink(value: MachineRoute(id: first.machineID)) {
                            HStack(spacing: 5) {
                                Image(systemName: model.machine(first.machineID)?.symbol ?? "laptopcomputer")
                                Text(model.machine(first.machineID)?.name ?? first.machineID).lineLimit(1)
                                Image(systemName: "chevron.right").font(.caption2.weight(.bold))
                            }
                            .fontWeight(.semibold)
                            .foregroundStyle(Theme.accent)
                            .padding(.horizontal, 10)
                            .padding(.vertical, 5)
                            .background(Theme.accent.opacity(0.1), in: .capsule)
                            .contentShape(.capsule)
                        }
                        .buttonStyle(.plain)
                        .layoutPriority(1)
                        .accessibilityLabel("\(model.machine(first.machineID)?.name ?? first.machineID) 上的所有项目")
                        Text(Fmt.path(first.cwd, home: model.machine(first.machineID)?.home)).lineLimit(1).truncationMode(.head)
                    }
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                }
                LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], spacing: 12) {
                    ForEach(sessions) { s in
                        NavigationLink(value: s.key) { SlotCard(session: s, pending: model.interactions(for: s.key).count) }
                            .buttonStyle(.plain)
                    }
                    Button { showNew = true } label: {
                        VStack(spacing: 8) {
                            Image(systemName: "plus").font(.title2.weight(.semibold))
                            Text("在这个空间开新标签").font(.footnote.weight(.medium))
                        }
                        .foregroundStyle(Theme.accent)
                        .frame(maxWidth: .infinity, minHeight: 150)
                        .background(Theme.accent.opacity(0.06), in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
                        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).strokeBorder(Theme.accent.opacity(0.3), style: StrokeStyle(lineWidth: 1, dash: [5])))
                    }
                    .buttonStyle(.plain)
                    .disabled(first == nil)
                }
            }
            .padding(16)
        }
        .themedGroupedPage()
        .navigationTitle(first?.spaceName ?? "空间")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            if let g = SpaceGroup.build(sessions).first {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu { SpaceMenuItems(group: g) { renaming = g } } label: {
                        Label("空间设置", systemImage: model.isMuted(g) ? "bell.slash" : "ellipsis")
                    }
                    .accessibilityLabel("空间操作")
                }
            }
        }
        .sheet(isPresented: $showNew) {
            if let first, let ws = first.spaceID {
                NewChatView(preset: SpacePreset(machineID: first.machineID, cwd: first.cwd, workspaceID: ws, spaceName: first.spaceName ?? "")) { s in
                    showNew = false
                    started = s.key
                }
            }
        }
        .navigationDestination(item: $started) { key in ChatView(key: key).id(key) }
        .renameSpaceAlert($renaming)
    }
}

struct SlotCard: View {
    var session: Session
    var pending: Int

    var body: some View {
        let tone = session.state.tone
        VStack(alignment: .leading, spacing: 8) {
            // Chips don't shrink: in a narrow column (two cards a row on a phone) a long tab name pushed the card wider
            // than its column and the whole grid off the screen. Drop the agent chip first, then let the name truncate.
            ViewThatFits(in: .horizontal) {
                header(agent: true)
                header(agent: false)
                HStack(spacing: 4) {
                    Text(session.slotLabel ?? "—")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(tone.text)
                        .lineLimit(1)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 2.5)
                        .background(tone.tint, in: .capsule)
                    Spacer(minLength: 4)
                    StatusDot(state: session.state, size: 9)
                }
            }
            Text(session.displayTitle).font(.subheadline.weight(.semibold)).lineLimit(2)
            Text(session.state.label + (pending > 0 ? " · \(pending) 件待处理" : ""))
                .font(.caption.weight(.semibold))
                .foregroundStyle(tone.text)
            if let t = session.lastText, !t.isEmpty {
                Text(Fmt.plain(t)).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
            Spacer(minLength: 0)
            if let m = Fmt.model(session.model) { Text(m).font(.caption2).foregroundStyle(.tertiary).lineLimit(1) }
        }
        .frame(minWidth: 0, maxWidth: .infinity, minHeight: 150, alignment: .topLeading)
        .card(padding: 12)
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(session.state == .needsYou ? Theme.needsYou.opacity(0.5) : .clear, lineWidth: 1.5))
    }

    private func header(agent: Bool) -> some View {
        HStack {
            Chip(text: session.slotLabel ?? "—", look: .tone(session.state.tone))
            if agent { Chip(text: session.agentLabel, look: .outline) }
            Spacer(minLength: 4)
            StatusDot(state: session.state, size: 9)
        }
    }
}
