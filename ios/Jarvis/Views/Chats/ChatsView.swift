import SwiftUI

/// Agents that share a space (a herdr workspace, or whatever groups agents in another host) on one machine.
struct SpaceGroup: Identifiable {
    var id: String // machineID/spaceID
    var machineID: String
    var spaceID: String
    var name: String
    var index: Int
    var sessions: [Session] // ordered by slot

    var state: AgentState { sessions.map(\.state).min { $0.priority < $1.priority } ?? .idle }
    var updatedAt: Date { sessions.compactMap(\.updatedAt).max() ?? .distantPast }
    var isQuiet: Bool { sessions.allSatisfy(\.isQuiet) }

    func count(_ s: AgentState) -> Int { sessions.filter { $0.state == s }.count }

    static func build(_ sessions: [Session]) -> [SpaceGroup] {
        var groups: [String: SpaceGroup] = [:]
        for s in sessions where s.state.isLive {
            let spaceID = s.spaceID ?? "-"
            let key = "\(s.machineID)/\(spaceID)"
            if groups[key] == nil {
                groups[key] = SpaceGroup(id: key, machineID: s.machineID, spaceID: spaceID,
                                         name: s.spaceName ?? s.folderName, index: s.spaceIndex ?? 0, sessions: [])
            }
            groups[key]!.sessions.append(s)
        }
        return groups.values.map { g in
            var g = g
            g.sessions.sort { ($0.slotIndex ?? 0, $0.sessionID) < ($1.slotIndex ?? 0, $1.sessionID) }
            return g
        }
        .sorted { a, b in
            a.state.priority != b.state.priority ? a.state.priority < b.state.priority : a.updatedAt > b.updatedAt
        }
    }
}

/// 开发: every agent across machines, grouped by space so tabs that work together stay together.
struct ChatsView: View {
    @Environment(AppModel.self) private var model
    @State private var path = NavigationPath()
    @State private var query = ""
    @AppStorage("dev.machineFilter") private var machineFilter = ""
    @State private var showNew = false
    @State private var showAllRecent = false
    @State private var expandedQuiet = Set<String>()
    @State private var expandedIdle = Set<String>()

    private var filtered: [Session] {
        model.allSessions.filter { s in
            !s.assistant && (machineFilter.isEmpty || s.machineID == machineFilter) &&
            (query.isEmpty || [s.title, s.cwd, s.lastText ?? "", s.spaceName ?? "", s.slotName ?? ""]
                .contains { $0.localizedCaseInsensitiveContains(query) })
        }
    }

    var body: some View {
        let groups = SpaceGroup.build(filtered)
        // Pinned spaces lead and never fold away with the quiet ones.
        // Muted spaces drop to a folded section at the bottom (no notifications, no attention).
        let muted = groups.filter { model.isMuted($0) }
        let unmuted = groups.filter { !model.isMuted($0) }
        let pinnedSpaces = unmuted.filter { model.isPinned($0) }
        let active = pinnedSpaces + unmuted.filter { !$0.isQuiet && !model.isPinned($0) }
        let quiet = unmuted.filter { $0.isQuiet && !model.isPinned($0) }
        let pinnedAgents = filtered.filter { model.isPinned($0.key) }
            .sorted { a, b in (model.pins.sessions.firstIndex(of: AppModel.pinKey(session: a.key)) ?? 0) < (model.pins.sessions.firstIndex(of: AppModel.pinKey(session: b.key)) ?? 0) }
        let recent = filtered.filter { !$0.state.isLive }.sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
        let interactions = model.pendingInteractions.filter { model.session($0.sessionKey)?.assistant != true }

        NavigationStack(path: $path) {
            List {
                Section { ConnectionBanner() }
                    .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 16))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)

                if !interactions.isEmpty && query.isEmpty {
                    Section {
                        ForEach(interactions) { ix in
                            InteractionPreviewCard(interaction: ix) { path.append(ix.sessionKey) }
                                .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                                .listRowBackground(Color.clear)
                                .listRowSeparator(.hidden)
                        }
                    } header: {
                        SectionHeader(title: "需要你处理", count: interactions.count, tint: Theme.needsYou)
                    }
                }

