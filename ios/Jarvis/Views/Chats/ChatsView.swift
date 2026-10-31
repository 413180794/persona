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
        return groups.values.map { g -> (group: SpaceGroup, priority: Int, updatedAt: Date) in
            var g = g
            // Tab order, as in herdr: an agent keeps its place whatever its state (the state shows on the row).
            g.sessions.sort { ($0.slotIndex ?? 0, $0.sessionID) < ($1.slotIndex ?? 0, $1.sessionID) }
            return (group: g, priority: g.state.priority, updatedAt: g.updatedAt)
        }
        .sorted { a, b in
            if a.priority != b.priority { return a.priority < b.priority }
            return a.updatedAt != b.updatedAt ? a.updatedAt > b.updatedAt : a.group.id < b.group.id // one order on every render
        }
        .map(\.group)
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
        case .space: Theme.Icon.space
        case .state: "circle.grid.2x1"
        case .machine: Theme.Icon.machine
        }
    }
}

/// The quick filters over the Agents list (the bar under the search field): one tap, stacked on the machine menu.
enum AgentFilter: String, CaseIterable, Identifiable {
    case all, needsYou, unread, working
    var id: String { rawValue }
    var title: String {
        switch self {
        case .all: "全部"
        case .needsYou: "需要你"
        case .unread: "未读"
        case .working: "工作中"
        }
    }
    var state: AgentState? {
        switch self {
        case .all: nil
        case .needsYou: .needsYou
        case .unread: .unread
        case .working: .working
        }
    }
    func matches(_ s: Session) -> Bool { state.map { s.state == $0 } ?? true }
}

/// 开发: every agent across machines, grouped by space (default), by state or by machine.
struct ChatsView: View {
    @Environment(AppModel.self) private var model
    /// Regular width (iPhone Duo open): the list is the sidebar column of a split.
    @Environment(\.horizontalSizeClass) private var widthClass
    @State private var path = NavigationPath()
    /// Which space page sits at which depth of `path` (only this view pushes them), so a chat opened from a space
    /// page goes back to it instead of stacking another copy.
    @State private var spaceAt: [Int: String] = [:]
    @State private var query = ""
    @State private var searching = false
    /// What was searched for when a result was opened: closing the search empties the system field (our text stays,
    /// so nothing writes it back), and on coming back the list showed the results under an empty field (2026-10-31).
    @State private var searchedBeforePush: String?

    /// An active search keeps the system navigation bar up over what is pushed (the chat showed it over its own
    /// header; closing the search once pushed is too late): opening something from the results closes the search
    /// first and pushes on the next turn of the run loop. The search's text and results stay for coming back.
    private var pushing: Binding<NavigationPath> {
        Binding(get: { path }, set: { new in
            guard searching, new.count > path.count else { return path = new }
            searchedBeforePush = query
            searching = false
            DispatchQueue.main.async { path = new }
        })
    }
    @AppStorage("dev.machineFilter") private var machineFilter = ""
    @AppStorage("dev.grouping") private var grouping: DevGrouping = .space
    /// 细列表: one line per agent.
    @AppStorage("dev.compact") private var compact = false
    /// Folded sections (spaces, machines, states), one id per line.
    @AppStorage("dev.folded") private var foldedList = ""
    @State private var quick: AgentFilter = .all
    /// Under a quick filter, the agents it showed stay until it is chosen again: one opened from 未读 is seen now
    /// but keeps its place, so the next one is still right below it.
    @State private var kept = Set<SessionKey>()
    @State private var confirmReadAll = false
    @State private var showNew = false
    @State private var expandedQuiet = Set<String>()
    @State private var expandedIdle = Set<String>()
    @State private var renamingSpace: SpaceGroup?
    @State private var renamingSession: Session?
    @State private var closingSession: Session?

    /// The agents the machine menu and the search leave.
    private var base: [Session] {
        model.allSessions.filter { s in
            !s.assistant && (machineFilter.isEmpty || s.machineID == machineFilter) &&
            (query.isEmpty || [s.title, s.cwd, s.lastText ?? "", s.spaceName ?? "", s.slotName ?? ""]
                .contains { $0.localizedCaseInsensitiveContains(query) })
        }
    }

    private var folded: Set<String> { Set(foldedList.split(separator: "\n").map(String.init)) }

    private func setFolded(_ ids: Set<String>) { foldedList = ids.sorted().joined(separator: "\n") }

