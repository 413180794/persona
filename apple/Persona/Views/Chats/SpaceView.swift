import BeautifulUI
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
    var closed: (SessionKey, [SessionKey]) -> Void = { _, _ in }
    /// The strip's own scroll position (not ScrollViewReader: its scrollTo drags the page above along).
    @State private var shown: SessionKey?
    @State private var showAll = false
    @State private var closing: Session?
    @State private var stopping: SessionKey?
    @State private var closeOrder: [SessionKey] = []

    var body: some View {
        let siblings = model.siblings(of: current)
        HStack(spacing: Theme.Space.sm) {
            if !siblings.isEmpty {
                strip(siblings)
                allButton(siblings)
            }
        }
        .confirmClose($closing) { key in closed(key, closeOrder) }
        .sheet(isPresented: $showAll) {
            SiblingList(current: current, space: model.session(current)) { key in
                showAll = false
                if key != current { select(key) }
            } openSpace: { space in
                showAll = false
                openSpace?(space)
            } closed: { key, order in
                if key == current { showAll = false }
                closed(key, order)
            }
            .presentationDetents([.medium, .large])
            .presentationDragIndicator(.visible)
            #if os(macOS)
            // A Mac sheet ignores detents and opens at its content's minimum size: the list came up as its header and
            // one row (#906).
            .macSheet()
            #endif
        }
    }

    private func requestClose(_ session: Session) {
        guard stopping == nil, let latest = model.latest(session.key), latest.canTerminal else { return }
        closeOrder = model.siblings(of: current).map(\.key)
        if AgentCloseRules.needsConfirmation(latest) {
            closing = latest
        } else {
            stopping = latest.key
            Task {
                if await model.close(latest.key) { closed(latest.key, closeOrder) }
                stopping = nil
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
        return Group {
            if AppStyle.current.usesBeautifulUI {
                // Its filter chip: the state's dot (none for a quiet, seen or idle one) and the tab's name; the current
                // one is the raised capsule.
                BUIFilterChip(s.slotLabel ?? s.displayTitle, dot: quiet ? nil : tone.dot, isActive: on) {
                    if !on { Haptics.tap(); select(s.key) }
                }
            } else {
                plainTab(s, on: on, tone: tone, quiet: quiet)
            }
        }
        .accessibilityLabel("\(s.slotLabel ?? s.displayTitle)，\(s.state.label)")
        .accessibilityAddTraits(on ? .isSelected : [])
        .contextMenu {
            if s.canTerminal {
                Button(role: .destructive) { requestClose(s) } label: {
                    Label("停止并关闭 Agent", systemImage: Theme.Icon.end).lineLimit(1)
                }
                .disabled(stopping != nil)
            }
        }
        .disabled(stopping == s.key)
        .id(s.key)
    }

    private func plainTab(_ s: Session, on: Bool, tone: StatusTone, quiet: Bool) -> some View {
        Button { if !on { Haptics.tap(); select(s.key) } } label: {
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
        #if DEBUG
        // `-showSpaceTabs YES`: open this sheet (screenshots); `new` opens its new-tab form on top.
        .task { if ["YES", "new"].contains(UserDefaults.standard.string(forKey: "showSpaceTabs")) { try? await Task.sleep(for: .seconds(2)); showAll = true } }
        #endif
    }
}

/// The ⋯ sheet of a space's tabs: the space's page on top, then each tab with its state's dot and words.
private struct SiblingList: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var current: SessionKey
    var space: Session?
    var pick: (SessionKey) -> Void
    var openSpace: (String) -> Void
    var closed: (SessionKey, [SessionKey]) -> Void

    @State private var showNew = false
    @State private var closing: Session?
    @State private var stopping: SessionKey?
    @State private var closeOrder: [SessionKey] = []
    @State private var failure: String?

    private var siblings: [Session] { model.siblings(of: current) }

    private func requestClose(_ session: Session) {
        guard stopping == nil, let latest = model.latest(session.key), latest.canTerminal else { return }
        closeOrder = siblings.map(\.key)
        failure = nil
        if AgentCloseRules.needsConfirmation(latest) {
            closing = latest
        } else {
            stopping = latest.key
            Task {
                if await model.close(latest.key) {
                    closed(latest.key, closeOrder)
                } else {
                    failure = model.toast?.message ?? "停止失败，标签已保留，请重试"
                }
                stopping = nil
            }
        }
    }

    /// Where a new tab goes: the space this chat is in (its machine, directory and herdr workspace).
    private var preset: SpacePreset? {
        guard let s = space, let ws = s.spaceID else { return nil }
        return SpacePreset(machineID: s.machineRef.path, cwd: s.cwd, workspaceID: ws, spaceName: s.spaceName ?? "")
    }

    var body: some View {
        List {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: Theme.Space.s) {
                    Text("\(siblings.count) 个标签页").font(Theme.Font.heading).lineLimit(1)
                    Spacer(minLength: Theme.Space.s)
                    if preset != nil {
                        Button { Haptics.tap(); showNew = true } label: {
                            Label("新标签", systemImage: Theme.Icon.add).font(Theme.Font.secondaryStrong).lineLimit(1)
                        }
                        .themedButton()
                        .accessibilityLabel("在这个空间开新标签")
                    }
                    #if os(macOS)
                    // A Mac sheet has no swipe down or tap outside to close it; Esc presses this.
                    Button("完成") { dismiss() }.lineLimit(1).keyboardShortcut(.cancelAction)
                    #endif
                }
                .padding(.horizontal, Theme.Space.l).padding(.top, Theme.Space.l).padding(.bottom, Theme.Space.s)
                if let failure {
                    Text(failure)
                        .font(Theme.Font.secondary)
                        .foregroundStyle(Theme.danger)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.horizontal, Theme.Space.l)
                        .padding(.vertical, Theme.Space.s)
                        .accessibilityIdentifier("agent.tabs.close.error")
                }
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
            }
            .listRowInsets(EdgeInsets())
            .listRowSeparator(.hidden)
            .listRowBackground(Color.clear)
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
                            if stopping == s.key { ProgressView().tint(Theme.danger) }
                            else if on { Image(systemName: Theme.Icon.done).font(Theme.Font.captionStrong).foregroundStyle(Theme.accent) }
                        }
                        .padding(.horizontal, Theme.Space.l)
                        .scaledFrame(minHeight: Theme.Size.box50)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(s.slotLabel ?? s.displayTitle)，\(s.state.label)" + (Fmt.model(s.model).map { "，\($0)" } ?? ""))
                    .accessibilityAddTraits(on ? .isSelected : [])
                    .listRowInsets(EdgeInsets())
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
                    .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                        if s.canTerminal {
                            Button { requestClose(s) } label: {
                                Label("关闭", systemImage: Theme.Icon.end).lineLimit(1)
                            }
                            .tint(Theme.danger)
                            .accessibilityLabel("停止并关闭 \(s.slotLabel ?? s.displayTitle)")
                        }
                    }
                    .contextMenu {
                        if s.canTerminal {
                            Button(role: .destructive) { requestClose(s) } label: {
                                Label("停止并关闭 Agent", systemImage: Theme.Icon.end).lineLimit(1)
                            }
                        }
                    }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .scrollBounceBehavior(.basedOnSize)
        .disabled(stopping != nil)
        .interactiveDismissDisabled(stopping != nil)
        .confirmClose($closing) { key in closed(key, closeOrder) }
        // The new tab opens in place of this one, like picking a tab (pick closes this sheet and the form with it).
        .sheet(isPresented: $showNew) {
            if let preset {
                NewChatView(preset: preset) { s in
                    showNew = false
                    pick(s.key)
                }
            }
        }
        #if DEBUG
        // `-showSpaceTabs new`: open the new-tab form too (screenshots).
        .task { if UserDefaults.standard.string(forKey: "showSpaceTabs") == "new" { try? await Task.sleep(for: .seconds(1)); showNew = true } }
        #endif
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
                        NavigationLink(value: MachineRoute(first.machineRef)) {
                            HStack(spacing: Theme.Space.sm) {
                                Image(systemName: model.machine(first.machineRef)?.symbol ?? Theme.Icon.machine)
                                Text(model.machine(first.machineRef)?.title ?? first.machineID).lineLimit(1)
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
                        .accessibilityLabel("\(model.machine(first.machineRef)?.title ?? first.machineID) 上的所有项目")
                        Text(Fmt.path(first.cwd, home: model.machine(first.machineRef)?.home)).lineLimit(1).truncationMode(.head)
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
        .pageToolbar {
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
                NewChatView(preset: SpacePreset(machineID: first.machineRef.path, cwd: first.cwd, workspaceID: ws, spaceName: first.spaceName ?? "")) { s in
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
            if let t = session.listPreview {
                Text(t).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(3)
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
