import SwiftUI

/// Agents in the same space shown together: switch between them without leaving the chat.
struct SiblingStrip: View {
    @Environment(AppModel.self) private var model
    var current: SessionKey
    var select: (SessionKey) -> Void

    var body: some View {
        let siblings = model.siblings(of: current)
        if siblings.count > 1 {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    if let name = model.session(current)?.spaceName {
                        Label(name, systemImage: "square.stack.3d.up")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .padding(.trailing, 2)
                    }
                    ForEach(siblings) { s in
                        let on = s.key == current
                        Button { select(s.key) } label: {
                            HStack(spacing: 5) {
                                StatusDot(state: s.state, size: 7)
                                Text(s.slotLabel ?? s.displayTitle).lineLimit(1)
                            }
                            .font(.caption.weight(on ? .bold : .medium))
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(on ? Theme.accent.opacity(0.16) : Color.primary.opacity(0.05), in: .capsule)
                            .overlay(Capsule().stroke(on ? Theme.accent.opacity(0.5) : .clear, lineWidth: 1))
                            .foregroundStyle(on ? Theme.accent : Color.primary)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("\(s.slotLabel ?? s.displayTitle)，\(s.state.label)")
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
            }
            .background(.bar)
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
            .sorted { ($0.slotIndex ?? 0, $0.sessionID) < ($1.slotIndex ?? 0, $1.sessionID) }
    }

    var body: some View {
        let sessions = sessions
        let first = sessions.first
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let first {
                    HStack(spacing: 8) {
                        Image(systemName: model.machine(first.machineID)?.symbol ?? "laptopcomputer")
                        Text(model.machine(first.machineID)?.name ?? first.machineID)
                        Text("·")
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
                        .background(Theme.accent.opacity(0.06), in: .rect(cornerRadius: 18, style: .continuous))
                        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Theme.accent.opacity(0.3), style: StrokeStyle(lineWidth: 1, dash: [5])))
                    }
                    .buttonStyle(.plain)
                    .disabled(first == nil)
                }
            }
            .padding(16)
        }
        .background(Color(.systemGroupedBackground))
        .navigationTitle(first?.spaceName ?? "空间")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            if let g = SpaceGroup.build(sessions).first {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu { SpaceMenuItems(group: g) { renaming = g } } label: {
                        Image(systemName: model.isMuted(g) ? "bell.slash" : "ellipsis")
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
        .navigationDestination(item: $started) { key in ChatView(key: key) }
        .renameSpaceAlert($renaming)
    }
}

struct SlotCard: View {
    var session: Session
    var pending: Int

    var body: some View {
        let tint = session.state == .idle || session.state == .unknown ? Color.secondary : session.state.color
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(session.slotLabel ?? "—")
                    .font(.caption.weight(.bold).monospaced())
                    .foregroundStyle(tint)
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(tint.opacity(0.12), in: .capsule)
                Spacer()
                StatusDot(state: session.state, size: 9)
            }
            Text(session.displayTitle).font(.subheadline.weight(.semibold)).lineLimit(2)
            Text(session.state.label + (pending > 0 ? " · \(pending) 件待处理" : ""))
                .font(.caption.weight(.semibold))
                .foregroundStyle(tint)
            if let t = session.lastText, !t.isEmpty {
                Text(Fmt.plain(t)).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
            Spacer(minLength: 0)
            if let m = Fmt.model(session.model) { Text(m).font(.caption2).foregroundStyle(.tertiary).lineLimit(1) }
        }
        .frame(maxWidth: .infinity, minHeight: 150, alignment: .topLeading)
        .card(padding: 12, radius: 18)
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).stroke(session.state == .needsYou ? Theme.needsYou.opacity(0.5) : .clear, lineWidth: 1.5))
    }
}