    private func toggleFold(_ id: String) {
        Haptics.tap()
        var f = folded
        if f.contains(id) { f.remove(id) } else { f.insert(id) }
        withAnimation(Theme.Motion.standard) { setFolded(f) }
    }

    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("ChatsView.body")
        #endif
        let base = base
        let matching = Set(base.filter { quick != .all && quick.matches($0) }.map(\.key))
        let filtered = quick == .all ? base : base.filter { matching.contains($0.key) || kept.contains($0.key) }
        // A pinned agent shows once, under 置顶; every grouping below is built from the rest.
        let unpinned = filtered.filter { !model.isPinned($0.key) }
        let groups = SpaceGroup.build(unpinned)
        // Pinned spaces lead; muted spaces with something running come after the other busy ones, shown quietly;
        // quiet spaces (muted or not) fold into 空闲的空间. Under a quick filter every space it leaves is listed, most
        // recent first: the order must not move while its agents are being seen.
        let pinnedSpaces = groups.filter { model.isPinned($0) && !model.isMuted($0) }
        let active = quick != .all
            ? pinnedSpaces + groups.filter { !(model.isPinned($0) && !model.isMuted($0)) }
                .sorted { $0.updatedAt != $1.updatedAt ? $0.updatedAt > $1.updatedAt : $0.id < $1.id }
            : pinnedSpaces + groups.filter { !$0.isQuiet && !model.isPinned($0) && !model.isMuted($0) }
                + groups.filter { !$0.isQuiet && model.isMuted($0) }
        let quiet = quick != .all ? [] : groups.filter { $0.isQuiet && !(model.isPinned($0) && !model.isMuted($0)) }
        let pinnedAgents = filtered.filter { model.isPinned($0.key) }
            .map { (session: $0, order: model.pins.sessions.firstIndex(of: AppModel.pinKey(session: $0.key)) ?? 0) }
            .sorted { $0.order < $1.order }
            .map(\.session)
        let interactions = model.pendingInteractions.filter { ix in
            guard let s = model.session(ix.sessionKey), !s.assistant else { return false }
            return quick == .all || quick == .needsYou
        }
        let unreadShown = filtered.filter { $0.state == .unread }.map(\.key)

        SplitNavigation(path: pushing, destinations: AgentsDestinations()) {
            List {
                if !interactions.isEmpty && query.isEmpty {
                    Section {
                        ForEach(interactions) { ix in
                            InteractionPreviewCard(interaction: ix) { openFromList(ix.sessionKey) }
                                .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                                .listRowBackground(Color.clear)
                                .listRowSeparator(.hidden)
                        }
                    } header: {
                        SectionHeader(title: "需要你处理", count: interactions.count, tone: StatusPalette.needsYou)
                    }
                }

                if quick == .unread && !unreadShown.isEmpty {
                    Section {
                        Button { confirmReadAll = true } label: {
                            HStack {
                                Label("全部标为已读", systemImage: Theme.Icon.success).lineLimit(1)
                                Spacer()
                                Text("\(unreadShown.count) 个完成未看").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                        .confirmationDialog("把 \(unreadShown.count) 个完成未看的会话标为已读？", isPresented: $confirmReadAll, titleVisibility: .visible) {
                            Button("全部标为已读（\(unreadShown.count)）") {
                                model.markSeen(unreadShown)
                                kept = []
                            }
                        }
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
                    ForEach(active) { g in spaceSection(g) }

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

                if base.isEmpty {
                    Section {
                        EmptyStateView(symbol: Theme.Icon.agents,
                                       title: machineFilter.isEmpty ? "开始第一个开发会话" : "这台机器上没有会话",
                                       message: "在任意一台电脑的 herdr 里启动 Claude，手机上实时看、随时接手。",
                                       action: ("新建会话", { showNew = true }))
                    }
                    .listRowBackground(Color.clear)
                } else if filtered.isEmpty, let st = quick.state {
                    Section {
                        EmptyStateView(symbol: Theme.Icon.success, title: "没有\(st.label)的会话",
                                       message: machineFilter.isEmpty ? "各台电脑上的 Agent 有新动静会出现在这里。" : "这台机器上的 Agent 有新动静会出现在这里。",
                                       action: ("看全部", { choose(.all, base) }))
                    }
                    .listRowBackground(Color.clear)
                }
            }
            .listStyle(.insetGrouped)
            .environment(\.defaultMinListRowHeight, compact ? 30 : 44)
            .themedPage()
            // In the split's sidebar column (iPhone Duo open) the bar sits above the list; on a phone in its safe area.
            .modifier(TopBar(abovePage: widthClass == .regular) {
                QuickFilterBar(selection: quick, counts: Self.counts(base)) { choose($0, base) }
            })
            .navigationTitle("Agents")
            // No title over the list (the tab says it); the name stays for the back button. The principal item below
            // takes the title's place: empty while all is well, 连接中… and the like while the hub is out of reach
            // (toolbar(removing: .title) would hide that too).
            .navigationBarTitleDisplayMode(.inline)
            // The search field always shows: when scrolling folded it away, the filter bar (safeAreaBar) stayed where
            // it sat under the open field on device (TestFlight 35), ~120 pt of blurred list between it and the bar.
            .searchable(text: $query, isPresented: $searching, placement: .navigationBarDrawer(displayMode: .always),
                        prompt: "搜索空间、标签、会话、内容")
            .refreshable { await model.refresh() }
            .onChange(of: path.isEmpty) { _, atList in
                // Back at the list: the field shows what the results are for again (set it anew, the field is empty).
                guard atList, let q = searchedBeforePush else { return }
                searchedBeforePush = nil
                query = ""
                DispatchQueue.main.async { query = q }
            }
            .toolbar {
                ToolbarItem(placement: .principal) { ConnectionTitle() }
                ToolbarItem(placement: .topBarLeading) { listMenu(sections: grouping == .space ? active.map(\.id) : sectionIDs(unpinned)) }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showNew = true } label: { Label("新建会话", systemImage: Theme.Icon.compose) }
                }
            }
            .renameSpaceAlert($renamingSpace)
            .renameSessionAlert($renamingSession)
            .confirmClose($closingSession)
            .sheet(isPresented: $showNew) {
                NewChatView { session in
                    showNew = false
                    openFromList(session.key)
                }
            }
            .onChange(of: matching) { _, now in kept.formUnion(now) }
            .onChange(of: machineFilter) { _, _ in kept = [] }
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
                if path.isEmpty, let m = UserDefaults.standard.string(forKey: "openMachinePage") { openFromList(MachineRoute(id: m)) }
                // `-openMonitor all|<machine id>` (with `-tab dev`): 机器监控, or one machine's charts.
                if path.isEmpty, let m = UserDefaults.standard.string(forKey: "openMonitor") {
                    if m == "all" { openFromList(MachineDashboardRoute()) } else { openFromList(MachineMonitorRoute(id: m)) }
                }
                #endif
                if !machineFilter.isEmpty, !model.machines.isEmpty, model.machine(machineFilter) == nil { machineFilter = "" }
            }
        } empty: {
            SplitPlaceholder(symbol: Theme.Icon.agents, title: "选一个 Agent",
                             message: "左边列表里点一个 Agent，对话会在这里打开。")
        }
        .environment(\.openSpace) { space in
            if spaceAt[path.count - 2] == space { path.removeLast() } else { pushSpace(space) }
        }
    }

