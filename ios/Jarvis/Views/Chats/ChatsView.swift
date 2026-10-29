import SwiftUI

/// Agents that share a space (a herdr workspace, or whatever groups agents in another host) on one machine.
struct SpaceGroup: Identifiable {
    var id: String // machineID/spaceID
    var machineID: String
    var spaceID: String
    var name: String
    var index: Int
    var sessions: [Session] // in tab order

    var state: AgentState { sessions.map(\.state).min { $0.priority < $1.priority } ?? .idle }
    var updatedAt: Date { sessions.compactMap(\.updatedAt).max() ?? .distantPast }
    /// Nothing going on: every agent quiet and none waiting on a /loop or background work.
    var isQuiet: Bool { sessions.allSatisfy { $0.isQuiet && !$0.isWaiting } }

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
            // Tab order, as in herdr: an agent keeps its place whatever its state (the state shows on the row).
            g.sessions.sort { ($0.slotIndex ?? 0, $0.sessionID) < ($1.slotIndex ?? 0, $1.sessionID) }
            return g
        }
        .sorted { a, b in
            a.state.priority != b.state.priority ? a.state.priority < b.state.priority : a.updatedAt > b.updatedAt
        }
    }
}

extension Session {
    /// Nothing to look at now, but a /loop that has not fired yet or background work still running will wake it:
    /// new messages may come in minutes. 待唤醒 in the list.
    var isWaiting: Bool {
        isQuiet && (loop != nil || !(background ?? []).isEmpty || (subagents ?? []).contains(where: \.running))
    }
}

/// How 开发 groups its agents (the menu at the top left; remembered).
enum DevGrouping: String, CaseIterable, Identifiable {
    case space, state, machine
    var id: String { rawValue }
    var title: String {
        switch self {
        case .space: "按项目"
        case .state: "按状态"
        case .machine: "按机器"
        }
    }
    var symbol: String {
        switch self {
        case .space: "square.stack.3d.up"
        case .state: "circle.grid.2x1"
        case .machine: "desktopcomputer"
        }
    }
}

/// 开发: every agent across machines, grouped by space (default), by state or by machine.
struct ChatsView: View {
    @Environment(AppModel.self) private var model
    @State private var path = NavigationPath()
    /// Which space page sits at which depth of `path` (only this view pushes them), so a chat opened from a space
    /// page goes back to it instead of stacking another copy.
    @State private var spaceAt: [Int: String] = [:]
    @State private var query = ""
    @State private var searching = false

    /// An active search keeps the system navigation bar up over what is pushed (the chat showed it over its own
    /// header; closing the search once pushed is too late): opening something from the results closes the search
    /// first and pushes on the next turn of the run loop. The search's text and results stay for coming back.
    private var pushing: Binding<NavigationPath> {
        Binding(get: { path }, set: { new in
            guard searching, new.count > path.count else { return path = new }
            searching = false
            DispatchQueue.main.async { path = new }
        })
    }
    @AppStorage("dev.machineFilter") private var machineFilter = ""
    @AppStorage("dev.grouping") private var grouping: DevGrouping = .space
    @State private var showNew = false
    @State private var expandedQuiet = Set<String>()
    @State private var expandedIdle = Set<String>()
    @State private var renamingSpace: SpaceGroup?
    @State private var renamingSession: Session?
    @State private var closingSession: Session?

    private var filtered: [Session] {
        model.allSessions.filter { s in
            !s.assistant && (machineFilter.isEmpty || s.machineID == machineFilter) &&
            (query.isEmpty || [s.title, s.cwd, s.lastText ?? "", s.spaceName ?? "", s.slotName ?? ""]
                .contains { $0.localizedCaseInsensitiveContains(query) })
        }
    }