                if !pinnedAgents.isEmpty {
                    Section {
                        ForEach(pinnedAgents) { s in slotRow(s) }
                    } header: {
                        SectionHeader(title: "置顶", count: nil, tint: Theme.accent)
                    }
                }

                ForEach(active) { g in
                    Section {
                        // Busy (and pinned / looping) agents first; idle ones fold into one row so a big workspace
                        // stays compact.
                        let stays: (Session) -> Bool = {
                            !model.isMuted($0.key) && (!$0.isQuiet || model.isPinned($0.key) || $0.loop != nil)
                        }
                        let idle = g.sessions.filter { !stays($0) }
                        let showIdle = expandedIdle.contains(g.id) || !query.isEmpty
                        ForEach(g.sessions.filter(stays)) { s in slotRow(s) }
                        if !idle.isEmpty {
                            if showIdle {
                                ForEach(idle) { s in slotRow(s) }
                                if query.isEmpty {
                                    Button {
                                        withAnimation(.snappy) { _ = expandedIdle.remove(g.id) }
                                    } label: {
                                        Label("收起空闲的 \(idle.count) 个", systemImage: "chevron.up")
                                            .font(.subheadline.weight(.medium))
                                            .lineLimit(1)
                                    }
                                }
                            } else {
                                Button {
                                    Haptics.tap()
                                    withAnimation(.snappy) { _ = expandedIdle.insert(g.id) }
                                } label: { IdleFoldRow(sessions: idle) }
                                    .buttonStyle(.plain)
                            }
                        }
                    } header: {
                        // A visible ⋯ for pin / mute: long press on a list section header does not reach a context menu.
                        HStack(spacing: 4) {
                            Button { path.append(SpaceRoute(id: g.id)) } label: {
                                SpaceHeader(group: g, machine: model.machine(g.machineID), showMachine: machineFilter.isEmpty,
                                            pinned: model.isPinned(g))
                            }
                            .buttonStyle(.plain)
                            Menu { spaceMenu(g) } label: {
                                Image(systemName: "ellipsis.circle")
                                    .font(.body)
                                    .foregroundStyle(.secondary)
                                    .frame(width: 32, height: 28)
                                    .contentShape(.rect)
                            }
                            .accessibilityLabel("\(g.name) 的更多操作")
                        }
                    }
                }

                if !quiet.isEmpty {
                    Section {
                        ForEach(quiet) { g in
                            DisclosureGroup(isExpanded: Binding(
                                get: { expandedQuiet.contains(g.id) || !query.isEmpty },
                                set: { if $0 { expandedQuiet.insert(g.id) } else { expandedQuiet.remove(g.id) } }
                            )) {
                                ForEach(g.sessions) { s in slotRow(s) }
                            } label: {
                                QuietSpaceRow(group: g, machine: model.machine(g.machineID), showMachine: machineFilter.isEmpty)
                            }
                        }
                    } header: {
                        SectionHeader(title: "空闲的空间", count: quiet.count, tint: .secondary)
                    }
                }

                if !muted.isEmpty {
                    Section {
                        ForEach(muted) { g in
                            DisclosureGroup(isExpanded: Binding(
                                get: { expandedQuiet.contains(g.id) || !query.isEmpty },
                                set: { if $0 { expandedQuiet.insert(g.id) } else { expandedQuiet.remove(g.id) } }
                            )) {
                                ForEach(g.sessions) { s in slotRow(s) }
                            } label: {
                                QuietSpaceRow(group: g, machine: model.machine(g.machineID), showMachine: machineFilter.isEmpty)
                                    .opacity(0.6)
                            }
                            .contextMenu { spaceMenu(g) }
                        }
                    } header: {
                        SectionHeader(title: "已屏蔽", count: muted.count, tint: .secondary)
                    }
                }

                if !recent.isEmpty {
                    Section {
                        ForEach(showAllRecent ? recent : Array(recent.prefix(10))) { s in
                            NavigationLink(value: s.key) {
                                RecentRow(session: s, machine: model.machine(s.machineID), showMachine: machineFilter.isEmpty)
                            }
                        }
                        if recent.count > 10 && !showAllRecent {
                            Button("显示全部 \(recent.count) 个") { withAnimation { showAllRecent = true } }
                                .font(.subheadline.weight(.medium))
                        }
                    } header: {
                        SectionHeader(title: "最近（未运行）", count: nil, tint: .secondary)
                    }
                }