    /// Each quick filter's count. Out of the body's modifier chain: closures in a long chain are type-checked with all
    /// of it (Release builds on the release machine time out).
    private static func counts(_ base: [Session]) -> [AgentFilter: Int] {
        var out: [AgentFilter: Int] = [:]
        for f in AgentFilter.allCases {
            if let st = f.state { out[f] = base.count { $0.state == st } }
        }
        return out
    }

    /// A quick filter chosen (again): it starts from what matches now.
    private func choose(_ f: AgentFilter, _ base: [Session]) {
        Haptics.tap()
        quick = f
        kept = Set(base.filter { f != .all && f.matches($0) }.map(\.key))
    }

    /// A space page from a page already open (a chat's 空间 menu): on top of it.
    private func pushSpace(_ id: String) {
        path.append(SpaceRoute(id: id))
        spaceAt[path.count - 1] = id
    }

    /// Something the list opens: in the split it replaces the detail column; in one stack the list is at the root, so
    /// this is the same as pushing it.
    private func openFromList(_ value: some Hashable) {
        spaceAt = (value as? SpaceRoute).map { [0: $0.id] } ?? [:]
        path = NavigationPath([value])
    }

    // MARK: By space

    /// A space: tap its header to fold it (the counts stay on it); busy (and pinned / looping) agents first, idle ones
    /// fold into one row so a big workspace stays compact. Under a quick filter every agent it left shows.
    @ViewBuilder private func spaceSection(_ g: SpaceGroup) -> some View {
        if folded.contains(g.id) {
            // Folded: the header alone, as a plain row. As an empty section each kept a header's and a section's
            // padding (全部折叠 showed six a screen).
            Section {
                spaceHeader(g, folded: true)
                    .listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20)) // where a header sits
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
            .listSectionSpacing(.custom(0))
        } else {
            Section {
                let sessions = ordered(g.sessions)
                let idle = quick == .all ? sessions.filter { !stays($0) } : []
                let showIdle = expandedIdle.contains(g.id) || !query.isEmpty
                ForEach(quick == .all ? sessions.filter(stays) : sessions) { s in slotRow(s) }
                if !idle.isEmpty {
                    if showIdle {
                        ForEach(idle) { s in slotRow(s) }
                        if query.isEmpty {
                            Button {
                                withAnimation(Theme.Motion.standard) { _ = expandedIdle.remove(g.id) }
                            } label: {
                                Label("收起空闲的 \(idle.count) 个", systemImage: Theme.Icon.collapse)
                                    .font(Theme.Font.secondaryMedium)
                                    .lineLimit(1)
                            }
                        }
                    } else {
                        Button {
                            Haptics.tap()
                            withAnimation(Theme.Motion.standard) { _ = expandedIdle.insert(g.id) }
                        } label: { IdleFoldRow(sessions: idle) }
                            .buttonStyle(.plain)
                    }
                }
            } header: {
                spaceHeader(g, folded: false)
            }
        }
    }

    /// A visible ⋯ for open / mark read / pin / mute: long press on a list section header does not reach a context menu.
    private func spaceHeader(_ g: SpaceGroup, folded isFolded: Bool) -> some View {
        HStack(spacing: Theme.Space.xs) {
            Button { toggleFold(g.id) } label: {
                SpaceHeader(group: g, machine: model.machine(g.machineID), showMachine: machineFilter.isEmpty,
                            pinned: model.isPinned(g), muted: model.isMuted(g), folded: isFolded)
            }
            .buttonStyle(.plain)
            Menu { SpaceMenuItems(group: g, rename: { renamingSpace = g }, open: { openFromList(SpaceRoute(id: g.id)) }) } label: {
                Image(systemName: Theme.Icon.moreMenu)
                    .font(Theme.Font.body)
                    .foregroundStyle(.secondary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .accessibilityLabel("\(g.name) 的更多操作")
        }
    }

    // MARK: By state / by machine

    private var order: AgentListOrder {
        AgentListOrder(isPinned: model.isPinned, isMuted: model.isMuted, byRank: quick == .all)
    }
    private func stays(_ s: Session) -> Bool { order.stays(s) }
    private func rank(_ s: Session) -> Int { order.rank(s) }
    private func ordered(_ list: [Session]) -> [Session] { order.ordered(list) }

    private static let stateTitles: [(key: String, title: String, tint: StatusTone)] = [
        ("needs", "需要你", StatusPalette.needsYou), ("unread", "完成未看", StatusPalette.unread),
        ("working", "工作中", StatusPalette.working), ("waiting", "待唤醒", StatusTone(dot: Theme.loop, text: Theme.loop)),
        ("muted", "已屏蔽", StatusPalette.idle), ("idle", "已看", StatusPalette.idle),
    ]

    /// The fold ids of the sections 按状态 / 按机器 shows now (全部折叠).
    private func sectionIDs(_ sessions: [Session]) -> [String] { order.sectionIDs(sessions, byMachine: grouping == .machine) }

    /// A section per rank (`rank`); 已看 folded, it is the long tail. 待唤醒: nothing new yet, but a /loop or background
    /// work will bring some.
    @ViewBuilder private func stateSections(_ live: [Session]) -> some View {
        let byRank = Dictionary(grouping: live, by: rank)
        let buckets = Self.stateTitles.enumerated().map { i, t in (key: t.key, title: t.title, rows: byRank[i] ?? [], tint: t.tint) }
        ForEach(buckets, id: \.key) { b in
            let rows = ordered(b.rows)
            let id = "state/" + b.key
            if !rows.isEmpty {
                Section {
                    if folded.contains(id) {
                    } else if b.key == "idle" {
                        foldedRows(rows, key: "state-idle", label: "已看", inStateSection: true)
                    } else {
                        // 已屏蔽 mixes states: each row says its own. Loops and background work stay off the rows (the chat shows them).
                        ForEach(rows) { s in slotRow(s, context: true, inStateSection: b.key != "muted") }
                    }
                } header: {
                    FoldButton(title: b.title, folded: folded.contains(id), detail: "\(rows.count) 个") { toggleFold(id) } label: {
                        SectionHeader(title: b.title, count: rows.count, tone: b.tint)
                    }
                }
                .listSectionSpacing(folded.contains(id) ? .custom(0) : .default)
            }
        }
    }

    /// One section per machine, its agents by state; idle ones fold like in a space.
    @ViewBuilder private func machineSections(_ live: [Session]) -> some View {
        let machines = model.machines.filter { m in live.contains { $0.machineID == m.id } }
        ForEach(machines) { m in
            let rows = ordered(live.filter { $0.machineID == m.id })
            let id = "machine/" + m.id
            let group = SpaceGroup(id: "m-" + m.id, machineID: m.id, spaceID: "-", name: m.name, index: 0, sessions: rows)
            Section {
                if !folded.contains(id) {
                    if quick == .all {
                        ForEach(rows.filter(stays)) { s in slotRow(s, context: true) }
                        foldedRows(rows.filter { !stays($0) }, key: "machine-" + m.id, label: "空闲")
                    } else {
                        ForEach(rows) { s in slotRow(s, context: true) }
                    }
                }
            } header: {
                FoldButton(title: m.name, folded: folded.contains(id), detail: StateCounts.spoken(group)) { toggleFold(id) } label: {
                    MachineSectionHeader(machine: m, group: group)
                }
            }
            .listSectionSpacing(folded.contains(id) ? .custom(0) : .default)
        }
    }

    /// Quiet rows behind one "还有 N 个…" row (always open while searching).
    @ViewBuilder private func foldedRows(_ rows: [Session], key: String, label: String, inStateSection: Bool = false) -> some View {
        if !rows.isEmpty {
            if expandedIdle.contains(key) || !query.isEmpty {
                ForEach(rows) { s in slotRow(s, context: true, inStateSection: inStateSection) }
                if query.isEmpty {
                    Button {
                        withAnimation(Theme.Motion.standard) { _ = expandedIdle.remove(key) }
                    } label: {
                        Label("收起\(label)的 \(rows.count) 个", systemImage: Theme.Icon.collapse).font(Theme.Font.secondaryMedium).lineLimit(1)
                    }
                }
            } else {
                Button {
                    Haptics.tap()
                    withAnimation(Theme.Motion.standard) { _ = expandedIdle.insert(key) }
                } label: { IdleFoldRow(sessions: rows) }
                    .buttonStyle(.plain)
            }
        }
    }

    /// `context`: also name the space (and machine) — when the list is not grouped by space.
    private func slotRow(_ s: Session, context: Bool = false, inStateSection: Bool = false) -> some View {
        NavigationLink(value: s.key) {
            if compact {
                CompactSlotRow(session: s, muted: model.isMuted(s.key), space: context ? (s.spaceName ?? s.folderName) : nil)
                    .equatable()
            } else {
                SlotRow(session: s, muted: model.isMuted(s.key), space: context ? (s.spaceName ?? s.folderName) : nil,
                        machine: context && grouping != .machine && machineFilter.isEmpty ? model.machine(s.machineID)?.name : nil,
                        hideState: inStateSection)
                    .equatable()
            }
        }
            .accessibilityIdentifier(compact ? "agents.compactRow" : "agents.row")
            .listRowInsets(compact ? EdgeInsets(top: 5, leading: 16, bottom: 5, trailing: 14) : nil)
            .swipeActions(edge: .trailing) {
                if s.state.isLive {
                    Button { closingSession = s } label: { Label("关闭", systemImage: Theme.Icon.close) }.tint(Theme.danger)
                }
            }
            .swipeActions(edge: .leading) {
                Button { model.togglePin(s.key) } label: {
                    Label(model.isPinned(s.key) ? "取消置顶" : "置顶", systemImage: model.isPinned(s.key) ? "pin.slash.fill" : Theme.Icon.pinned)
                }
                .tint(Theme.accent)
                if s.state == .working || s.state == .needsYou {
                    Button { Task { await model.interrupt(s.key) } } label: { Label("中断", systemImage: Theme.Icon.interrupt) }
                        .tint(Theme.busy)
                }
                if s.state == .unread {
                    Button { model.markSeen(s.key) } label: { Label("标为已看", systemImage: Theme.Icon.view) }
                        .tint(.gray)
                }
            }
            .contextMenu {
                Button { model.togglePin(s.key) } label: {
                    Label(model.isPinned(s.key) ? "取消置顶" : "置顶", systemImage: model.isPinned(s.key) ? Theme.Icon.unpin : Theme.Icon.pin)
                }
                Button { renamingSession = s } label: { Label("重命名…", systemImage: Theme.Icon.edit) }
                Button { model.toggleMute(s.key) } label: {
                    Label(model.isMuted(s.key) ? "开启通知" : "屏蔽通知", systemImage: model.isMuted(s.key) ? Theme.Icon.unmute : Theme.Icon.mute)
                }
                if s.loop != nil {
                    Button(role: .destructive) { model.stopLoop(s.key) } label: { Label("停止循环", systemImage: Theme.Icon.stopLoop) }
                }
                Button { Task { await model.askJarvis(AgentHandoff.prompt(s, machine: model.machine(s.machineID)?.name)) } } label: {
                    Label("交给 Jarvis", systemImage: Theme.Icon.ai)
                }
                Button { UIPasteboard.general.string = s.cwd } label: { Label("拷贝目录", systemImage: Theme.Icon.folder) }
                Button { UIPasteboard.general.string = "claude --resume \(s.sessionID)" } label: { Label("拷贝恢复命令", systemImage: Theme.Icon.terminal) }
                if s.state.isLive {
                    Button(role: .destructive) { closingSession = s } label: { Label("关闭 Agent…", systemImage: Theme.Icon.end) }
                }
            }
    }

    /// The menu at the top left: grouping, 卡片 / 细列表, folding, the machine filter. Shows the chosen machine's name
    /// in the corner; "全部" keeps the plain filter icon. `sections`: the fold ids of what is listed now.
    private func listMenu(sections: [String]) -> some View {
        Menu {
            Picker("分组", selection: $grouping) {
                ForEach(DevGrouping.allCases) { g in Label(g.title, systemImage: g.symbol).tag(g) }
            }
            Picker("显示", selection: $compact) {
                Label("卡片", systemImage: "rectangle.grid.1x2").tag(false)
                Label("细列表", systemImage: "list.bullet").tag(true)
            }
            Section {
                Button {
                    withAnimation(Theme.Motion.standard) { setFolded(folded.union(sections)) }
                    expandedQuiet = []
                } label: { Label("全部折叠", systemImage: Theme.Icon.collapseAll) }
                Button {
                    withAnimation(Theme.Motion.standard) { setFolded([]) }
                } label: { Label("全部展开", systemImage: "rectangle.expand.vertical") }
            }
            Picker("机器", selection: $machineFilter) {
                Label("全部机器", systemImage: "square.grid.2x2").tag("")
                ForEach(model.machines) { m in
                    Label(m.name, systemImage: m.symbol).tag(m.id)
                }
            }
            Section {
                Button { openFromList(MachineDashboardRoute()) } label: {
                    Label("机器监控", systemImage: Theme.Icon.context)
                }
            }
        } label: {
            if let m = model.machine(machineFilter) {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: m.symbol)
                    Text(m.name).lineLimit(1)
                    Image(systemName: Theme.Icon.expand).font(Theme.Font.labelHeavy)
                }
                .font(Theme.Font.secondaryStrong)
                .fixedSize()
            } else {
                Image(systemName: grouping == .space ? "line.3.horizontal.decrease" : grouping.symbol)
            }
        }
        .accessibilityLabel((model.machine(machineFilter).map { "当前机器：\($0.name)，" } ?? "") + "分组：" + grouping.title)
    }
}