    var body: some View {
        // A pinned agent shows once, under 置顶; every grouping below is built from the rest.
        let unpinned = filtered.filter { !model.isPinned($0.key) }
        let groups = SpaceGroup.build(unpinned)
        // Pinned spaces lead and never fold away with the quiet ones.
        // Pinned spaces lead; muted spaces with something running come after the other busy ones, shown quietly;
        // quiet spaces (muted or not) fold into 空闲的空间.
        let pinnedSpaces = groups.filter { model.isPinned($0) && !model.isMuted($0) }
        let active = pinnedSpaces + groups.filter { !$0.isQuiet && !model.isPinned($0) && !model.isMuted($0) }
            + groups.filter { !$0.isQuiet && model.isMuted($0) }
        let quiet = groups.filter { $0.isQuiet && !(model.isPinned($0) && !model.isMuted($0)) }
        let pinnedAgents = filtered.filter { model.isPinned($0.key) }
            .sorted { a, b in (model.pins.sessions.firstIndex(of: AppModel.pinKey(session: a.key)) ?? 0) < (model.pins.sessions.firstIndex(of: AppModel.pinKey(session: b.key)) ?? 0) }
        let interactions = model.pendingInteractions.filter { model.session($0.sessionKey)?.assistant != true }

        NavigationStack(path: pushing) {
            List {
                if !interactions.isEmpty && query.isEmpty {
                    Section {
                        ForEach(interactions) { ix in
                            InteractionPreviewCard(interaction: ix) { path.append(ix.sessionKey) }
                                .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                                .listRowBackground(Color.clear)
                                .listRowSeparator(.hidden)
                        }
                    } header: {
                        SectionHeader(title: "需要你处理", count: interactions.count, tone: StatusPalette.needsYou)
                    }
                }

                if !pinnedAgents.isEmpty {
                    Section {
                        // No state word: the dot and the name's color already say it.
                        ForEach(pinnedAgents) { s in slotRow(s, context: true, inStateSection: true) }
                    } header: {
                        SectionHeader(title: "置顶", count: nil, tone: StatusPalette.idle)
                    }
                }

                if grouping == .state {
                    stateSections(unpinned.filter { $0.state.isLive })
                } else if grouping == .machine {
                    machineSections(unpinned.filter { $0.state.isLive })
                } else {
                ForEach(active) { g in
                    Section {
                        // Busy (and pinned / looping) agents first; idle ones fold into one row so a big workspace
                        // stays compact.
                        let stays: (Session) -> Bool = {
                            !$0.isQuiet || model.isPinned($0.key) || $0.loop != nil || !($0.background ?? []).isEmpty
                                || ($0.subagents ?? []).contains(where: \.running)
                        }
                        let sessions = ordered(g.sessions)
                        let idle = sessions.filter { !stays($0) }
                        let showIdle = expandedIdle.contains(g.id) || !query.isEmpty
                        ForEach(sessions.filter(stays)) { s in slotRow(s) }
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
                            Button { pushSpace(g.id) } label: {
                                SpaceHeader(group: g, machine: model.machine(g.machineID), showMachine: machineFilter.isEmpty,
                                            pinned: model.isPinned(g), muted: model.isMuted(g))
                            }
                            .buttonStyle(.plain)
                            Menu { spaceMenu(g) } label: {
                                Image(systemName: "ellipsis.circle")
                                    .font(.body)
                                    .foregroundStyle(.secondary)
                                    .frame(width: 44, height: 44)
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
                        SectionHeader(title: "空闲的空间", count: quiet.count, tone: StatusPalette.idle)
                    }
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
            .navigationTitle("Agents")
            // No title over the list (the tab says it); the name stays for the back button. The principal item below
            // takes the title's place: empty while all is well, 连接中… and the like while the hub is out of reach
            // (toolbar(removing: .title) would hide that too).
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $query, isPresented: $searching, prompt: "搜索空间、标签、会话、内容")
            .refreshable { await model.refresh() }
            .toolbar {
                ToolbarItem(placement: .principal) { ConnectionTitle() }
                ToolbarItem(placement: .topBarLeading) { machineMenu }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showNew = true } label: { Image(systemName: "square.and.pencil") }
                        .accessibilityLabel("新建会话")
                }
            }
            .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key) }
            .renameSpaceAlert($renamingSpace)
            .renameSessionAlert($renamingSession)
            .confirmClose($closingSession)
            .navigationDestination(for: SpaceRoute.self) { r in SpaceView(spaceKey: r.id) }
            .navigationDestination(for: MachineRoute.self) { r in MachineSpacesView(machineID: r.id) }
            .sheet(isPresented: $showNew) {
                NewChatView { session in
                    showNew = false
                    path.append(session.key)
                }
            }
            .onChange(of: path) { _, p in
                spaceAt = spaceAt.filter { $0.key < p.count }
                if p.isEmpty { AppModel.rememberChat(nil) } // back at the list: next launch starts here
            }
            .onChange(of: model.openChat, initial: true) { _, key in
                guard let key else { return }
                path = NavigationPath([key])
                model.openChat = nil
            }
            .onAppear {
                if model.debugShowNewChat { model.debugShowNewChat = false; showNew = true }
                #if DEBUG
                // `-openMachinePage <machine id>` (with `-tab dev`): that machine's projects.
                if path.isEmpty, let m = UserDefaults.standard.string(forKey: "openMachinePage") { path.append(MachineRoute(id: m)) }
                #endif
                if !machineFilter.isEmpty, !model.machines.isEmpty, model.machine(machineFilter) == nil { machineFilter = "" }
            }
        }
        .environment(\.openSpace) { space in
            if spaceAt[path.count - 2] == space { path.removeLast() } else { pushSpace(space) }
        }
    }

    private func pushSpace(_ id: String) {
        path.append(SpaceRoute(id: id))
        spaceAt[path.count - 1] = id
    }

    // MARK: By state / by machine

    /// Rows that stay out of the idle fold: busy, pinned, looping, or with background work.
    private func stays(_ s: Session) -> Bool {
        !s.isQuiet || model.isPinned(s.key) || s.loop != nil || !(s.background ?? []).isEmpty
            || (s.subagents ?? []).contains(where: \.running)
    }

    /// One order for every grouping (inside a space, a machine, and the sections of 按状态):
    /// 需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看. Muted agents that are working, finished-unread or waiting
    /// rank as 已屏蔽; one that needs you stays first.
    private func rank(_ s: Session) -> Int {
        if s.state == .needsYou { return 0 }
        let live = s.state == .working || s.state == .unread || s.isWaiting
        if live && model.isMuted(s.key) { return 4 }
        switch s.state {
        case .unread: return 1
        case .working: return 2
        default: return s.isWaiting ? 3 : 5
        }
    }

    /// By rank, then the most recently updated (working: most recently started) first.
    private func ordered(_ list: [Session]) -> [Session] {
        list.sorted { a, b in
            let ra = rank(a), rb = rank(b)
            if ra != rb { return ra < rb }
            let ta = a.sortDate, tb = b.sortDate
            return ta != tb ? ta > tb : a.sessionID < b.sessionID
        }
    }

    /// A section per rank (`rank`); 已看 folded, it is the long tail. 待唤醒: nothing new yet, but a /loop or background
    /// work will bring some.
    @ViewBuilder private func stateSections(_ live: [Session]) -> some View {
        let titles: [(key: String, title: String, tint: StatusTone)] = [
            ("needs", "需要你", StatusPalette.needsYou), ("unread", "完成未看", StatusPalette.unread),
            ("working", "工作中", StatusPalette.working), ("waiting", "待唤醒", StatusTone(dot: Theme.loop, text: Theme.loop)),
            ("muted", "已屏蔽", StatusPalette.idle), ("idle", "已看", StatusPalette.idle),
        ]
        let byRank = Dictionary(grouping: live, by: rank)
        let buckets = titles.enumerated().map { i, t in (key: t.key, title: t.title, rows: byRank[i] ?? [], tint: t.tint) }
        ForEach(buckets, id: \.key) { b in
            let rows = ordered(b.rows)
            if !rows.isEmpty {
                Section {
                    if b.key == "idle" {
                        foldedRows(rows, key: "state-idle", label: "已看", inStateSection: true)
                    } else {
                        // 已屏蔽 mixes states: each row says its own. Loops and background work stay off the rows (the chat shows them).
                        ForEach(rows) { s in slotRow(s, context: true, inStateSection: b.key != "muted") }
                    }
                } header: {
                    SectionHeader(title: b.title, count: rows.count, tone: b.tint)
                }
            }
        }
    }

    /// One section per machine, its agents by state; idle ones fold like in a space.
    @ViewBuilder private func machineSections(_ live: [Session]) -> some View {
        let machines = model.machines.filter { m in live.contains { $0.machineID == m.id } }
        ForEach(machines) { m in
            let rows = ordered(live.filter { $0.machineID == m.id })
            Section {
                ForEach(rows.filter(stays)) { s in slotRow(s, context: true) }
                foldedRows(rows.filter { !stays($0) }, key: "machine-" + m.id, label: "空闲")
            } header: {
                MachineSectionHeader(machine: m, group: SpaceGroup(id: "m-" + m.id, machineID: m.id, spaceID: "-",
                                                                   name: m.name, index: 0, sessions: rows))
            }
        }
    }

    /// Quiet rows behind one "还有 N 个…" row (always open while searching).
    @ViewBuilder private func foldedRows(_ rows: [Session], key: String, label: String, inStateSection: Bool = false) -> some View {
        if !rows.isEmpty {
            if expandedIdle.contains(key) || !query.isEmpty {
                ForEach(rows) { s in slotRow(s, context: true, inStateSection: inStateSection) }
                if query.isEmpty {
                    Button {
                        withAnimation(.snappy) { _ = expandedIdle.remove(key) }
                    } label: {
                        Label("收起\(label)的 \(rows.count) 个", systemImage: "chevron.up").font(.subheadline.weight(.medium)).lineLimit(1)
                    }
                }
            } else {
                Button {
                    Haptics.tap()
                    withAnimation(.snappy) { _ = expandedIdle.insert(key) }
                } label: { IdleFoldRow(sessions: rows) }
                    .buttonStyle(.plain)
            }
        }
    }

    private func spaceMenu(_ g: SpaceGroup) -> some View { SpaceMenuItems(group: g) { renamingSpace = g } }

    /// `context`: also name the space (and machine) — when the list is not grouped by space.
    private func slotRow(_ s: Session, context: Bool = false, inStateSection: Bool = false) -> some View {
        NavigationLink(value: s.key) {
            SlotRow(session: s, muted: model.isMuted(s.key), space: context ? (s.spaceName ?? s.folderName) : nil,
                    machine: context && grouping != .machine && machineFilter.isEmpty ? model.machine(s.machineID)?.name : nil,
                    hideState: inStateSection)
        }
            .swipeActions(edge: .trailing) {
                if s.state.isLive {
                    Button { closingSession = s } label: { Label("关闭", systemImage: "xmark") }.tint(Theme.danger)
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
                Button { renamingSession = s } label: { Label("重命名…", systemImage: "pencil") }
                Button { model.toggleMute(s.key) } label: {
                    Label(model.isMuted(s.key) ? "开启通知" : "屏蔽通知", systemImage: model.isMuted(s.key) ? "bell" : "bell.slash")
                }
                if s.loop != nil {
                    Button(role: .destructive) { model.stopLoop(s.key) } label: { Label("停止循环", systemImage: "stop.circle") }
                }
                Button { Task { await model.askJarvis(AgentHandoff.prompt(s, machine: model.machine(s.machineID)?.name)) } } label: {
                    Label("交给 Jarvis", systemImage: "sparkles")
                }
                Button { UIPasteboard.general.string = s.cwd } label: { Label("拷贝目录", systemImage: "folder") }
                Button { UIPasteboard.general.string = "claude --resume \(s.sessionID)" } label: { Label("拷贝恢复命令", systemImage: "terminal") }
                if s.state.isLive {
                    Button(role: .destructive) { closingSession = s } label: { Label("关闭 Agent…", systemImage: "xmark.circle") }
                }
            }
    }

    /// Shows the chosen machine's name in the corner; "全部" keeps the plain filter icon.
    private var machineMenu: some View {
        Menu {
            Picker("分组", selection: $grouping) {
                ForEach(DevGrouping.allCases) { g in Label(g.title, systemImage: g.symbol).tag(g) }
            }
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
                Image(systemName: grouping == .space ? "line.3.horizontal.decrease" : grouping.symbol)
            }
        }
        .accessibilityLabel((model.machine(machineFilter).map { "当前机器：\($0.name)，" } ?? "") + "分组：" + grouping.title)
    }
}