                if filtered.isEmpty {
                    Section {
                        EmptyStateView(symbol: "chevron.left.forwardslash.chevron.right",
                                       title: machineFilter.isEmpty ? "开始第一个开发会话" : "这台机器上没有会话",
                                       message: "在任意一台电脑的 herdr 里启动 Claude，手机上实时看、随时接手。",
                                       action: ("新建会话", { showNew = true }))
                    }
                    .listRowBackground(Color.clear)
                }
            }
            .listStyle(.insetGrouped)
            .themedPage()
            .navigationTitle("开发")
            .searchable(text: $query, prompt: "搜索空间、标签、会话、内容")
            .refreshable { await model.refresh() }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { machineMenu }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showNew = true } label: { Image(systemName: "square.and.pencil") }
                        .accessibilityLabel("新建会话")
                }
            }
            .navigationDestination(for: SessionKey.self) { key in ChatView(key: key) }
            .navigationDestination(for: SpaceRoute.self) { r in SpaceView(spaceKey: r.id) }
            .sheet(isPresented: $showNew) {
                NewChatView { session in
                    showNew = false
                    path.append(session.key)
                }
            }
            .onChange(of: path) { _, p in
                if p.isEmpty { AppModel.rememberChat(nil) } // back at the list: next launch starts here
            }
            .onChange(of: model.openChat, initial: true) { _, key in
                guard let key else { return }
                path = NavigationPath([key])
                model.openChat = nil
            }
            .onAppear {
                if model.debugShowNewChat { model.debugShowNewChat = false; showNew = true }
                if !machineFilter.isEmpty, !model.machines.isEmpty, model.machine(machineFilter) == nil { machineFilter = "" }
            }
        }
    }

    private func spaceMenu(_ g: SpaceGroup) -> some View { SpaceMenuItems(group: g) }

    private func slotRow(_ s: Session) -> some View {
        NavigationLink(value: s.key) { SlotRow(session: s, muted: model.isMuted(s.key)) }
            .swipeActions(edge: .trailing) {
                if s.startedByJarvis && s.state.isLive {
                    Button(role: .destructive) { Task { await model.close(s.key) } } label: { Label("关闭", systemImage: "xmark") }
                }
            }
            .swipeActions(edge: .leading) {
                Button { model.togglePin(s.key) } label: {
                    Label(model.isPinned(s.key) ? "取消置顶" : "置顶", systemImage: model.isPinned(s.key) ? "pin.slash.fill" : "pin.fill")
                }
                .tint(Theme.accent)
                if s.state == .working || s.state == .needsYou {
                    Button { Task { await model.interrupt(s.key) } } label: { Label("中断", systemImage: "stop.fill") }
                        .tint(Theme.busy)
                }
                if s.state == .unread {
                    Button { model.markSeen(s.key) } label: { Label("标为已看", systemImage: "eye") }
                        .tint(.gray)
                }
            }
            .contextMenu {
                Button { model.togglePin(s.key) } label: {
                    Label(model.isPinned(s.key) ? "取消置顶" : "置顶", systemImage: model.isPinned(s.key) ? "pin.slash" : "pin")
                }
                Button { model.toggleMute(s.key) } label: {
                    Label(model.isMutedItself(s.key) ? "取消屏蔽通知" : "屏蔽通知", systemImage: model.isMutedItself(s.key) ? "bell" : "bell.slash")
                }
                if s.loop != nil {
                    Button(role: .destructive) { model.stopLoop(s.key) } label: { Label("停止循环", systemImage: "stop.circle") }
                }
                Button { UIPasteboard.general.string = s.cwd } label: { Label("拷贝目录", systemImage: "folder") }
                Button { UIPasteboard.general.string = "claude --resume \(s.sessionID)" } label: { Label("拷贝恢复命令", systemImage: "terminal") }
            }
    }

    /// Shows the chosen machine's name in the corner; "全部" keeps the plain filter icon.
    private var machineMenu: some View {
        Menu {
            Picker("机器", selection: $machineFilter) {
                Label("全部机器", systemImage: "square.grid.2x2").tag("")
                ForEach(model.machines) { m in
                    Label(m.name, systemImage: m.symbol).tag(m.id)
                }
            }
        } label: {
            if let m = model.machine(machineFilter) {
                HStack(spacing: 5) {
                    Image(systemName: m.symbol)
                    Text(m.name).lineLimit(1)
                    Image(systemName: "chevron.down").font(.caption2.weight(.bold))
                }
                .font(.subheadline.weight(.semibold))
                .fixedSize()
            } else {
                Image(systemName: "line.3.horizontal.decrease")
            }
        }
        .accessibilityLabel(model.machine(machineFilter).map { "当前机器：\($0.name)" } ?? "按机器筛选")
    }
}