/// 全部 / 需要你 / 未读 / 工作中 under the search field, each with its count; one tap switches.
struct QuickFilterBar: View {
    var selection: AgentFilter
    var counts: [AgentFilter: Int]
    var choose: (AgentFilter) -> Void

    /// No sideways scroll view: the navigation stack can take one at the top of the page for the page's own and
    /// inset it by the whole top bar. It did behind a chat reopened at launch (the chips drawn ~170 pt under the
    /// search field, blurred list between: TestFlight 35 and 38), and in the split's sidebar column. Element frames
    /// do not show it (UIKit moves the content), the pixels do: `AgentsListTests.drawn`. Chips that do not fit
    /// tighten, then go to a second row.
    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: Theme.Space.s) { chips(padding: 12) }
            HStack(spacing: Theme.Space.sm) { chips(padding: 8) }
            LarkFlowLayout(spacing: 6, lineSpacing: 8) { chips(padding: 8) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.sm)
    }

    /// 全部, and the states that have agents right now; the chosen one stays while it empties, so it can be left.
    private var shown: [AgentFilter] {
        AgentFilter.allCases.filter { $0 == .all || $0 == selection || (counts[$0] ?? 0) > 0 }
    }

    @ViewBuilder private func chips(padding: CGFloat) -> some View {
        ForEach(shown) { f in
            let on = f == selection
            let tone = f.state?.tone
            let tint = tone?.text ?? Theme.accent
            Button { if !on { choose(f) } } label: {
                HStack(spacing: Theme.Space.sm) {
                    if let tone { Circle().fill(tone.dot).frame(width: 7, height: 7) }
                    Text(f.title)
                    if let n = counts[f] {
                        Text("\(n)").monospacedDigit().foregroundStyle(on ? tint : Color.secondary)
                            .contentTransition(.numericText(value: Double(n)))
                    }
                }
                .font(on ? Theme.Font.secondaryStrong : Theme.Font.secondaryMedium)
                .lineLimit(1)
                .fixedSize()
                .foregroundStyle(on ? tint : Color.primary)
                .padding(.horizontal, padding)
                .padding(.vertical, Theme.Space.s)
                .background(Capsule().fill(on ? tint.opacity(0.15) : Color.secondary.opacity(0.1)))
                .overlay(Capsule().strokeBorder(on ? tint.opacity(0.35) : .clear, lineWidth: 1))
                .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("agents.filter." + f.rawValue)
            .accessibilityLabel(counts[f].map { "\(f.title) \($0)" } ?? f.title)
            .accessibilityAddTraits(on ? .isSelected : [])
        }
    }
}