struct SpaceRoute: Hashable { var id: String }

struct SectionHeader: View {
    var title: String
    var count: Int?
    /// The count's color: a state's tone, or StatusPalette.idle for a plain count.
    var tone: StatusTone

    var body: some View {
        HStack(spacing: 6) {
            Text(title)
            if let count {
                Text("\(count)")
                    .font(.caption2.weight(.bold).monospacedDigit())
                    .foregroundStyle(tone.text)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(tone.tint, in: .capsule)
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
    /// Muted with agents running: quieter than the busy spaces, not fainter than the idle ones.
    var muted = false

    var body: some View {
        HStack(spacing: 8) {
            Text(group.name).font(.subheadline.weight(.bold)).foregroundStyle(Color.primary).textCase(nil).lineLimit(1)
            if pinned { Image(systemName: "pin.fill").font(.caption2).foregroundStyle(Theme.accent).accessibilityLabel("已置顶") }
            if muted { Image(systemName: "bell.slash.fill").font(.caption2).foregroundStyle(.secondary).accessibilityLabel("已屏蔽") }
            if showMachine, let machine {
                Text(machine.name).font(.caption).foregroundStyle(.secondary).textCase(nil).lineLimit(1)
            }
            Spacer(minLength: 4)
            StateCounts(group: group).opacity(muted ? 0.6 : 1)
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
                        Text("\(n)").font(.caption2.weight(.bold).monospacedDigit()).foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("\(n) 个\(st.label)")
                }
            }
            // Idle as a gray dot and a number like the others: "4 空闲" wrapped to two lines in a busy header.
            let idle = group.count(.idle) + group.count(.unknown)
            if idle > 0 {
                HStack(spacing: 3) {
                    Circle().fill(Color.secondary.opacity(0.45)).frame(width: 6, height: 6)
                    Text("\(idle)").font(.caption2.weight(.semibold).monospacedDigit()).foregroundStyle(.secondary)
                }
                .accessibilityLabel("\(idle) 个空闲")
            }
        }
        .lineLimit(1)
        .fixedSize()
    }
}

/// One agent: its state dot, the title and when, the latest line, then where it runs. Only 需要你 is spelled out (in
/// its red, on the last line); the dot says every other state. The tab, the agent kind and the machine are plain
/// words there: a tinted tab chip, a colored state word and an outlined kind tag on one line said the state three times.
struct SlotRow: View {
    var session: Session
    /// Muted (itself or its space): no state word, a bell-slash — it should not catch the eye.
    var muted = false
    /// Named when the list is grouped by state or machine (otherwise the section is the space).
    var space: String? = nil
    var machine: String? = nil
    /// The section already says the state (grouped by state): no state word on the row.
    var hideState = false

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            StatusDot(state: session.state, size: 9).padding(.top, 6)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(session.displayTitle)
                        .font(.body.weight(session.state == .unread ? .bold : .semibold))
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                    Spacer(minLength: 4)
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        Text(Fmt.relative(session.updatedAt, now: ctx.date)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                    .fixedSize()
                }
                if let last = session.lastText.flatMap({ $0.isEmpty ? nil : Fmt.plain($0) }) {
                    Text(last).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                }
                HStack(spacing: 4) {
                    meta.lineLimit(1)
                    if muted {
                        Image(systemName: "bell.slash.fill").font(.caption2).accessibilityLabel("已屏蔽通知")
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.top, 1)
            }
        }
        .padding(.vertical, 3)
        .opacity(muted ? 0.8 : 1) // quieter than live agents, never fainter than the idle spaces below
    }

    /// "需要你 · Claude · app › tests · MacBook": the end gives way when the line is short.
    private var meta: Text {
        var parts: [Text] = []
        if session.state == .needsYou && !muted && !hideState {
            parts.append(Text(session.state.label).fontWeight(.semibold).foregroundStyle(session.state.tone.text))
        }
        parts.append(Text(session.agentLabel))
        let slot = session.slotLabel
        if let space, !space.isEmpty {
            parts.append(Text(slot.map { space + " › " + $0 } ?? space))
        } else if let slot {
            parts.append(Text(slot))
        }
        if let machine { parts.append(Text(machine)) }
        return parts.dropFirst().reduce(parts[0]) { $0 + Text(" · ") + $1 }
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
                            .font(.caption)
                            .foregroundStyle(.secondary)
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


/// Pin / mute a space: the ⋯ on its header in 开发, the menu of its page, the long press on a folded row.
struct SpaceMenuItems: View {
    @Environment(AppModel.self) private var model
    var group: SpaceGroup
    var rename: (() -> Void)? = nil

    var body: some View {
        if let rename, group.spaceID != "-" {
            Button(action: rename) { Label("重命名空间", systemImage: "pencil") }
        }
        Button { model.togglePin(group) } label: {
            Label(model.isPinned(group) ? "取消置顶空间" : "置顶空间", systemImage: model.isPinned(group) ? "pin.slash" : "pin")
        }
        Button { model.toggleMute(group) } label: {
            Label(model.isMuted(group) ? "取消屏蔽空间" : "屏蔽这个空间", systemImage: model.isMuted(group) ? "bell" : "bell.slash")
        }
    }
}

extension View {
    /// The rename prompt for a space (herdr workspace): one name.
    func renameSpaceAlert(_ group: Binding<SpaceGroup?>) -> some View { modifier(RenameSpaceAlert(group: group)) }
    /// The rename prompt for an agent: its herdr tab and its Claude session name.
    func renameSessionAlert(_ session: Binding<Session?>) -> some View { modifier(RenameSessionAlert(session: session)) }
}

private struct RenameSpaceAlert: ViewModifier {
    @Environment(AppModel.self) private var model
    @Binding var group: SpaceGroup?
    @State private var name = ""

    func body(content: Content) -> some View {
        content
            .alert("重命名空间", isPresented: Binding(get: { group != nil }, set: { if !$0 { group = nil } })) {
                TextField("空间名", text: $name)
                Button("取消", role: .cancel) {}
                Button("保存") {
                    if let g = group { Task { await model.renameSpace(g, to: name) } }
                }
            } message: {
                Text("同时改 herdr 里这个 workspace 的名字。")
            }
            .onChange(of: group?.id) { _, _ in name = group?.name ?? "" }
    }
}

private struct RenameSessionAlert: ViewModifier {
    @Environment(AppModel.self) private var model
    @Binding var session: Session?
    @State private var tab = ""
    @State private var title = ""

    func body(content: Content) -> some View {
        content
            .alert("重命名", isPresented: Binding(get: { session != nil }, set: { if !$0 { session = nil } })) {
                if session?.paneID != nil { TextField("标签名（herdr tab）", text: $tab) }
                TextField("会话名", text: $title)
                Button("取消", role: .cancel) {}
                Button("保存") {
                    guard let s = session else { return }
                    let newTab = s.paneID != nil && tab != (s.slotName ?? "") ? tab : nil
                    let newTitle = title != s.title ? title : nil
                    guard newTab != nil || newTitle != nil else { return }
                    Task { await model.renameSession(s.key, tab: newTab, title: newTitle) }
                }
            } message: {
                Text("标签名同步到 herdr；会话名用 Claude 的 /rename，Mac 上 claude --resume 也看得到。")
            }
            .onChange(of: session?.id) { _, _ in
                tab = session?.slotName ?? ""
                title = session?.title ?? ""
            }
    }
}


/// A machine's section header in 开发 grouped by machine: its name, whether it is online, state counts.
struct MachineSectionHeader: View {
    var machine: Machine
    var group: SpaceGroup

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: machine.symbol).foregroundStyle(machine.online ? Theme.accent : .secondary)
            Text(machine.name).font(.subheadline.weight(.bold)).foregroundStyle(Color.primary).textCase(nil).lineLimit(1)
            if !machine.online {
                Text("离线").font(.caption2.weight(.semibold)).foregroundStyle(.secondary).textCase(nil)
            }
            Spacer(minLength: 4)
            StateCounts(group: group)
        }
    }
}


/// Which coding agent a row is (Claude / Codex): the same quiet outline for every one.
struct AgentTag: View {
    var label: String

    var body: some View {
        Text(label)
            .font(.caption2.weight(.bold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .overlay(Capsule().stroke(Color.secondary.opacity(0.4), lineWidth: 0.8))
            .lineLimit(1)
            .fixedSize()
    }
}