struct SpaceRoute: Hashable { var id: String }

struct SectionHeader: View {
    var title: String
    var count: Int?
    var tint: Color

    var body: some View {
        HStack(spacing: 6) {
            Text(title)
            if let count {
                Text("\(count)")
                    .font(.caption2.weight(.bold).monospacedDigit())
                    .foregroundStyle(tint)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(tint.opacity(0.14), in: .capsule)
            }
        }
    }
}

/// Section header of a space: name, machine and a count per state.
struct SpaceHeader: View {
    var group: SpaceGroup
    var machine: Machine?
    var showMachine: Bool
    var pinned = false

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "square.stack.3d.up.fill").foregroundStyle(group.state.color)
            Text(group.name).font(.subheadline.weight(.bold)).foregroundStyle(Color.primary).textCase(nil).lineLimit(1)
            if pinned { Image(systemName: "pin.fill").font(.caption2).foregroundStyle(Theme.accent).accessibilityLabel("已置顶") }
            if showMachine, let machine {
                Text(machine.name).font(.caption).foregroundStyle(.secondary).textCase(nil).lineLimit(1)
            }
            Spacer(minLength: 4)
            StateCounts(group: group)
            Image(systemName: "chevron.right").font(.caption2.weight(.bold)).foregroundStyle(.tertiary)
        }
        .contentShape(.rect)
    }
}

struct StateCounts: View {
    var group: SpaceGroup

    var body: some View {
        HStack(spacing: 6) {
            ForEach([AgentState.needsYou, .working, .unread], id: \.self) { st in
                let n = group.count(st)
                if n > 0 {
                    HStack(spacing: 3) {
                        Circle().fill(st.color).frame(width: 6, height: 6)
                        Text("\(n)").font(.caption2.weight(.bold).monospacedDigit()).foregroundStyle(st.color)
                    }
                }
            }
            let idle = group.count(.idle) + group.count(.unknown)
            if idle > 0 {
                Text("\(idle) 空闲").font(.caption2).foregroundStyle(.secondary).textCase(nil)
            }
        }
    }
}

/// One agent inside a space: one state dot, the tab name and state on the first line, then a two-line title and
/// the latest line.
struct SlotRow: View {
    var session: Session
    /// Muted (itself or its space): gray, no state color, a bell-slash — it should not catch the eye.
    var muted = false

    var body: some View {
        let quiet = muted || session.state == .idle || session.state == .unknown
        let tint = quiet ? Color.secondary : session.state.color
        HStack(alignment: .top, spacing: 10) {
            StatusDot(state: session.state, size: 9).padding(.top, 5)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    if let slot = session.slotLabel {
                        Text(slot)
                            .font(.caption.weight(.semibold).monospaced())
                            .lineLimit(1)
                            .truncationMode(.middle)
                            .foregroundStyle(tint)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 1.5)
                            .background(tint.opacity(0.12), in: .capsule)
                            .layoutPriority(1)
                    }
                    if !quiet {
                        Text(session.state.label).font(.caption.weight(.semibold)).foregroundStyle(tint).lineLimit(1)
                    }
                    if let loop = session.loop { LoopBadge(loop: loop).opacity(muted ? 0.6 : 1) }
                    if muted {
                        Image(systemName: "bell.slash.fill").font(.caption2).foregroundStyle(.tertiary).accessibilityLabel("已屏蔽通知")
                    }
                    Spacer(minLength: 4)
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        Text(Fmt.relative(session.updatedAt, now: ctx.date)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                    .fixedSize()
                }
                Text(session.displayTitle)
                    .font(.body.weight(session.state == .unread ? .bold : .semibold))
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
                if let last = session.lastText, !last.isEmpty {
                    Text(Fmt.plain(last)).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                }
            }
        }
        .padding(.vertical, 3)
        .opacity(muted ? 0.55 : 1)
    }
}