/// A section header that folds its section: the header itself plus a chevron, spoken as "名字，已展开 / 已折叠，…".
struct FoldButton<Content: View>: View {
    var title: String
    var folded: Bool
    var detail: String
    var action: () -> Void
    @ViewBuilder var label: Content

    var body: some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.sm) {
                label
                Spacer(minLength: 4)
                FoldChevron(folded: folded)
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([title, folded ? "已折叠" : "已展开", detail].filter { !$0.isEmpty }.joined(separator: "，"))
        .accessibilityAddTraits(.isButton)
    }
}

struct FoldChevron: View {
    var folded: Bool
    var body: some View {
        Image(systemName: Theme.Icon.expand)
            .font(Theme.Font.labelHeavy)
            .foregroundStyle(.tertiary)
            .rotationEffect(.degrees(folded ? -90 : 0))
            .animation(Theme.Motion.standard, value: folded)
    }
}

struct SpaceRoute: Hashable { var id: String }

struct SectionHeader: View {
    var title: String
    var count: Int?
    /// The count's color: a state's tone, or StatusPalette.idle for a plain count.
    var tone: StatusTone

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Text(title)
            if let count { CountBadge(count: count, look: .tone(tone)) }
        }
    }
}

/// Section header of a space: name, machine and a count per state; tapping it folds the space (the counts stay).
struct SpaceHeader: View {
    var group: SpaceGroup
    var machine: Machine?
    var showMachine: Bool
    var pinned = false
    /// Muted with agents running: quieter than the busy spaces, not fainter than the idle ones.
    var muted = false
    var folded = false

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Text(group.name).font(Theme.Font.sectionHeader).foregroundStyle(.secondary).textCase(nil).lineLimit(1)
            if pinned { Image(systemName: Theme.Icon.pinned).font(Theme.Font.label).foregroundStyle(Theme.accent).accessibilityLabel("已置顶") }
            if muted { Image(systemName: Theme.Icon.muted).font(Theme.Font.label).foregroundStyle(.secondary).accessibilityLabel("已屏蔽") }
            if showMachine, let machine {
                Text(machine.name).font(Theme.Font.label).foregroundStyle(.secondary).textCase(nil).lineLimit(1)
            }
            Spacer(minLength: 4)
            StateCounts(group: group).opacity(muted ? 0.6 : 1)
            FoldChevron(folded: folded)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([group.name, folded ? "已折叠" : "已展开", pinned ? "已置顶" : "", muted ? "已屏蔽" : "",
                             showMachine ? machine?.name ?? "" : "", StateCounts.spoken(group)]
            .filter { !$0.isEmpty }.joined(separator: "，"))
        .accessibilityAddTraits(.isButton)
    }
}

