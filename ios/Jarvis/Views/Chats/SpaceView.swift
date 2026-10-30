import SwiftUI

extension EnvironmentValues {
    /// Opens a space's page (its agents) from a chat; set on the dev tab's NavigationStack.
    @Entry var openSpace: ((String) -> Void)? = nil
}

/// The space's tabs (its agents) in the chat's header, one capsule each in its state's color (no dots: the pulsing ones
/// were too busy in a row of them), the current one bold with a ring. As many as the width holds show around the current one; a swipe scrolls through the rest. ⋯ at the end
/// lists every tab with its state, and the space's page, for a quick pick.
struct SiblingTabs: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openSpace) private var openSpace
    var current: SessionKey
    var select: (SessionKey) -> Void
    /// The strip's own scroll position (not ScrollViewReader: its scrollTo drags the page above along).
    @State private var shown: SessionKey?
    @State private var showAll = false

    var body: some View {
        let siblings = model.siblings(of: current)
        if siblings.count > 1 {
            HStack(spacing: 6) {
                strip(siblings)
                allButton(siblings)
            }
        }
    }

    private func strip(_ siblings: [Session]) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(siblings) { s in tab(s) }
            }
            .scrollTargetLayout()
            .padding(.horizontal, 2)
        }
        .scrollTargetBehavior(.viewAligned)
        // A space with many tabs opens on the current one and its neighbours, not the first few.
        .scrollPosition(id: $shown, anchor: .center)
        .onAppear { shown = current }
        .onChange(of: current) { _, k in withAnimation(.snappy) { shown = k } }
        .frame(maxWidth: .infinity)
    }

    private func tab(_ s: Session) -> some View {
        let on = s.key == current
        let tone = s.state.tone
        let quiet = s.state == .idle || s.state == .unknown || s.state == .offline
        return Button { if !on { Haptics.tap(); select(s.key) } } label: {
            Text(s.slotLabel ?? s.displayTitle)
                .lineLimit(1)
                .font(.caption.weight(on ? .bold : .medium))
                .padding(.horizontal, 12)
                .frame(height: 30)
                // The state is the capsule's color; a quiet (seen, idle) one is the plain gray.
                .background(quiet ? AnyShapeStyle(Theme.Fill.chip) : AnyShapeStyle(tone.dot.opacity(on ? 0.26 : 0.16)), in: .capsule)
                .overlay(Capsule().strokeBorder(on ? (quiet ? Color.primary.opacity(0.45) : tone.dot.opacity(0.8)) : .clear, lineWidth: 1.5))
                .foregroundStyle(quiet ? Color.primary : tone.text)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(s.slotLabel ?? s.displayTitle)，\(s.state.label)")
        .accessibilityAddTraits(on ? .isSelected : [])
        .id(s.key)
    }

    /// ⋯: a sheet with every tab, its state and the space's page.
    private func allButton(_ siblings: [Session]) -> some View {
        let waiting = siblings.contains { $0.key != current && $0.state == .needsYou }
        return Button { Haptics.tap(); showAll = true } label: {
            Image(systemName: "ellipsis")
                .font(.footnote.weight(.bold))
                .foregroundStyle(.secondary)
                .frame(width: 34, height: 30)
                .background(Theme.Fill.chip, in: .capsule)
                .overlay(alignment: .topTrailing) {
                    if waiting { Circle().fill(Theme.needsYou).frame(width: 8, height: 8).offset(x: 1, y: -1) } // still, not pulsing
                }
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("同一空间的 \(siblings.count) 个 Agent" + (waiting ? "，有 Agent 需要你" : ""))
        .sheet(isPresented: $showAll) {
            SiblingList(siblings: siblings, current: current, space: model.session(current)) { key in
                showAll = false
                if key != current { select(key) }
            } openSpace: { space in
                showAll = false
                openSpace?(space)
            }
            .presentationDetents([.medium, .large])
            .presentationDragIndicator(.visible)
        }
    }
}

/// The ⋯ sheet of a space's tabs: the space's page on top, then each tab with its state's dot and words.
private struct SiblingList: View {
    var siblings: [Session]
    var current: SessionKey
    var space: Session?
    var pick: (SessionKey) -> Void
    var openSpace: (String) -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                Text("\(siblings.count) 个标签页").font(.headline).padding(.horizontal, 14).padding(.top, 18).padding(.bottom, 8)
                if let s = space, let name = s.spaceName, let k = s.spaceKey {
                    Button { openSpace(k) } label: {
                        HStack(spacing: 10) {
                            Image(systemName: "square.stack.3d.up").foregroundStyle(Theme.accent).frame(width: 22)
                            Text("空间 \(name) 的所有 Agent").font(.subheadline.weight(.semibold)).lineLimit(1)
                            Spacer(minLength: 8)
                            Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
                        }
                        .padding(.horizontal, 14)
                        .frame(minHeight: 46)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    Divider().padding(.leading, 14)
                }
                ForEach(siblings) { s in
                    let on = s.key == current
                    Button { pick(s.key) } label: {
                        HStack(spacing: 10) {
                            Circle().fill(s.state.color).frame(width: 9, height: 9).frame(width: 22) // a still dot
                            VStack(alignment: .leading, spacing: 1) {
                                Text(s.slotLabel ?? s.displayTitle).font(.subheadline.weight(on ? .semibold : .regular)).lineLimit(1)
                                Text(s.state.label).font(.caption).foregroundStyle(s.state.tone.text).lineLimit(1)
                            }
                            Spacer(minLength: 8)
                            if on { Image(systemName: "checkmark").font(.footnote.weight(.semibold)).foregroundStyle(Theme.accent) }
                        }
                        .padding(.horizontal, 14)
                        .frame(minHeight: 50)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(s.slotLabel ?? s.displayTitle)，\(s.state.label)")
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
            .padding(.vertical, 6)
        }
        .scrollBounceBehavior(.basedOnSize)
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