/// The idle agents of a busy space folded into one line: count plus their tab names.
struct IdleFoldRow: View {
    var sessions: [Session]

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "chevron.down.circle")
                .foregroundStyle(.secondary)
            Text("还有 \(sessions.count) 个空闲")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .fixedSize()
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 4) {
                    ForEach(sessions.prefix(6)) { s in
                        Text(s.slotLabel ?? s.displayTitle)
                            .font(.caption.monospaced())
                            .foregroundStyle(.tertiary)
                            .lineLimit(1)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.primary.opacity(0.05), in: .capsule)
                    }
                }
            }
            .allowsHitTesting(false)
            .mask(LinearGradient(colors: [.black, .black, .clear], startPoint: .leading, endPoint: .trailing))
        }
        .padding(.vertical, 4)
        .contentShape(.rect)
        .accessibilityLabel("展开 \(sessions.count) 个空闲的 agent")
    }
}

/// A space where every agent is idle: one compact line, expandable.
struct QuietSpaceRow: View {
    var group: SpaceGroup
    var machine: Machine?
    var showMachine: Bool

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "square.stack.3d.up").foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 2) {
                Text(group.name).font(.body.weight(.medium)).lineLimit(1)
                Text([showMachine ? machine?.name : nil, "\(group.sessions.count) 个标签",
                      group.sessions.compactMap(\.slotLabel).prefix(3).joined(separator: " · ")]
                    .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer()
            Text(Fmt.relative(group.updatedAt)).font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// A session that is no longer running (resumable by sending a message).
struct RecentRow: View {
    var session: Session
    var machine: Machine?
    var showMachine: Bool

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            SessionAvatar(machine: machine, state: .offline, size: 36)
            VStack(alignment: .leading, spacing: 3) {
                HStack {
                    Text(session.displayTitle).font(.body.weight(.medium)).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(Fmt.relative(session.updatedAt)).font(.caption).foregroundStyle(.secondary)
                }
                Text([session.folderName, showMachine ? machine?.name : nil].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                if let last = session.lastText, !last.isEmpty {
                    Text(Fmt.plain(last)).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
    }
}

/// Used by the machine detail page.
struct SessionRow: View {
    var session: Session
    var machine: Machine?
    var showMachine = true

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            SessionAvatar(machine: machine, state: session.state)
            SlotRow(session: session)
        }
    }
}

/// "⟳ 每 30 分钟 · 下次 11:57" on an agent driven by /loop.
struct LoopBadge: View {
    var loop: LoopInfo

    var body: some View {
        HStack(spacing: 3) {
            Image(systemName: "arrow.triangle.2.circlepath").font(.system(size: 10, weight: .bold))
            Text(label).lineLimit(1)
        }
        .font(.caption.weight(.semibold))
        .foregroundStyle(Theme.done)
        .padding(.horizontal, 6)
        .padding(.vertical, 1.5)
        .background(Theme.done.opacity(0.12), in: .capsule)
        .accessibilityLabel("循环中，" + label)
    }

    private var label: String {
        if let next = loop.nextAt, next > .now { return "下次 " + next.formatted(date: .omitted, time: .shortened) }
        return loop.cadence ?? "循环"
    }
}

/// Pin / mute a space: the ⋯ on its header in 开发, the menu of its page, the long press on a folded row.
struct SpaceMenuItems: View {
    @Environment(AppModel.self) private var model
    var group: SpaceGroup

    var body: some View {
        Button { model.togglePin(group) } label: {
            Label(model.isPinned(group) ? "取消置顶空间" : "置顶空间", systemImage: model.isPinned(group) ? "pin.slash" : "pin")
        }
        Button { model.toggleMute(group) } label: {
            Label(model.isMuted(group) ? "取消屏蔽空间" : "屏蔽这个空间", systemImage: model.isMuted(group) ? "bell" : "bell.slash")
        }
    }
}