struct StateCounts: View {
    var group: SpaceGroup

    /// "3 个需要你，12 个工作中，35 个完成未看，4 个空闲" for VoiceOver (and the UI tests).
    static func spoken(_ group: SpaceGroup) -> String {
        let idle = group.count(.idle) + group.count(.unknown)
        return ([AgentState.needsYou, .working, .unread].compactMap { st in group.count(st) > 0 ? "\(group.count(st)) 个\(st.label)" : nil }
            + (idle > 0 ? ["\(idle) 个空闲"] : [])).joined(separator: "，")
    }

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            ForEach([AgentState.needsYou, .working, .unread], id: \.self) { st in
                let n = group.count(st)
                if n > 0 {
                    HStack(spacing: Theme.Space.xs) {
                        Circle().fill(st.color).frame(width: 6, height: 6)
                        Text("\(n)").font(Theme.Font.labelHeavy.monospacedDigit()).foregroundStyle(.secondary)
                            .contentTransition(.numericText(value: Double(n)))
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("\(n) 个\(st.label)")
                }
            }
            // Idle as a gray dot and a number like the others: "4 空闲" wrapped to two lines in a busy header.
            let idle = group.count(.idle) + group.count(.unknown)
            if idle > 0 {
                HStack(spacing: Theme.Space.xs) {
                    Circle().fill(Color.secondary.opacity(0.45)).frame(width: 6, height: 6)
                    Text("\(idle)").font(Theme.Font.labelStrong.monospacedDigit()).foregroundStyle(.secondary)
                        .contentTransition(.numericText(value: Double(idle)))
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
nonisolated struct SlotRow: View, Equatable {
    var session: Session
    /// Muted (itself or its space): no state word, a bell-slash — it should not catch the eye.
    var muted = false
    /// Named when the list is grouped by state or machine (otherwise the section is the space).
    var space: String? = nil
    var machine: String? = nil
    /// The section already says the state (grouped by state): no state word on the row.
    var hideState = false

    @MainActor
    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("SlotRow.body")
        #endif
        HStack(alignment: .top, spacing: Theme.Space.m) {
            StatusDot(state: session.state, size: 9).padding(.top, Theme.Space.sm)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.sm) {
                    Text(session.displayTitle)
                        .font(session.state == .unread ? Theme.Font.bodyHeavy : Theme.Font.bodyStrong)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                    Spacer(minLength: 4)
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        Text(Fmt.relative(session.updatedAt, now: ctx.date)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                    .fixedSize()
                }
                if let last = session.lastText.flatMap({ $0.isEmpty ? nil : Fmt.plain($0) }) {
                    Text(last).font(Theme.Font.secondary).lineHeight(.secondary).foregroundStyle(.secondary).lineLimit(2)
                }
                HStack(spacing: Theme.Space.xs) {
                    meta.lineLimit(1)
                    if muted {
                        Image(systemName: Theme.Icon.muted).font(Theme.Font.label).accessibilityLabel("已屏蔽通知")
                    }
                }
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
                .padding(.top, 1)
            }
        }
        .padding(.vertical, Theme.Space.xs)
        .opacity(muted ? 0.8 : 1) // quieter than live agents, never fainter than the idle spaces below
    }

    /// "需要你 · Claude · app › tests · MacBook": the end gives way when the line is short.
    @MainActor private var meta: Text {
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

/// 细列表: one line per agent — state dot, title, where (space › tab, or the tab when the section is the space), when.
nonisolated struct CompactSlotRow: View, Equatable {
    var session: Session
    var muted = false
    var space: String? = nil

    @MainActor
    var body: some View {
        HStack(spacing: Theme.Space.s) {
            StatusDot(state: session.state, size: 8)
            Text(session.displayTitle)
                .font(session.state == .unread ? Theme.Font.secondaryStrong : Theme.Font.secondary)
                .lineLimit(1)
                .layoutPriority(1)
            Spacer(minLength: 4)
            if muted { Image(systemName: Theme.Icon.muted).font(Theme.Font.label).foregroundStyle(.secondary).accessibilityLabel("已屏蔽通知") }
            if let place, !place.isEmpty {
                Text(place).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head)
            }
            TimelineView(.periodic(from: .now, by: 30)) { ctx in
                Text(Fmt.relative(session.updatedAt, now: ctx.date)).font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
            }
            .fixedSize()
        }
        .opacity(muted ? 0.8 : 1)
    }

    @MainActor private var place: String? {
        let slot = session.slotLabel
        guard let space, !space.isEmpty else { return slot }
        return slot.map { space + " › " + $0 } ?? space
    }
}

/// The idle agents of a busy space folded into one line: count plus their tab names.
struct IdleFoldRow: View {
    var sessions: [Session]

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Image(systemName: "chevron.down.circle")
                .foregroundStyle(.secondary)
            Text("还有 \(sessions.count) 个空闲")
                .font(Theme.Font.secondaryMedium)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .fixedSize()
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Theme.Space.xs) {
                    ForEach(sessions.prefix(6)) { s in Chip(text: s.slotLabel ?? s.displayTitle) }
                }
            }
            .allowsHitTesting(false)
            .mask(LinearGradient(colors: [.black, .black, .clear], startPoint: .leading, endPoint: .trailing))
        }
        .padding(.vertical, Theme.Space.xs)
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
        HStack(spacing: Theme.Space.m) {
            Image(systemName: Theme.Icon.space).foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(group.name).font(Theme.Font.heading).lineLimit(1)
                Text([showMachine ? machine?.name : nil, "\(group.sessions.count) 个标签",
                      group.sessions.compactMap(\.slotLabel).prefix(3).joined(separator: " · ")]
                    .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer()
            Text(Fmt.relative(group.updatedAt)).font(Theme.Font.label).foregroundStyle(.secondary)
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
        HStack(alignment: .top, spacing: Theme.Space.m) {
            SessionAvatar(machine: machine, state: session.state)
            SlotRow(session: session).equatable()
        }
    }
}


/// Pin / mute a space: the ⋯ on its header in 开发, the menu of its page, the long press on a folded row.
struct SpaceMenuItems: View {
    @Environment(AppModel.self) private var model
    var group: SpaceGroup
    var rename: (() -> Void)? = nil
    /// 打开空间 (the Agents list: its header folds the space instead).
    var open: (() -> Void)? = nil

