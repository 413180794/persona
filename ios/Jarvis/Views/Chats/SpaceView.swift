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
            HStack(spacing: Theme.Space.sm) {
                strip(siblings)
                allButton(siblings)
            }
        }
    }

    private func strip(_ siblings: [Session]) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Theme.Space.sm) {
                ForEach(siblings) { s in tab(s) }
            }
            .scrollTargetLayout()
            .padding(.horizontal, Theme.Space.xxs)
        }
        .scrollTargetBehavior(.viewAligned)
        // A space with many tabs opens on the current one and its neighbours, not the first few.
        .scrollPosition(id: $shown, anchor: .center)
        .onAppear { shown = current }
        .onChange(of: current) { _, k in withAnimation(Theme.Motion.standard) { shown = k } }
        .frame(maxWidth: .infinity)
    }

    private func tab(_ s: Session) -> some View {
        let on = s.key == current
        let tone = s.state.tone
        let quiet = s.state == .idle || s.state == .unknown || s.state == .offline
        return Button { if !on { Haptics.tap(); select(s.key) } } label: {
            Text(s.slotLabel ?? s.displayTitle)
                .lineLimit(1)
                .font(on ? Theme.Font.labelHeavy : Theme.Font.labelMedium)
                .padding(.horizontal, Theme.Space.m)
                .scaledFrame(height: Theme.Size.wellS)
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
            Image(systemName: Theme.Icon.more)
                .font(Theme.Font.captionHeavy)
                .foregroundStyle(.secondary)
                .scaledFrame(width: Theme.Size.wellM, height: Theme.Size.wellS)
                .background(Theme.Fill.chip, in: .capsule)
                .overlay(alignment: .topTrailing) {
                    if waiting { Circle().fill(Theme.needsYou).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot).offset(x: 1, y: -1) } // still, not pulsing
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
        #if DEBUG
        // `-showSpaceTabs YES`: open this sheet (screenshots).
        .task { if UserDefaults.standard.bool(forKey: "showSpaceTabs") { try? await Task.sleep(for: .seconds(2)); showAll = true } }
        #endif
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
                Text("\(siblings.count) 个标签页").font(Theme.Font.heading).padding(.horizontal, Theme.Space.l).padding(.top, Theme.Space.l).padding(.bottom, Theme.Space.s)
                if let s = space, let name = s.spaceName, let k = s.spaceKey {
                    Button { openSpace(k) } label: {
                        HStack(spacing: Theme.Space.m) {
                            Image(systemName: Theme.Icon.space).foregroundStyle(Theme.accent).scaledFrame(width: Theme.Size.iconS)
                            Text("空间 \(name) 的所有 Agent").font(Theme.Font.secondaryStrong).lineLimit(1)
                            Spacer(minLength: 8)
                            Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                        }
                        .padding(.horizontal, Theme.Space.l)
                        .scaledFrame(minHeight: Theme.Size.avatarL)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    Divider().padding(.leading, Theme.Space.l)
                }
                ForEach(siblings) { s in
                    let on = s.key == current
                    Button { pick(s.key) } label: {
                        HStack(spacing: Theme.Space.m) {
                            Circle().fill(s.state.color).scaledFrame(width: Theme.Size.dotL, height: Theme.Size.dotL).scaledFrame(width: Theme.Size.iconS) // a still dot
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(s.slotLabel ?? s.displayTitle).font(on ? Theme.Font.secondaryStrong : Theme.Font.secondary).lineLimit(1)
                                // The state in its color, then the model the tab runs on (2026-10-31: which tab is which model).
                                (Text(s.state.label).foregroundStyle(s.state.tone.text)
                                    + Text(Fmt.model(s.model).map { " · " + $0 } ?? "").foregroundStyle(.secondary))
                                    .font(Theme.Font.label).lineLimit(1).truncationMode(.tail)
                            }
                            Spacer(minLength: 8)
                            if on { Image(systemName: Theme.Icon.done).font(Theme.Font.captionStrong).foregroundStyle(Theme.accent) }
                        }
                        .padding(.horizontal, Theme.Space.l)
                        .scaledFrame(minHeight: Theme.Size.box50)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(s.slotLabel ?? s.displayTitle)，\(s.state.label)" + (Fmt.model(s.model).map { "，\($0)" } ?? ""))
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
            .padding(.vertical, Theme.Space.sm)
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
            VStack(alignment: .leading, spacing: Theme.Space.l) {
                if let first {
                    HStack(spacing: Theme.Space.s) {
                        // The machine opens its own list of projects.
                        NavigationLink(value: MachineRoute(id: first.machineID)) {
                            HStack(spacing: Theme.Space.sm) {
                                Image(systemName: model.machine(first.machineID)?.symbol ?? Theme.Icon.machine)
                                Text(model.machine(first.machineID)?.name ?? first.machineID).lineLimit(1)
                                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelHeavy)
                            }
                            .fontWeight(.semibold)
                            .foregroundStyle(Theme.accent)
                            .padding(.horizontal, Theme.Space.m)
                            .padding(.vertical, Theme.Space.sm)
                            .background(Theme.accent.opacity(0.1), in: .capsule)
                            .contentShape(.capsule)
                        }
                        .buttonStyle(.plain)
                        .layoutPriority(1)
                        .accessibilityLabel("\(model.machine(first.machineID)?.name ?? first.machineID) 上的所有项目")
                        Text(Fmt.path(first.cwd, home: model.machine(first.machineID)?.home)).lineLimit(1).truncationMode(.head)
                    }
                    .font(Theme.Font.caption)
                    .foregroundStyle(.secondary)
                }
                LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], spacing: 12) {
                    ForEach(sessions) { s in
                        NavigationLink(value: s.key) { SlotCard(session: s, pending: model.interactions(for: s.key).count) }
                            .buttonStyle(.plain)
                    }
                    Button { showNew = true } label: {
                        VStack(spacing: Theme.Space.s) {
                            Image(systemName: Theme.Icon.add).font(Theme.Font.title)
                            Text("在这个空间开新标签").font(Theme.Font.captionMedium)
                        }
                        .foregroundStyle(Theme.accent)
                        // Same box as a SlotCard (min height + the card's 12pt padding), and it fills its grid row
                        // so it is as tall as the card next to it instead of floating centered and shorter.
                        .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.box150)
                        .padding(Theme.Space.m)
                        .frame(maxHeight: .infinity)
                        .background(Theme.accent.opacity(0.06), in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
                        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).strokeBorder(Theme.accent.opacity(0.3), style: StrokeStyle(lineWidth: 1, dash: [5])))
                    }
                    .buttonStyle(.plain)
                    .disabled(first == nil)
                }
            }
            .padding(Theme.Space.l)
        }
        .themedGroupedPage()
        .navigationTitle(first?.spaceName ?? "空间")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            if let g = SpaceGroup.build(sessions).first {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu { SpaceMenuItems(group: g) { renaming = g } } label: {
                        Label("空间设置", systemImage: model.isMuted(g) ? Theme.Icon.mute : Theme.Icon.more)
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
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            // Chips don't shrink: in a narrow column (two cards a row on a phone) a long tab name pushed the card wider
            // than its column and the whole grid off the screen. Drop the agent chip first, then let the name truncate.
            ViewThatFits(in: .horizontal) {
                header(agent: true)
                header(agent: false)
                HStack(spacing: Theme.Space.xs) {
                    Text(session.slotLabel ?? "—")
                        .font(Theme.Font.labelMedium)
                        .foregroundStyle(tone.text)
                        .lineLimit(1)
                        .padding(.horizontal, Theme.Space.s)
                        .padding(.vertical, 2.5)
                        .background(tone.tint, in: .capsule)
                    Spacer(minLength: 4)
                    StatusDot(state: session.state, size: 9)
                }
            }
            Text(session.displayTitle).font(Theme.Font.secondaryStrong).lineLimit(2)
            Text(session.state.label + (pending > 0 ? " · \(pending) 件待处理" : ""))
                .font(Theme.Font.labelStrong)
                .foregroundStyle(tone.text)
            if let t = session.lastText, !t.isEmpty {
                Text(Fmt.plain(t)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(3)
            }
            Spacer(minLength: 0)
            if let m = Fmt.model(session.model) { Text(m).font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(1) }
        }
        .scaledFrame(minWidth: 0, maxWidth: .infinity, minHeight: Theme.Size.box150, alignment: .topLeading)
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