    var body: some View {
        if let open {
            Button(action: open) { Label("打开空间", systemImage: "arrow.right.square") }
        }
        let unread = group.sessions.filter { $0.state == .unread }.map(\.key)
        if !unread.isEmpty {
            Button { model.markSeen(unread) } label: { Label("全部标为已读（\(unread.count)）", systemImage: Theme.Icon.success) }
        }
        if let rename, group.spaceID != "-" {
            Button(action: rename) { Label("重命名空间", systemImage: Theme.Icon.edit) }
        }
        Button { model.togglePin(group) } label: {
            Label(model.isPinned(group) ? "取消置顶空间" : "置顶空间", systemImage: model.isPinned(group) ? Theme.Icon.unpin : Theme.Icon.pin)
        }
        Button { model.toggleMute(group) } label: {
            Label(model.isMuted(group) ? "取消屏蔽空间" : "屏蔽这个空间", systemImage: model.isMuted(group) ? Theme.Icon.unmute : Theme.Icon.mute)
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
        HStack(spacing: Theme.Space.s) {
            Image(systemName: machine.symbol).foregroundStyle(machine.online ? Theme.accent : .secondary)
            Text(machine.name).font(Theme.Font.sectionHeader).foregroundStyle(.secondary).textCase(nil).lineLimit(1)
            if !machine.online {
                Text("离线").font(Theme.Font.labelStrong).foregroundStyle(.secondary).textCase(nil)
            }
            Spacer(minLength: 4)
            StateCounts(group: group)
        }
    }
}

/// The pages the Agents list opens, in either layout (`SplitNavigation`).
private struct AgentsDestinations: ViewModifier {
    func body(content: Content) -> some View {
        content
            .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key).splitDetailPage() }
            .navigationDestination(for: SpaceRoute.self) { r in SpaceView(spaceKey: r.id).splitDetailPage() }
            .navigationDestination(for: MachineRoute.self) { r in MachineSpacesView(machineID: r.id).splitDetailPage() }
            .navigationDestination(for: MachineDetailRoute.self) { r in MachineDetailView(machineID: r.id).splitDetailPage() }
            .navigationDestination(for: MachineDashboardRoute.self) { _ in MachineDashboardView().splitDetailPage() }
            .navigationDestination(for: MachineMonitorRoute.self) { r in MachineMonitorView(machineID: r.id).splitDetailPage() }
    }
}

/// A bar at the top of a list: in its safe area (the list scrolls under it), or above it in a stack.
private struct TopBar<Bar: View>: ViewModifier {
    var abovePage: Bool
    @ViewBuilder var bar: Bar

    func body(content: Content) -> some View {
        if abovePage {
            VStack(spacing: 0) {
                bar
                content
            }
            .background(Theme.pageBackground)
        } else {
            content.safeAreaBar(edge: .top) { bar }
        }
    }
}
