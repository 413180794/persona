import BeautifulUI
import OSLog
import SwiftUI

/// Agents that share a space (a herdr workspace, or whatever groups agents in another host) on one machine.
struct SpaceGroup: Identifiable {
    var id: String // machineID/spaceID, "group|machineID/spaceID" for another group of hubs
    var machineID: String
    var spaceID: String
    /// The group of hubs its machine is in (docs/multi-hub.md); "" = the default group.
    var group: String = ""
    var machineRef: MachineRef { MachineRef(group: group, id: machineID) }
    var name: String
    var index: Int
    var sessions: [Session] // in tab order
    /// The hub project key for whole-space todo links; nil outside a herdr workspace.
    var todoSpace: String? = nil

    var state: AgentState { sessions.map(\.state).min { $0.priority < $1.priority } ?? .idle }
    var updatedAt: Date { sessions.compactMap(\.updatedAt).max() ?? .distantPast }
    /// The same from the rows' times while the Agents list follows them (`AgentsFeed.time`): the list's structure
    /// does not change for time passing, so its agents' `updatedAt` may be older (docs/ios-state.md「第 3 步」).
    @MainActor func updatedAt(_ feed: AgentsFeed?) -> Date {
        sessions.compactMap { feed?.time($0.id) ?? $0.updatedAt }.max() ?? .distantPast
    }
    /// Where it sorts among spaces in the same state: its agents' `sortDate`, so a working agent's every line does not
    /// move the space (a busy hub pushes every two seconds: the spaces changed places under the finger, 2026-11-01).
    var sortDate: Date { sessions.map(\.sortDate).max() ?? .distantPast }
    /// Nothing going on: every agent quiet and none waiting on a /loop or background work.
    var isQuiet: Bool { sessions.allSatisfy { $0.isQuiet && !$0.isWaiting } }

    func count(_ s: AgentState) -> Int { sessions.filter { $0.state == s }.count }

    static func build(_ sessions: [Session]) -> [SpaceGroup] {
        var groups: [String: SpaceGroup] = [:]
        for s in sessions where s.state.isLive {
            let spaceID = s.spaceID ?? "-"
            let key = "\(s.machineRef.path)/\(spaceID)"
            if groups[key] == nil {
                let todoSpace: String?
                if let id = s.spaceID, !id.isEmpty {
                    todoSpace = s.spaceName.flatMap { $0.isEmpty ? nil : $0 } ?? id
                } else {
                    todoSpace = nil
                }
                groups[key] = SpaceGroup(id: key, machineID: s.machineID, spaceID: spaceID, group: s.group,
                                         name: s.spaceName ?? s.folderName, index: s.spaceIndex ?? 0, sessions: [], todoSpace: todoSpace)
            }
            groups[key]!.sessions.append(s)
        }
        return groups.values.map { g -> (group: SpaceGroup, priority: Int, sortDate: Date) in
            var g = g
            // Tab order, as in herdr: an agent keeps its place whatever its state (the state shows on the row).
            // The agents of one tab together, in the order of their panes (#2320).
            g.sessions.sort(by: TabPanes.inTabOrder)
            return (group: g, priority: g.state.priority, sortDate: g.sortDate)
        }
        .sorted { a, b in
            if a.priority != b.priority { return a.priority < b.priority }
            return a.sortDate != b.sortDate ? a.sortDate > b.sortDate : a.group.id < b.group.id // one order on every render
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

extension DevGrouping {
    /// The groupings offered: 「按待办」 needs the hub's todos (an old hub answers 404 and has none).
    static func offered(supportsTodos: Bool) -> [DevGrouping] { allCases.filter { supportsTodos || $0 != .todo } }

    /// The grouping in use: a stored 「按待办」 falls back to 「按项目」 on a hub without todos.
    static func resolved(_ stored: DevGrouping, supportsTodos: Bool) -> DevGrouping {
        stored == .todo && !supportsTodos ? .space : stored
    }
}

/// How 开发 groups its agents (the menu at the top left; remembered).
enum DevGrouping: String, CaseIterable, Identifiable {
    case repo, space, todo, state, machine
    /// What an app that never chose a grouping shows (#2108): machines → repositories → working trees, after Orca.
    static let standard = DevGrouping.repo
    var id: String { rawValue }
    var title: String {
        switch self {
        case .repo: "按仓库"
        case .space: "按项目"
        case .todo: "按待办"
        case .state: "按状态"
        case .machine: "按机器"
        }
    }
    var symbol: String {
        switch self {
        case .repo: Theme.Icon.folder
        case .space: Theme.Icon.space
        case .todo: Theme.Icon.todo
        case .state: Theme.Icon.byState
        case .machine: Theme.Icon.machine
        }
    }
}

/// How the Agents list orders its agents (the same menu; remembered): the state first (default), or the latest update
/// only. Under 按状态 the sections are the states, so only the rows' order inside them is the time either way.
enum AgentSort: String, CaseIterable, Identifiable {
    case state, updated
    var id: String { rawValue }
    var title: String {
        switch self {
        case .state: "状态优先"
        case .updated: "最近更新"
        }
    }
    var symbol: String {
        switch self {
        case .state: Theme.Icon.byState
        case .updated: Theme.Icon.time
        }
    }
}

/// The quick filters over the Agents list (the bar under the search field): one tap, stacked on the machine menu.
nonisolated enum AgentFilter: String, CaseIterable, Identifiable, Sendable {
    case all, needsYou, unread, working
    /// Where the agent was opened (#2526): a botmux session (飞书群) or a herdr pane (终端).
    case feishu, terminal
    var id: String { rawValue }
    var title: String {
        switch self {
        case .all: "全部"
        case .needsYou: "需要你"
        case .unread: "未读"
        case .working: "工作中"
        case .feishu: SessionSource.feishu.label
        case .terminal: SessionSource.terminal.label
        }
    }
    var state: AgentState? {
        switch self {
        case .all, .feishu, .terminal: nil
        case .needsYou: .needsYou
        case .unread: .unread
        case .working: .working
        }
    }
    var source: SessionSource? {
        switch self {
        case .feishu: .feishu
        case .terminal: .terminal
        default: nil
        }
    }
    /// A muted agent that is working or finished unread is not counted or listed under 未读 / 工作中 (it sits under
    /// 已屏蔽); one that needs you still is, the same as the list's order (`AgentListOrder.rank`).
    func matches(_ s: Session, muted: Bool = false) -> Bool {
        if let source { return s.source == source }
        guard let state else { return true }
        return s.state == state && (state == .needsYou || !muted)
    }
}

/// 开发: every agent across machines, grouped by repository (default), space, todo, state or machine. The navigation
/// and the toolbar are here; the list is `AgentsList`, the view that follows the hub. A toolbar declared by the view the
/// hub's pushes redraw is handed to UIKit again on each of them, and the ⋯ menu open over the list jumped back to its
/// top (2026-10-31): nothing here reads the sessions.
struct ChatsView: View {
    @Environment(AppModel.self) private var model
    /// The shell (docs/ios.md「新外壳」): its ☰ and 助手 | Agents switch take the navigation bar.
    @Environment(\.shell) private var shell
    @State private var path = NavigationPath()
    /// Which space page sits at which depth of `path` (only this view pushes them), so a chat opened from a space
    /// page goes back to it instead of stacking another copy.
    @State private var spaceAt: [Int: String] = [:]
    @State private var query = ""
    @State private var searching = false
    /// What was searched for when a result was opened: closing the search empties the system field (our text stays,
    /// so nothing writes it back), and on coming back the list showed the results under an empty field (2026-10-31).
    @State private var searchedBeforePush: String?
    @State private var showNew = false
    /// The ⋯ menu's actions, kept current by the list (not observed: see `AgentsListMenu`).
    @State private var menuActions = AgentsListMenu.Actions()
    @StoredValue("dev.machineFilter") private var machineFilter = ""

    /// The list has appeared once: pushes made before that are held back (`model.openChat`, #2061).
    @State private var rootShown = false
    /// Whether the stack is in the middle of a push or a pop: a chat opened from outside waits for it (#1941).
    @State private var stackStill = StackStillWatcher()

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

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ChatsView")
        #endif
        // Closing the search before a push from its results redraws the navigation with it, as when the list was
        // drawn here: drawn by the list alone, the chat came up under the system navigation bar again (SearchOpenTests).
        let _ = searching
        SplitNavigation(path: pushing, destinations: AgentsDestinations()) {
            AgentsList(path: $path, spaceAt: $spaceAt, query: $query, searching: $searching,
                       searchedBeforePush: $searchedBeforePush, showNew: $showNew, menuActions: menuActions)
                // The shell's top bar floats over the navigation bar: this page's own bar makes room for it (not the
                // navigation container: a chat pushed on it must not move, nor the list on the way back, #340).
                .background { ShellNavigationClearance(active: shell != nil && !searching) }
                .background { StackStillProbe(watcher: stackStill) }
                #if os(macOS)
                .onAppear { MacShellState.shared.agentsList = menuActions } // the shell's toolbar menu, 显示 › 全部折叠 / 全部展开
                #endif
                .onChange(of: path.isEmpty) { _, atList in
                    // Back at the list: the field shows what the results are for again (set it anew, the field is empty).
                    guard atList, let q = searchedBeforePush else { return }
                    searchedBeforePush = nil
                    query = ""
                    DispatchQueue.main.async { query = q }
                }
                // The Mac's buttons are in the list column's own title row (below); the shell is the phone's.
                #if os(iOS) || os(visionOS)
                .pageToolbar {
                    if shell != nil {
                        // The shell's own bar floats over this one (AgentsTopBar): the bar stays for the search field.
                        ToolbarItem(placement: .principal) { Color.clear.frame(width: Theme.Size.hairline) }
                            .sharedBackgroundVisibility(.hidden)
                    } else {
                        // Not on the Mac (#907): the connection state is at the foot of the window's sections column (and
                        // the page's while that is hidden), and holding the bar's middle this empty item pushed 新建会话
                        // out of the list column, an empty stretch to its left over the conversation. Without it the
                        // button sits at the list column's trailing edge, by the conversation, as Mail's compose does.
                        ToolbarItem(placement: .principal) { ConnectionTitle() }
                        // On the Mac the shell shows it, next to its sidebar button (MacShell).
                        ToolbarItem(placement: .topBarLeading) { AgentsListMenu(actions: menuActions) }
                        ToolbarItem(placement: .topBarTrailing) {
                            Button { openFromList(PersonalAgentsRoute()) } label: { Label("个人 Agent", systemImage: Theme.Icon.personalAgents) }
                        }
                        ToolbarItem(placement: .topBarTrailing) {
                            Button { showNew = true } label: { Label("新建会话", systemImage: Theme.Icon.compose) }
                        }
                    }
                }
                #else
                // In the list column's own title row, at its trailing edge (#1223): as window toolbar items they followed
                // the window's middle, not the column (in the middle of the list at 920 wide, over the chat at 1800).
                .overlay(alignment: .topTrailing) {
                    if shell == nil { // always on the Mac: its window is not the phone's shell
                        HStack(spacing: Theme.Space.s) {
                            MacTitleRowButton("个人 Agent", symbol: Theme.Icon.personalAgents) { openFromList(PersonalAgentsRoute()) }
                            MacTitleRowButton("新建会话", symbol: Theme.Icon.compose) { showNew = true }
                        }
                        .padding(.trailing, Theme.Space.l)
                        .frame(height: Theme.Size.macToolbarHeight)
                        .ignoresSafeArea(.container, edges: .top)
                    }
                }
                #endif
                .sheet(isPresented: $showNew) {
                    NewChatView { session in
                        showNew = false
                        openFromList(session.key)
                    }
                }
                // Searching is not the root either: the drawer would slide the page with the keyboard up and the field open.
                .onChange(of: path.isEmpty && !searching, initial: true) { _, root in shell?.workAtRoot = root }
                .onChange(of: path) { _, p in
                    spaceAt = spaceAt.filter { $0.key < p.count }
                    if p.isEmpty { AppModel.rememberChat(nil); model.chatSwitched = [:] } // back at the list: next launch starts here
                }
                .onChange(of: model.openChat, initial: true) { _, key in
                    // Not before the list is on screen: a push into a stack whose root has not appeared yet built the
                    // chat three times (the old layout's launch onto a chat, `-openSession` / a notification: items
                    // read three times, `-openUploadDemo` uploading twice, #2061). The list's appearing opens it.
                    if rootShown { takeOpenChat() }
                }
                .onAppear {
                    if !rootShown {
                        rootShown = true
                        // Asked for before the list was there (above): a turn later, as the remembered chat below.
                        if model.openChat != nil { DispatchQueue.main.async { takeOpenChat() } }
                    }
                    // A new tree (RootView swaps the shell and the tabs when the width class changes, which iOS does
                    // for its snapshots when the app goes to the background: iPhone Duo open, 2026-11-04) starts at the
                    // list: open the chat that was open, as a launch does (AppModel.init). Back at the list it is
                    // forgotten (rememberChat(nil) above). A turn later: the split of the tree going away empties
                    // the paths as it goes, and a push made in the same update did not show.
                    // Still the same chat a turn later: going back runs onAppear before onChange(path) forgets it (#1868).
                    if path.isEmpty, model.openChat == nil, model.selectedTab == .chats, let v = AppModel.rememberedChat {
                        DispatchQueue.main.async {
                            if path.isEmpty, model.openChat == nil, AppModel.rememberedChat == v { model.openChat = SessionKey(path: v) }
                        }
                    }
                    if model.debugShowNewChat { model.debugShowNewChat = false; showNew = true }
                    #if DEBUG
                    // `-openMachinePage <machine id>` (with `-tab dev`): that machine's projects.
                    if path.isEmpty, let m = UserDefaults.standard.string(forKey: "openMachinePage") { openFromList(MachineRoute(id: m)) }
                    // `-openMonitor all|<machine id>` (with `-tab dev`): 机器, or one machine's page.
                    if path.isEmpty, let m = UserDefaults.standard.string(forKey: "openMonitor") {
                        if m == "all" { openFromList(MachineListRoute()) } else { openFromList(MachineDetailRoute(id: m)) }
                    }
                    #endif
                    if !machineFilter.isEmpty, !model.machines.isEmpty, model.machine(MachineRef(path: machineFilter)) == nil { machineFilter = "" }
                }
        } empty: {
            SplitPlaceholder(symbol: Theme.Icon.agents, title: "选一个 Agent",
                             message: "左边列表里点一个 Agent，对话会在这里打开。")
        }
        // While searching the field rises into the bar the shell's floats over: the bar makes way (the field has its
        // own 取消), as a system search takes the navigation bar, and comes back when the search closes (TestFlight 70:
        // the field was drawn under ☰, the switch and 新建会话). A result opened closes the search before the push
        // (`searchedBeforePush` set): the bar stays away rather than flash back for that moment.
        .overlay(alignment: .top) {
            ZStack {
                if let shell, path.isEmpty, !searching, searchedBeforePush == nil {
                    AgentsTopBar(shell: shell, menuActions: menuActions) { showNew = true }
                        .equatable()
                        .transition(.opacity)
                }
            }
            .animation(Theme.Motion.state, value: searching) // the bar only: not the list under it
        }
        .animation(Theme.Motion.state, value: path.isEmpty)
        .environment(\.openSpace) { space in
            if spaceAt[path.count - 2] == space { path.removeLast() } else { pushSpace(space) }
        }
    }

    /// The chat asked for from outside the list (a notification, a launch onto it): open, and the request taken.
    /// Not while the stack is still moving (a back swipe under the finger or let go, a pop or push on its way): set then,
    /// the path and the stack fell apart, the stack left with a page of nothing over the list under the shell's bar,
    /// which shows at the root only (#1941, AgentsPushPopRaceTests). Once it is still, at once.
    private func takeOpenChat() {
        guard model.openChat != nil else { return }
        stackStill.whenStill { pushOpenChat() }
    }

    private func pushOpenChat() {
        guard let key = model.openChat else { return }
        path = NavigationPath([key])
        #if DEBUG
        // `-openOverSpace YES` with `-openSession`: the chat on its space's page, a page under it (its ‹, #1337).
        if UserDefaults.standard.bool(forKey: "openOverSpace") {
            Task {
                try? await Task.sleep(for: .seconds(3)) // the session list may not be in yet
                guard let space = model.session(key)?.spaceKey else { return }
                var under = NavigationPath()
                under.append(SpaceRoute(id: space))
                under.append(key)
                path = under
                spaceAt = [0: space]
            }
        }
        #endif
        model.openChat = nil
    }

    /// A space page from a page already open (a chat's 空间 menu): on top of it.
    private func pushSpace(_ id: String) {
        path.append(SpaceRoute(id: id))
        spaceAt[path.count - 1] = id
    }

    /// Something opened from here (a new chat, a debug page): it replaces the detail column, or pushes onto the list.
    private func openFromList(_ value: some Hashable) {
        spaceAt = (value as? SpaceRoute).map { [0: $0.id] } ?? [:]
        path = NavigationPath([value])
    }
}

/// The Agents list itself: what follows the hub's pushes (`ChatsView` holds the navigation and the toolbar).
private struct AgentsList: View {
    #if os(macOS)
    static let emptyMessage = "在任意一台电脑的 herdr 里启动 Claude，在这里实时看、随时接手。"
    #else
    static let emptyMessage = "在任意一台电脑的 herdr 里启动 Claude，手机上实时看、随时接手。"
    #endif
    @Environment(AppModel.self) private var model
    @Environment(\.shell) private var shell
    /// Regular width (iPhone Duo open): the list is the sidebar column of a split.
    @Environment(\.horizontalSizeClass) private var widthClass
    @Binding var path: NavigationPath
    @Binding var spaceAt: [Int: String]
    @Binding var query: String
    @Binding var searching: Bool
    @Binding var searchedBeforePush: String?
    @Binding var showNew: Bool
    var menuActions: AgentsListMenu.Actions

    @State private var searchClose = SearchCloseWatcher()
    @StoredValue("dev.machineFilter") private var machineFilter = ""
    /// Several groups of hubs: the one the list shows ("" = all; `HubFilter`).
    @StoredValue(HubFilter.key) private var hubFilter = ""
    @StoredValue("dev.grouping") private var storedGrouping: DevGrouping = .standard // by repository first (#2108)
    /// The grouping in use (`DevGrouping.resolved`).
    private var grouping: DevGrouping { DevGrouping.resolved(storedGrouping, supportsTodos: TodoStore.shared.isSupported) }
    @StoredValue("dev.sort") private var sort: AgentSort = .state
    /// 细列表: one line per agent.
    @StoredValue("dev.compact") private var compact = false
    /// Folded sections (spaces, machines, states), one id per line.
    @StoredValue("dev.folded") private var foldedList = ""
    @State private var quick: AgentFilter = .all
    /// Under a quick filter, the agents it showed stay until it is chosen again: one opened from 未读 is seen now
    /// but keeps its place, so the next one is still right below it.
    @State private var kept = Set<SessionKey>()
    @State private var confirmReadAll = false
    @State private var expandedQuiet = Set<String>()
    @State private var expandedIdle = Set<String>()
    #if os(macOS)
    /// The rows in order for ↑ / ↓ (`AgentListRows`), kept current without drawing the list again.
    @State private var macRows = MacRowOrder()
    #endif
    @State private var renamingSpace: SpaceGroup?
    @State private var renamingSession: Session?
    @State private var closingSession: Session?
    @State private var deletingSession: Session?
    @State private var linkingSession: Session?
    @State private var linkingSpace: SpaceGroup?
    /// The long-press menu's actions on a row (not observed: see `AgentRowMenu`).
    @State private var rowActions = AgentRowMenu.Actions()
    /// The agents, the questions and the machines the list shows (`AgentsFeed`): held still while it is scrolled or
    /// out of sight.
    @State private var feed = AgentsFeed()

    /// The machines and agents the rows name, as the body last saw them (`Lookup`).
    @State private var lookup = Lookup()
    /// Machines whose herdr does not run: a row with 启动 each (#1916).
    @State private var herdr = HerdrWatch()

    /// A page pushed over it (beside it in the split it is in sight), the other page of the shell or another tab (the
    /// shell's slide between the pages shows it again).
    private var covered: Bool {
        if widthClass != .regular && !path.isEmpty { return true }
        #if os(iOS) || os(visionOS)
        if model.selectedTab != .chats && shell?.pager.moving != true { return true }
        #endif
        return false
    }

    private var sessions: [Session] { feed.following ? feed.sessions : model.allSessions }
    private var machines: [Machine] { feed.following ? feed.machines : model.machines }
    /// What the rows read (a machine's name, an agent's label), filled by each run of the body and not observed. SwiftUI
    /// tracks what each row of a `List` read when it was last drawn: a row that asked the model for its machine went on
    /// following the machines while the list was held, was drawn again by the next push, and the `List` measured its
    /// rows anew mid-glide (the content jumped by the difference from its estimate).
    private final class Lookup {
        var machines: [MachineRef: Machine] = [:]
        var sessions: [SessionKey: Session] = [:]

        func update(machines: [Machine], sessions: [Session]) {
            self.machines = Dictionary(machines.map { ($0.ref, $0) }, uniquingKeysWith: { a, _ in a })
            self.sessions = Dictionary(sessions.map { ($0.key, $0) }, uniquingKeysWith: { a, _ in a })
        }
    }

    private func machine(_ ref: MachineRef) -> Machine? { lookup.machines[ref] }

    private var showsHerdrNotices: Bool { query.isEmpty && model.connection.shown == .online && !herdrStopped.isEmpty }

    /// Beside the split (the Mac): the 「herdr 没在运行」 rows under the filter bar, not as the list's first section, which
    /// a grouped list pads above for a header (an empty band between the bar and the row; `docs/lessons/macos.md`).
    private var herdrNoticesAbove: some View {
        VStack(spacing: 0) {
            ForEach(herdrStopped) { m in
                HerdrNoticeRow(machine: m, watch: herdr)
                    .padding(.horizontal, Theme.Space.page)
                    .padding(.vertical, Theme.Space.s)
                Divider().padding(.leading, Theme.Space.page)
            }
        }
    }

    /// The machines with a 「herdr 没在运行」 row, under the machine menu and the group of hubs shown.
    private var herdrStopped: [Machine] {
        machines.filter { m in
            herdr.stopped.contains(m.ref) && HerdrWatch.watched(m) && (machineFilter.isEmpty || m.ref.path == machineFilter) &&
                (!model.isMultiHub || HubFilter.matches(hubFilter, m.group))
        }
    }
    private func session(_ key: SessionKey) -> Session? { lookup.sessions[key] }

    /// The agents the machine menu and the search leave.
    private var base: [Session] {
        sessions.filter { s in
            !s.assistant && (machineFilter.isEmpty || s.machineRef.path == machineFilter) &&
            (!model.isMultiHub || HubFilter.matches(hubFilter, s.group)) &&
            (query.isEmpty || [s.title, s.cwd, s.lastText ?? "", s.spaceName ?? "", s.slotName ?? ""]
                .contains { $0.localizedCaseInsensitiveContains(query) })
        }
    }

    private var folded: Set<String> { Set(foldedList.split(separator: "\n").map(String.init)) }

    /// Folded as shown: a search opens every folded section, so no match hides behind a fold (folded spaces showed
    /// only their header, and the agent searched for was not there to open; quiet spaces and idle rows already open).
    /// Sections that start folded: 按状态's 已看 is the long tail. For them the saved list holds "!<id>" once opened.
    private static let foldedByDefault: Set<String> = ["state/idle"]
    /// Sections whose default depends on what they hold (未归属, `TodoAgentGroup.folded`): the saved list holds "<id>"
    /// or "!<id>" once the user chose, nothing until then (#1836).
    private static let foldedByChoice: Set<String> = ["todo:none"]

    private func isFolded(_ id: String, otherwise fallback: Bool = false) -> Bool {
        guard query.isEmpty else { return false }
        if Self.foldedByChoice.contains(id) { return folded.contains(id) || (fallback && !folded.contains("!" + id)) }
        return Self.foldedByDefault.contains(id) ? !folded.contains("!" + id) : folded.contains(id)
    }

    private func setFolded(_ ids: Set<String>) { foldedList = ids.sorted().joined(separator: "\n") }

    /// `ids` folded (true) or open, on top of what is saved.
    private func folding(_ ids: some Sequence<String>, _ fold: Bool, in f: Set<String>) -> Set<String> {
        var f = f
        for id in ids {
            if Self.foldedByChoice.contains(id) {
                f.remove(id)
                f.remove("!" + id)
                f.insert(fold ? id : "!" + id)
                continue
            }
            let key = Self.foldedByDefault.contains(id) ? "!" + id : id
            let marks = Self.foldedByDefault.contains(id) ? !fold : fold // a default-folded section marks being open
            if marks { f.insert(key) } else { f.remove(key) }
        }
        return f
    }

    /// `shown`: whether it is folded on screen when that is not `isFolded(id)` alone (未归属's own default), so a
    /// tap always flips what is seen.
    private func toggleFold(_ id: String, shown: Bool? = nil) {
        Haptics.tap()
        feed.reader.keep(id)
        let f = folding([id], !(shown ?? isFolded(id)), in: folded)
        withAnimation(Theme.Motion.standard) { setFolded(f) }
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AgentsList")
        #endif
        let _ = lookup.update(machines: machines, sessions: sessions)
        let base = base
        let matching = Set(base.filter { quick != .all && quick.matches($0, muted: model.isMuted($0)) }.map(\.key))
        let filtered = quick == .all ? base : base.filter { matching.contains($0.key) || kept.contains($0.key) }
        // A pinned agent shows once, under 置顶; every grouping below is built from the rest.
        let unpinned = filtered.filter { !model.isPinned($0.key) }
        let groups = SpaceGroup.build(unpinned)
        // Pinned spaces lead; muted spaces with something running come after the other busy ones, shown quietly;
        // quiet spaces (muted or not) fold into 空闲的空间. Under a quick filter every space it leaves is listed, most
        // recent first: the order must not move while its agents are being seen.
        let pinnedSpaces = groups.filter { model.isPinned($0) && !model.isMuted($0) }
        let order = order
        // 最近更新: the busy spaces by their latest agent, not by state (muted ones still after the rest).
        let spaces = sort == .updated ? latestFirst(groups) : groups
        let busy: [SpaceGroup] = spaces.filter { !order.isQuiet($0) }
        let active = quick != .all
            ? pinnedSpaces + latestFirst(groups.filter { !(model.isPinned($0) && !model.isMuted($0)) })
            : pinnedSpaces + busy.filter { !model.isPinned($0) && !model.isMuted($0) } + busy.filter { model.isMuted($0) }
        let quiet = quick != .all ? [] : groups.filter { order.isQuiet($0) && !(model.isPinned($0) && !model.isMuted($0)) }
        let pinnedAgents = filtered.filter { model.isPinned($0.key) }
            .map { (session: $0, order: model.pinOrder($0.key)) }
            .sorted { $0.order < $1.order }
            .map(\.session)
        let interactions = (feed.following ? feed.interactions : model.pendingInteractions).filter { ix in
            guard let s = session(ix.sessionKey), !s.assistant, machineFilter.isEmpty || s.machineRef.path == machineFilter,
                  !model.isMultiHub || HubFilter.matches(hubFilter, s.group) else { return false }
            return quick == .all || quick == .needsYou
        }
        let unreadShown = filtered.filter { $0.state == .unread }.map(\.key)
        // Agents on more than one machine: 按仓库 and 按项目 split the list by machine first (#2108), and the headers under
        // a machine do not name it. Counted before the quick filter: choosing one does not add or take away headings.
        let byHost = machineFilter.isEmpty && Set(base.lazy.filter { $0.state.isLive }.map(\.machineRef)).count > 1
        let built = grouping == .repo ? RepoTree.build(unpinned.filter { $0.state.isLive }, machines: machines.map(\.ref)) : []
        // 最近更新: each machine's repositories by their latest agent (the machines keep their order, as 按项目's do).
        let tree = sort == .updated ? built.map { host in
            var h = host
            h.repos = AgentListOrder.latestFirst(host.repos, date: { $0.sessions.map(\.sortDate).max() ?? .distantPast }, id: \.id)
            return h
        } : built

        // What the ⋯ menu's actions act on, kept current without making the open menu a function of the list. Split by
        // machine, 全部折叠 still folds the spaces and repositories, not the machines: every one of them a line, the overview.
        let sectionsForMenu: [String] = {
            if grouping == .repo { return tree.flatMap(\.repos).filter { !isQuiet($0) }.map(\.id) }
            if grouping == .space { return active.map(\.id) }
            if grouping == .todo {
                return TodosLogic.buildTodoAgentGroups(sessions: unpinned, todos: TodoStore.shared.todos, isMuted: model.isMuted).map(\.key)
            }
            return sectionIDs(unpinned)
        }()
        let _ = menuActions.update(sections: sectionsForMenu,
                                   foldAll: { setFolded(folding($0, true, in: folded)); expandedQuiet = [] },
                                   unfoldAll: { setFolded(Set(Self.foldedByDefault.union(Self.foldedByChoice).map { "!" + $0 })) },
                                   onMachines: { openFromList(MachineListRoute()) })
        #if os(macOS)
        let _ = macRows.update(rowsInOrder(unpinned: unpinned, pinned: pinnedAgents, active: active, quiet: quiet, tree: tree, byHost: byHost))
        #endif
        let _ = rowActions.update(rename: { renamingSession = $0 }, close: { closingSession = $0 }, linkTodo: { linkingSession = $0 },
                                  delete: { deletingSession = $0 })

        let list = List {
            // On a phone the filter bar is the list's first row: it scrolls with the list, so the search field can
            // fold away and come back on a pull (a bar pinned in the safe area stayed put under a folded search field,
            // leaving a blurred gap, TestFlight 35). Beside the split (regular width) it stays above the list.
            if widthClass != .regular {
                Section {
                    filterBar
                        #if os(iOS) || os(visionOS)
                        .background { DrawerPan.NoRowSwipe() } // no swipe of its own: a drag to the right pages to 助手
                        #endif
                }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                    .listRowSeparator(.hidden)
            }
            #if os(iOS) || os(visionOS)
            if !model.desktops.isEmpty && query.isEmpty {
                Section { DesktopPresenceRow() } // 「Mac 上的 Persona 已登录」, WeChat's line over its chats
            }
            if model.hubNotice != nil && query.isEmpty {
                Section { HubNoticeRow() } // the hub's version: an update out, under way, or too old (docs/hub-update.md)
            }
            #endif
            if widthClass != .regular && showsHerdrNotices {
                // 「MacBook 的 herdr 没在运行」 启动 (#1916); beside the split they are under the filter bar (`herdrNoticesAbove`).
                ForEach(herdrStopped) { m in
                    Section { HerdrNoticeRow(machine: m, watch: herdr) }
                }
            }
            if !interactions.isEmpty && query.isEmpty {
                listSection {
                    ForEach(interactions, id: \.uid) { ix in
                        if compact {
                            // 细列表: one row like an agent's; the chat answers it.
                            NavigationLink(value: ix.sessionKey) {
                                CompactInteractionRow(interaction: ix, machine: machine(ix.machineRef)?.title)
                            }
                            .listRowInsets(CompactList.rowInsets)
                            .accessibilityIdentifier("agents.interaction")
                        } else if capsules {
                            // A card of its own on the page, as the agents' capsules.
                            // Several groups of hubs: the card says whose (one group: as it always was, no machine).
                            InteractionPreviewCard(interaction: ix, machine: model.isMultiHub ? machine(ix.machineRef)?.title : nil,
                                                   inCell: false) { openFromList(ix.sessionKey) }
                                .listRowInsets(Self.capsuleInsets)
                                .listRowBackground(Color.clear)
                                .listRowSeparator(.hidden)
                                .accessibilityIdentifier("agents.interaction")
                        } else {
                            InteractionPreviewCard(interaction: ix, machine: machine(ix.machineRef)?.title,
                                                   agentLabel: session(ix.sessionKey)?.agentLabel ?? "Claude", inCell: true) { openFromList(ix.sessionKey) }
                                .alignmentGuide(.listRowSeparatorLeading) { _ in 0 } // rows of different shapes: one line
                                .accessibilityIdentifier("agents.interaction")
                        }
                    }
                } header: {
                    SectionHeader(title: "需要你处理", count: interactions.count, tone: StatusPalette.needsYou)
                }
            }

            if quick == .unread && !unreadShown.isEmpty {
                Section {
                    Button { confirmReadAll = true } label: {
                        if compact {
                            HStack(spacing: CompactList.gap) {
                                Image(systemName: Theme.Icon.success).font(CompactList.metaStrong).scaledFrame(width: CompactList.mark)
                                Text("全部标为已读").font(CompactList.title).lineLimit(1)
                                Spacer()
                                Text("\(unreadShown.count) 个完成未看").font(CompactList.meta).foregroundStyle(.secondary).lineLimit(1)
                            }
                        } else if capsules {
                            HStack {
                                Label("全部标为已读", systemImage: Theme.Icon.success).lineLimit(1)
                                Spacer()
                                Text("\(unreadShown.count) 个完成未看").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                            .card(padding: Theme.Space.m)
                        } else {
                            HStack {
                                Label("全部标为已读", systemImage: Theme.Icon.success).lineLimit(1)
                                Spacer()
                                Text("\(unreadShown.count) 个完成未看").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                    }
                    .listRowInsets(compact ? CompactList.rowInsets : capsules ? Self.capsuleInsets : nil)
                    .listRowBackground(capsules ? Color.clear : nil)
                    .listRowSeparator(capsules ? .hidden : .automatic)
                    .confirmationDialog("把 \(unreadShown.count) 个完成未看的会话标为已读？", isPresented: $confirmReadAll, titleVisibility: .visible) {
                        Button("全部标为已读（\(unreadShown.count)）") {
                            model.markSeen(unreadShown)
                            kept = []
                        }
                    }
                }
            }

            if !pinnedAgents.isEmpty {
                listSection {
                    // No state word: the dot and the name's color already say it.
                    ForEach(pinnedAgents) { s in slotRow(s, context: true, inStateSection: true) }
                } header: {
                    SectionHeader(title: "置顶", count: nil, tone: StatusPalette.idle)
                }
            }

            if grouping == .todo {
                todoSections(unpinned.filter { $0.state.isLive })
            } else if grouping == .state {
                stateSections(unpinned.filter { $0.state.isLive })
            } else if grouping == .machine {
                machineSections(unpinned.filter { $0.state.isLive })
            } else if grouping == .repo {
                repoSections(tree, byHost: byHost)
            } else {
                spaceSections(active: active, quiet: quiet, byHost: byHost)
            }

            if base.isEmpty, !query.isEmpty {
                // Nothing matches the search: not 开始第一个开发会话 with its 新建会话, which read as if the agents were gone.
                Section {
                    EmptyStateView(symbol: Theme.Icon.search, title: "没有找到「\(query)」",
                                   message: "空间、标签、会话的标题和内容都能搜，换个词试试。")
                }
                .listRowBackground(Color.clear)
            } else if base.isEmpty {
                Section {
                    EmptyStateView(symbol: Theme.Icon.agents,
                                   title: machineFilter.isEmpty ? "开始第一个开发会话" : "这台机器上没有会话",
                                   message: Self.emptyMessage,
                                   action: ("新建会话", { showNew = true }))
                }
                .listRowBackground(Color.clear)
                .listRowSeparator(capsules ? .hidden : .automatic)
            } else if filtered.isEmpty, let st = quick.state {
                Section {
                    EmptyStateView(symbol: Theme.Icon.success, title: "没有\(st.label)的会话",
                                   message: machineFilter.isEmpty ? "各台电脑上的 Agent 有新动静会出现在这里。" : "这台机器上的 Agent 有新动静会出现在这里。",
                                   action: ("看全部", { choose(.all, base) }))
                }
                .listRowBackground(Color.clear)
                .listRowSeparator(capsules ? .hidden : .automatic)
            }
        }
        .modifier(AgentsListStyle(capsules: capsules))
        #if os(macOS)
        .modifier(MacListKeys(current: { macOpenNow }, delete: { key in deletingSession = session(key) }, move: macMove))
        #endif
        // Keep the phone's first-row controls close to the bar: active search otherwise adds the grouped list's
        // default first-section margin. The split's sidebar keeps its native content margins.
        .contentMargins(.top, widthClass == .regular ? nil : Theme.Space.xs, for: .scrollContent)
        .environment(\.defaultMinListRowHeight, compact ? 30 : 44)
        .environment(\.compactList, compact)
        #if DEBUG || PERF_SIGNPOST
        .overlay { if ScreenProbe.enabled { AgentsListProbe(feed: feed, sessions: base, asks: interactions, page: shell == nil ? nil : "agents") } }
        #endif
        .listSectionSpacing(compact ? .compact : .default)
        .modifier(AgentsListPage(capsules: capsules))
        // In the split's sidebar column (iPhone Duo open) the bar sits above the list; on a phone it is the list's
        // first row (above).
        .modifier(TopBar(abovePage: widthClass == .regular) {
            filterBar
            if showsHerdrNotices { herdrNoticesAbove } // beside the split: over the list, not its first section
        })
        .navigationTitle("Agents")
        // No title over the list (the tab says it); the name stays for the back button. The principal item (ChatsView)
        // takes the title's place: empty while all is well, 连接中… and the like while the hub is out of reach
        // (toolbar(removing: .title) would hide that too).
        .navigationBarTitleDisplayMode(.inline)
        .background { SearchCloseProbe(watcher: searchClose) }
        .onAppear { searchClose.foldOnce() } // the search field starts folded away; a pull shows it
        // Back in front (its tab picked again, the shell's Agents): folded again too (#740). Not on the way back from a
        // chat, which keeps the list as it was: that changes the view's appearance, not the tab.
        // From a view of its own: reading `tabIsActive` here drew the whole list again each time the page came to the
        // front or went behind (the Mac keeps it in the window behind other sections, #390).
        .background { PageFrontWatcher(initial: false) { [searchClose] active in if active { searchClose.refold() } } }
        #if (DEBUG || PERF_SIGNPOST) && (os(iOS) || os(visionOS))
        .background { if ScreenProbe.enabled { AgentsScrollProbe(page: shell == nil ? nil : "agents") } }
        #endif
        // The search field folds away as the list scrolls and comes back on a pull (2026-11-01 user request: the
        // always-shown field crowded the top). Safe only because the filter bar is no longer pinned in the safe area
        // on a phone: pinned, it stayed under the folded field (TestFlight 35, ~120 pt of blurred list).
        // On the list itself, before the alerts, as it always was.
        .pageSearchable(text: $query, isPresented: $searching, placement: .navigationBarDrawer(displayMode: .automatic),
                        prompt: "搜索空间、标签、会话、内容")
        .refreshable { await model.refresh() }
        // Whether each machine's herdr runs, while the list is in sight (a modifier of its own: in this chain the
        // compiler ran out of time type-checking it).
        .modifier(HerdrPolling(watch: herdr, key: HerdrWatch.key(machines), covered: covered))
        #if DEBUG
        // `-openQuietSpaces YES` (with `-tab dev`): every quiet space opened, for screenshots (`-dev.folded` folds spaces).
        .task(id: quiet.map(\.id)) { if UserDefaults.standard.bool(forKey: "openQuietSpaces") { expandedQuiet.formUnion(quiet.map(\.id)) } }
        #endif
        .renameSpaceAlert($renamingSpace)
        .renameSessionAlert($renamingSession)
        .confirmClose($closingSession)
        .confirmDeleteSession($deletingSession)
        .sheet(item: $linkingSession) { s in
            TodoLinkAgentSheet(session: s)
        }
        .sheet(item: $linkingSpace) { group in
            if let space = group.todoSpace {
                TodoLinkAgentSheet(space: TodoItem.Space(machineID: group.machineID, space: space))
            }
        }
        .onChange(of: matching) { _, now in kept.formUnion(now) }
        .onChange(of: query.isEmpty, initial: true) { _, empty in feed.searching = !empty }
        .onChange(of: machineFilter) { _, _ in kept = [] }
        .onAppear {
            feed.follow(model)
            #if os(iOS) || os(visionOS)
            Self.keepReader(feed.reader, searchClose)
            #endif
        }
        .onScrollPhaseChange { _, phase in
            #if (os(iOS) || os(visionOS)) && (DEBUG || PERF_SIGNPOST)
            PerfScroll.mark("Agents list \(phase)")
            #endif
            if phase != .idle { feed.reader.cancel() }
            feed.hold.set(.scrolling, phase != .idle)
        }
        .onChange(of: covered, initial: true) { _, covered in feed.hold.set(.outOfSight, covered) }
        // Another section in front (the page stays in the window behind it, `MacPageStack`).
        .background { PageFrontWatcher { front in feed.hold.set(.behind, !front) } }
        // The row being read, brought back when a change took it off screen (`ReaderAnchor.reveal`). Around a local:
        // the chain above in a closure is type-checked with all of it.
        ScrollViewReader { proxy in
            list.onAppear {
                feed.reader.reveal = { id, at in proxy.scrollTo(id, anchor: UnitPoint(x: 0.5, y: at)) }
                #if os(macOS)
                // Again once the rows on the way are laid out: the first pass goes by the lazy list's estimates and
                // stopped short of a row far below (#2213).
                macRows.scroll = { id in
                    proxy.scrollTo(id)
                    Task { @MainActor in
                        try? await Task.sleep(for: .milliseconds(120))
                        proxy.scrollTo(id)
                    }
                }
                #endif
            }
        }
        .environment(feed) // a quiet space's line reads its agents' times from the rows (`SpaceGroup.updatedAt(_:)`)
    }

    /// The quick filter bar. Equal while the counts are: the bar is not drawn again for pushes that change none of
    /// them. The tap reads the list as it is then.
    private var filterBar: some View {
        QuickFilterBar(selection: quick, counts: Self.counts(base, muted: model.isMuted)) { choose($0, self.base) }.equatable()
    }

    /// Each quick filter's count. Out of the body's modifier chain: closures in a long chain are type-checked with all
    /// of it (Release builds on the release machine time out).
    private static func counts(_ base: [Session], muted: (Session) -> Bool) -> [AgentFilter: Int] {
        var out: [AgentFilter: Int] = [:]
        for f in AgentFilter.allCases where f != .all {
            out[f] = base.count { f.matches($0, muted: muted($0)) }
        }
        return out
    }

    /// A quick filter chosen (again): it starts from what matches now.
    private func choose(_ f: AgentFilter, _ base: [Session]) {
        Haptics.tap()
        quick = f
        kept = Set(base.filter { f != .all && f.matches($0, muted: model.isMuted($0)) }.map(\.key))
    }

    #if os(macOS)
    /// The agents in the order the list shows them (`AgentListRows`), for ↑ / ↓.
    private func rowsInOrder(unpinned: [Session], pinned: [Session], active: [SpaceGroup], quiet: [SpaceGroup],
                             tree: [RepoTree.Host], byHost: Bool) -> [SessionKey] {
        let rows = AgentListRows(order: order, grouping: grouping, all: quick == .all, searching: !query.isEmpty,
                                 isFolded: { isFolded($0) }, expandedIdle: expandedIdle, expandedQuiet: expandedQuiet)
        let live = unpinned.filter { $0.state.isLive }
        let shown: [Session]
        switch grouping {
        case .todo:
            let groups = TodosLogic.buildTodoAgentGroups(sessions: live, todos: TodoStore.shared.todos, isMuted: model.isMuted)
            shown = rows.todos(groups) { g in g.todoID == nil ? isFolded(g.key, otherwise: g.folded) : isFolded(g.key) }
        case .state: shown = rows.states(live)
        case .machine: shown = rows.machines(live, order: machineOrder(live).map(\.ref))
        case .repo: shown = rows.repos(tree, byHost: byHost)
        case .space:
            let hosts = byHost ? RepoTree.hostOrder(Set((active + quiet).map(\.machineRef)), machines: machines.map(\.ref)) : nil
            shown = rows.spaces(active: active, quiet: quiet, hosts: hosts)
        }
        return (pinned + shown).map(\.key)
    }

    /// ↑ / ↓: the agent above or below the open one, opened and scrolled to (the list does not select, #2213).
    private func macMove(_ by: Int) {
        guard let next = AgentListRows.step(macRows.keys, from: macOpenNow, by: by) else { return }
        openFromList(next)
        if let id = session(next)?.id { macRows.scroll?(id) }
    }
    #endif

    /// The agent open beside the list, if it is one of the list's (not a Persona conversation).
    private var macOpenNow: SessionKey? { model.visibleSession.flatMap { session($0)?.assistant == true ? nil : $0 } }

    /// Something the list opens: in the split it replaces the detail column; in one stack the list is at the root, so
    /// this is the same as pushing it.
    private func openFromList(_ value: some Hashable) {
        spaceAt = (value as? SpaceRoute).map { [0: $0.id] } ?? [:]
        path = NavigationPath([value])
    }

    #if os(iOS) || os(visionOS)
    /// `ReaderAnchor` on the list's own scroll view.
    private static func keepReader(_ reader: ReaderAnchor, _ watcher: SearchCloseWatcher) {
        reader.shown = { [weak watcher] in
            guard let list = watcher?.listScrollView, list.window != nil else { return nil }
            let inset = list.adjustedContentInset
            let frame = list.convert(list.bounds, to: nil)
            let top = frame.minY + inset.top, bottom = frame.maxY - inset.bottom
            guard bottom > top else { return nil }
            return (top...bottom, list.contentOffset.y <= -inset.top + 1)
        }
        reader.scroll = { [weak watcher] dy in
            guard let list = watcher?.listScrollView else { return }
            let inset = list.adjustedContentInset
            let lowest = -inset.top
            let highest = max(lowest, list.contentSize.height + inset.bottom - list.bounds.height)
            list.contentOffset.y = min(max(list.contentOffset.y + dy, lowest), highest)
        }
        reader.moving = { [weak watcher] in
            guard let list = watcher?.listScrollView else { return false }
            return list.isTracking || list.isDragging || list.isDecelerating
        }
    }
    #endif

    /// Opens a search result: the search closes first (open, it keeps the system navigation bar up over the chat), and
    /// the chat is pushed once it has folded away. The search's text and results stay for coming back.
    private func openFromSearch(_ key: SessionKey) {
        searchedBeforePush = query
        searching = false
        searchClose.whenClosed { path.append(key) }
    }

    /// The row follows its own agent (`AgentsFeed.Row`): what only it shows changes it, not the list.
    @ViewBuilder private func slotRowContent(_ s: Session, context: Bool, inStateSection: Bool, cardLast: Bool? = nil, showHost: Bool = false,
                                             pane: TabPanes.Role? = nil) -> some View {
        if feed.following {
            FeedRow(row: feed.row(s), reader: feed.reader) { live, time in
                slotRowDrawn(live, time: time, context: context, inStateSection: inStateSection, cardLast: cardLast, showHost: showHost, pane: pane)
            }
        } else {
            slotRowDrawn(s, time: nil, context: context, inStateSection: inStateSection, cardLast: cardLast, showHost: showHost, pane: pane)
        }
    }

    @ViewBuilder private func slotRowDrawn(_ s: Session, time: AgentTime?, context: Bool, inStateSection: Bool, cardLast: Bool?, showHost: Bool = false,
                                           pane: TabPanes.Role? = nil) -> some View {
        // herdr is where most agents run: only the others name their host
        let host = showHost ? s.hostKind.flatMap { $0 == "herdr" ? nil : $0 } : nil
        if compact {
            CompactSlotRow(session: s, muted: model.isMuted(s), space: context ? (s.spaceName ?? s.folderName) : nil, host: host, time: time, pane: pane)
                .equatable()
        } else {
            SlotRow(session: s, muted: model.isMuted(s), space: context ? (s.spaceName ?? s.folderName) : nil,
                    machine: context && grouping != .machine && machineFilter.isEmpty ? machine(s.machineRef)?.title : nil, host: host,
                    hideState: inStateSection, surface: cardLast.map { .cardRow(last: $0) } ?? .capsule, time: time, pane: pane)
                .equatable()
        }
    }

    // MARK: By space

    /// 按项目: the spaces as they always were; with agents on more than one machine, each machine's spaces under its
    /// heading, its quiet ones in its own 空闲的空间 (#2108).
    @ViewBuilder private func spaceSections(active: [SpaceGroup], quiet: [SpaceGroup], byHost: Bool) -> some View {
        if byHost {
            let hosts = RepoTree.hostOrder(Set((active + quiet).map(\.machineRef)), machines: machines.map(\.ref))
            ForEach(hosts, id: \.self) { ref in
                let mine = active.filter { $0.machineRef == ref }
                let still = quiet.filter { $0.machineRef == ref }
                hostGroup(ref, sessions: (mine + still).flatMap(\.sessions)) {
                    ForEach(mine) { g in spaceSection(g, namesMachine: false) }
                    quietSection(still, title: "空闲的空间", namesMachine: false)
                }
            }
        } else {
            ForEach(active) { g in spaceSection(g, namesMachine: machineFilter.isEmpty) }
            quietSection(quiet, title: "空闲的空间", namesMachine: machineFilter.isEmpty)
        }
    }

    /// A space: tap its header to fold it (the counts stay on it); busy (and pinned / looping) agents first, idle ones
    /// fold into one row so a big workspace stays compact. Under a quick filter every agent it left shows.
    private func spaceSection(_ g: SpaceGroup, namesMachine: Bool) -> some View {
        foldableSection(g.id) { spaceHeader(g, folded: $0, namesMachine: namesMachine) } content: { agentRows(g.sessions, key: g.id) }
    }

    /// A section that folds from its header. Folded: the header alone, as a plain row. As an empty section each kept a
    /// header's and a section's padding (全部折叠 showed six a screen).
    @ViewBuilder private func foldableSection<Header: View, Content: View>(_ id: String, @ViewBuilder header: (Bool) -> Header,
                                                                           @ViewBuilder content: () -> Content) -> some View {
        if isFolded(id) {
            Section {
                header(true)
                    // Where a header sits; the capsules: the page's edge, where an open space's title is too.
                    .listRowInsets(capsules ? EdgeInsets(top: 0, leading: Theme.Space.page, bottom: 0, trailing: Theme.Space.page)
                                   : Self.foldedHeaderInsets)
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
            .listSectionSpacing(.custom(0))
        } else {
            listSection(content: content) { header(false) }
        }
    }

    /// The agents of a space or a working tree: busy (and pinned / looping) ones first, idle ones behind one 还有 N 个空闲
    /// row (`key` remembers it open). `indent`: a level in, under a working tree's row.
    /// The agents of a tab with several together, under the tab's row (`tabRows`, #2320).
    @ViewBuilder private func agentRows(_ list: [Session], key: String, indent: Bool = false) -> some View {
        let rows = order.tabEntries(list, all: quick == .all)
        ForEach(rows.busy) { e in entryRows(e, indent: indent) }
        if !rows.idle.isEmpty {
            let idle = rows.idle.flatMap(\.sessions)
            if expandedIdle.contains(key) || !query.isEmpty {
                ForEach(rows.idle) { e in entryRows(e, indent: indent) }
                if query.isEmpty { collapseRow("收起空闲的 \(idle.count) 个", key: key, indent: indent) }
            } else {
                expandRow(idle, key: key, indent: indent)
            }
        }
    }

    /// One agent's row, or a tab's: its row, then (unless folded) its agents, the 发起者 on top marked, the others a
    /// level in under it.
    @ViewBuilder private func entryRows(_ e: TabPanes.Entry, indent: Bool = false) -> some View {
        switch e {
        case .agent(let s):
            slotRow(s, indent: indent)
        case .tab(let g):
            tabRow(g, indent: indent)
            if !isFolded(g.foldID) {
                ForEach(g.sessions) { s in slotRow(s, indent: indent, pane: g.role(of: s), branch: g.isBranch(s)) }
            }
        }
    }

    /// A tab's row: its name, what its most urgent agent is doing in that one's state, 「N 个 Agent」; tap to fold it.
    private func tabRow(_ g: TabPanes.Group, indent: Bool) -> some View {
        let folded = isFolded(g.foldID)
        return Button { toggleFold(g.foldID) } label: {
            TabGroupRow(group: g, folded: folded).padding(.leading, nested(indent ? 1 : 0))
        }
        .buttonStyle(.plain)
        .modifier(FoldAnchor(id: g.foldID, reader: feed.reader))
        .listRowInsets(compact ? rowInsets(level: indent ? 1 : 0) : capsules ? Self.tabCapsuleInsets(level: indent ? 1 : 0) : nil)
        .listRowBackground(capsules ? Color.clear : nil)
        .listRowSeparator(capsules ? .hidden : .automatic)
        .accessibilityIdentifier("agents.tab")
    }

    /// A visible ⋯ for open / mark read / pin / mute: long press on a list section header does not reach a context menu.
    private func spaceHeader(_ g: SpaceGroup, folded isFolded: Bool, namesMachine: Bool) -> some View {
        HStack(spacing: Theme.Space.xs) {
            Button { toggleFold(g.id) } label: {
                SpaceHeader(group: g, machine: machine(g.machineRef), showMachine: namesMachine,
                            pinned: model.isPinned(g), muted: model.isMuted(g), folded: isFolded)
            }
            .buttonStyle(.plain)
            TodoSpaceTag(group: g) { todoID in openFromList(TodoDetailRoute(id: todoID)) }
            Menu {
                SpaceHeaderMenu(group: g, rename: { renamingSpace = g }, open: { openFromList(SpaceRoute(id: g.id)) },
                                linkTodo: { linkingSpace = g }).equatable()
            } label: {
                headerMenuLabel
            }
            // 细列表: as quiet as the counts beside it (a Menu's label takes the accent otherwise).
            .tint(compact ? Color.secondary : nil)
            .labelOnlyMenu()
            .accessibilityLabel("\(g.name) 的更多操作")
        }
    }

    /// The ⋯ on a space's or a repository's header.
    private var headerMenuLabel: some View {
        Image(systemName: compact ? Theme.Icon.more : Theme.Icon.moreMenu)
            .font(compact ? CompactList.metaStrong : Theme.Font.body)
            .foregroundStyle(.secondary)
            .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
            .contentShape(.rect)
    }

    /// The quiet spaces or repositories, one line each, opened in place: 空闲的空间 / 空闲的仓库.
    @ViewBuilder private func quietSection(_ groups: [SpaceGroup], title: String, namesMachine: Bool, repos: Bool = false) -> some View {
        if !groups.isEmpty {
            listSection {
                ForEach(groups) { g in
                    if compact { compactQuietSpace(g, namesMachine: namesMachine, repo: repos) } else if capsules {
                        quietSpaceCard(g, namesMachine: namesMachine, repo: repos)
                    } else {
                        DisclosureGroup(isExpanded: Binding(
                            get: { expandedQuiet.contains(g.id) || !query.isEmpty },
                            set: { if $0 { expandedQuiet.insert(g.id) } else { expandedQuiet.remove(g.id) } }
                        )) {
                            let roles = TabPanes.roles(g.sessions)
                            ForEach(TabPanes.grouped(g.sessions)) { s in slotRow(s, pane: roles[s.key]) }
                        } label: {
                            QuietSpaceRow(group: g, machine: machine(g.machineRef), showMachine: namesMachine, repo: repos)
                        }
                    }
                }
            } header: {
                SectionHeader(title: title, count: groups.count, tone: StatusPalette.idle)
            }
        }
    }

    // MARK: By repository

    /// The fold id of a machine where the list is split by machine: the same in 按仓库, 按项目 and 按机器.
    private static func hostFoldID(_ ref: MachineRef) -> String { "machine/" + ref.path }

    /// Nothing going on in it: every agent idle (none pinned, looping or with work in the background). Under a quick
    /// filter nothing is quiet: every repository it leaves is listed.
    private func isQuiet(_ r: RepoTree.Repo) -> Bool { quick == .all && r.sessions.allSatisfy { !stays($0) } }

    /// 按仓库 (#2108): the machines when there is more than one, each machine's repositories by name with their working
    /// trees in their card, as Orca's projects; repositories with nothing going on fold into the machine's 空闲的仓库.
    @ViewBuilder private func repoSections(_ tree: [RepoTree.Host], byHost: Bool) -> some View {
        ForEach(tree) { host in
            if byHost {
                hostGroup(host.ref, sessions: host.sessions) { hostRepos(host) }
            } else {
                hostRepos(host)
            }
        }
    }

    /// A machine's repositories: the ones with something going on, then the rest folded into its 空闲的仓库.
    @ViewBuilder private func hostRepos(_ host: RepoTree.Host) -> some View {
        ForEach(host.repos.filter { !isQuiet($0) }) { r in repoSection(r) }
        quietSection(host.repos.filter(isQuiet).map { r in
            SpaceGroup(id: r.id, machineID: r.machine.id, spaceID: "-", group: r.machine.group, name: r.name, index: 0,
                       sessions: ordered(r.sessions))
        }, title: "空闲的仓库", namesMachine: false, repos: true)
    }

    /// A machine's heading and, unless the machine is folded, the sections under it (its quiet ones last). iOS: the heading is a section of its own, the sections follow it.
    /// The Mac: the heading is the header of one section that holds the machine's sections. A Mac list has no
    /// `listSectionSpacing` and puts about 24pt over every section's header; a heading in a section of its own got that
    /// band above and the next section's below (#2318, about 100pt a heading). Sections inside a section are drawn as
    /// its rows (their headers too), so the machine's repositories or spaces sit right under its heading.
    @ViewBuilder private func hostGroup<Content: View>(_ ref: MachineRef, sessions: [Session],
                                                       @ViewBuilder content: () -> Content) -> some View {
        let open = !isFolded(Self.hostFoldID(ref))
        #if os(macOS)
        Section {
            if open { content() }
        } header: {
            hostHeadingRow(ref, sessions: sessions)
        }
        #else
        hostHeading(ref, sessions: sessions)
        if open { content() }
        #endif
    }

    /// A machine's heading: its mark, name and counts; tap to fold the machine.
    private func hostHeadingRow(_ ref: MachineRef, sessions: [Session]) -> some View {
        let id = Self.hostFoldID(ref)
        let m = machine(ref)
        let group = SpaceGroup(id: id, machineID: ref.id, spaceID: "-", group: ref.group, name: m?.title ?? ref.id, index: 0, sessions: sessions)
        return FoldButton(title: group.name, folded: isFolded(id), detail: StateCounts.spoken(group)) { toggleFold(id) } label: {
            HostHeading(symbol: m?.symbol ?? Theme.Icon.machine, title: group.name, group: group)
        }
        .accessibilityIdentifier("agents.host")
    }

    #if !os(macOS)
    /// A machine's heading: a plain row on the page's edge, a level above the headers under it.
    private func hostHeading(_ ref: MachineRef, sessions: [Session]) -> some View {
        Section {
            hostHeadingRow(ref, sessions: sessions)
                .listRowInsets(EdgeInsets(top: Theme.Space.l, leading: Theme.Space.page, bottom: Theme.Space.xs, trailing: Theme.Space.page))
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
        }
        .listSectionSpacing(.custom(0))
    }
    #endif

    /// A repository: its agents right in its card when it has only its main checkout, else its working trees, each a
    /// row with its agents a level in under it.
    private func repoSection(_ r: RepoTree.Repo) -> some View {
        foldableSection(r.id) { repoHeader(r, folded: $0) } content: {
            if r.flat {
                agentRows(r.worktrees[0].sessions, key: r.worktrees[0].id)
            } else {
                ForEach(r.worktrees) { w in worktreeRows(w) }
            }
        }
    }

    /// A repository's header: its name and counts; when its agents are all in one herdr space (the usual case), that
    /// space's marks, todo and ⋯ (pin, mute, rename…) are the repository's.
    private func repoHeader(_ r: RepoTree.Repo, folded isFolded: Bool) -> some View {
        let spaces = SpaceGroup.build(r.sessions)
        let space = spaces.count == 1 && spaces[0].spaceID != "-" ? spaces[0] : nil
        let counts = SpaceGroup(id: r.id, machineID: r.machine.id, spaceID: "-", group: r.machine.group, name: r.name, index: 0, sessions: r.sessions)
        let unread = r.sessions.filter { $0.state == .unread }.map(\.key)
        return HStack(spacing: Theme.Space.xs) {
            Button { toggleFold(r.id) } label: {
                SpaceHeader(group: counts, machine: nil, showMachine: false, pinned: space.map(model.isPinned) ?? false,
                            muted: space.map(model.isMuted) ?? false, folded: isFolded, symbol: Theme.Icon.folder)
            }
            .buttonStyle(.plain)
            if let space {
                TodoSpaceTag(group: space) { todoID in openFromList(TodoDetailRoute(id: todoID)) }
            }
            if space != nil || !unread.isEmpty {
                Menu {
                    if let space {
                        SpaceHeaderMenu(group: space, rename: { renamingSpace = space }, open: { openFromList(SpaceRoute(id: space.id)) },
                                        linkTodo: { linkingSpace = space }).equatable()
                    } else {
                        RepoMenuItems(unread: unread).equatable()
                    }
                } label: {
                    headerMenuLabel
                }
                .tint(compact ? Color.secondary : nil)
                .labelOnlyMenu()
                .accessibilityLabel("\(r.name) 的更多操作")
            }
        }
    }

    /// A working tree in its repository's card: its row (⌂ for the main checkout), then its agents a level in. One with
    /// nothing going on starts folded, as a quiet space does; a busy one folds from its row, remembered.
    @ViewBuilder private func worktreeRows(_ w: RepoTree.Worktree) -> some View {
        let quiet = quick == .all && w.sessions.allSatisfy { !stays($0) }
        let open = !query.isEmpty || (quiet ? expandedQuiet.contains(w.id) : !isFolded(w.id))
        Button {
            if quiet {
                Haptics.tap()
                withAnimation(Theme.Motion.standard) {
                    if open { expandedQuiet.remove(w.id) } else { expandedQuiet.insert(w.id) }
                }
            } else {
                toggleFold(w.id)
            }
        } label: {
            WorktreeRow(name: w.name, main: w.main, count: w.sessions.count, quiet: quiet, open: open)
        }
        .buttonStyle(.plain)
        .listRowInsets(compact ? CompactList.rowInsets : capsules ? Self.worktreeCapsuleInsets : nil)
        .listRowBackground(capsules ? Color.clear : nil)
        .listRowSeparator(capsules ? .hidden : .automatic)
        .accessibilityIdentifier("agents.worktree")
        if open {
            if quiet {
                ForEach(TabPanes.entries(ordered(w.sessions))) { e in entryRows(e, indent: true) }
            } else {
                agentRows(w.sessions, key: w.id, indent: true)
            }
        }
    }

    // MARK: By state / by machine

    private var order: AgentListOrder {
        AgentListOrder(isPinned: model.isPinned, isMuted: model.isMuted, byRank: quick == .all && sort == .state, held: model.heldInList)
    }
    /// 按机器's machines: those with live agents, in their own order; under 最近更新 the one with the latest agent first.
    /// The list and ↑ / ↓ (`rowsInOrder`) both go by it.
    private func machineOrder(_ live: [Session]) -> [Machine] {
        let shown = machines.filter { m in live.contains { $0.machineRef == m.ref } }
        guard sort == .updated else { return shown }
        return AgentListOrder.latestFirst(shown, date: { m in
            live.filter { $0.machineRef == m.ref }.map(\.sortDate).max() ?? .distantPast
        }, id: \.ref.path)
    }

    private func latestFirst(_ groups: [SpaceGroup]) -> [SpaceGroup] {
        AgentListOrder.latestFirst(groups, date: \.sortDate, id: \.id)
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

    /// A section per rank (`rank`); 已看 starts folded, it is the long tail, and its header is its one fold: no
    /// 还有 N 个空闲 row inside it as well (2026-10-31: it took two taps where the other states take one). 待唤醒: nothing new
    /// yet, but a /loop or background work will bring some.
    @ViewBuilder private func stateSections(_ live: [Session]) -> some View {
        let byRank = Dictionary(grouping: live, by: rank)
        let buckets = Self.stateTitles.enumerated().map { i, t in (key: t.key, title: t.title, rows: byRank[i] ?? [], tint: t.tint) }
        ForEach(buckets, id: \.key) { b in
            let rows = ordered(b.rows)
            let id = "state/" + b.key
            if !rows.isEmpty {
                listSection {
                    if isFolded(id) {
                    } else if b.key == "idle" {
                        ForEach(rows) { s in slotRow(s, context: true, inStateSection: true) }
                    } else {
                        // 已屏蔽 mixes states: each row says its own. Loops and background work stay off the rows (the chat shows them).
                        ForEach(rows) { s in slotRow(s, context: true, inStateSection: b.key != "muted") }
                    }
                } header: {
                    FoldButton(title: b.title, folded: isFolded(id), detail: "\(rows.count) 个") { toggleFold(id) } label: {
                        SectionHeader(title: b.title, count: rows.count, tone: b.tint)
                    }
                    .modifier(FoldAnchor(id: id, reader: feed.reader))
                }
                .listSectionSpacing(isFolded(id) ? .custom(0) : .default)
            }
        }
    }

    /// One section per todo, its agents ordered; agents opened by hand in herdr go into "手动开的" (open); the other
    /// unassigned ones into "未归属" (folded by default, open while one of them needs you or nothing is above it:
    /// `TodoAgentGroup.folded`).
    @ViewBuilder private func todoSections(_ live: [Session]) -> some View {
        let todoGroups = TodosLogic.buildTodoAgentGroups(sessions: live, todos: TodoStore.shared.todos, isMuted: model.isMuted)
        let mixedHosts = Set(live.compactMap(\.hostKind)).count > 1
        ForEach(todoGroups) { g in
            let id = g.key
            let folded = g.todoID == nil ? isFolded(id, otherwise: g.folded) : isFolded(id)
            if !g.sessions.isEmpty {
                listSection {
                    if !folded {
                        ForEach(g.sessions) { s in slotRow(s, context: true, showHost: mixedHosts) }
                    }
                } header: {
                    FoldButton(title: g.name, folded: folded, detail: "\(g.sessions.count) 个") { toggleFold(id, shown: folded) } label: {
                        HStack(spacing: Theme.Space.sm) {
                            if compact {
                                Text(g.name).font(CompactList.sectionTitle).foregroundStyle(Color.primary).textCase(nil).lineLimit(1)
                            } else {
                                Text(g.name)
                            }
                            if let st = g.todoStatus {
                                CountBadge(count: g.sessions.count, look: .tone(st.todoTone))
                            } else {
                                CountBadge(count: g.sessions.count, look: .tone(StatusPalette.idle))
                            }
                            if let todoID = g.todoID,
                                g.sessions.allSatisfy({ TodosLogic.isOnTodosHub(sessionGroup: $0.key.group, hubGroup: model.hub?.group) }) {
                                Button {
                                    openFromList(TodoDetailRoute(id: todoID))
                                } label: {
                                    Image(systemName: Theme.Icon.disclosure)
                                        .font(compact ? CompactList.metaStrong : Theme.Font.body)
                                        .foregroundStyle(.secondary)
                                        .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                                        .contentShape(.rect)
                                }
                                .buttonStyle(.plain)
                                .accessibilityLabel("查看待办详情")
                            }
                        }
                    }
                }
                .listSectionSpacing(folded ? .custom(0) : .default)
            }
        }
    }

    /// One section per machine, its agents by state; idle ones fold like in a space.
    @ViewBuilder private func machineSections(_ live: [Session]) -> some View {
        ForEach(machineOrder(live), id: \.ref) { m in
            let rows = ordered(live.filter { $0.machineRef == m.ref })
            let id = "machine/" + m.ref.path
            let group = SpaceGroup(id: "m-" + m.ref.path, machineID: m.id, spaceID: "-", group: m.group, name: m.title, index: 0, sessions: rows)
            listSection {
                if !isFolded(id) {
                    if quick == .all {
                        ForEach(rows.filter(stays)) { s in slotRow(s, context: true) }
                        foldedRows(rows.filter { !stays($0) }, key: "machine-" + m.ref.path, label: "空闲")
                    } else {
                        ForEach(rows) { s in slotRow(s, context: true) }
                    }
                }
            } header: {
                FoldButton(title: m.title, folded: isFolded(id), detail: StateCounts.spoken(group)) { toggleFold(id) } label: {
                    MachineSectionHeader(machine: m, group: group)
                }
            }
            .listSectionSpacing(isFolded(id) ? .custom(0) : .default)
        }
    }

    /// Quiet rows behind one "还有 N 个…" row (always open while searching).
    @ViewBuilder private func foldedRows(_ rows: [Session], key: String, label: String, inStateSection: Bool = false) -> some View {
        if !rows.isEmpty {
            if expandedIdle.contains(key) || !query.isEmpty {
                ForEach(rows) { s in slotRow(s, context: true, inStateSection: inStateSection) }
                if query.isEmpty { collapseRow("收起\(label)的 \(rows.count) 个", key: key) }
            } else {
                expandRow(rows, key: key)
            }
        }
    }

    /// 还有 N 个空闲: the idle agents behind one row.
    private func expandRow(_ rows: [Session], key: String, indent: Bool = false) -> some View {
        Button {
            Haptics.tap()
            withAnimation(Theme.Motion.standard) { _ = expandedIdle.insert(key) }
        } label: {
            Group { if compact { CompactFoldRow(sessions: rows) } else { IdleFoldRow(sessions: rows) } }
                .padding(.leading, nested(indent))
        }
        .buttonStyle(.plain)
        .listRowInsets(rowInsets(indent: indent))
        .listRowBackground(capsules ? Color.clear : nil)
        .listRowSeparator(capsules ? .hidden : .automatic)
    }

    private func collapseRow(_ title: String, key: String, indent: Bool = false) -> some View {
        Button {
            withAnimation(Theme.Motion.standard) { _ = expandedIdle.remove(key) }
        } label: {
            Group {
                if compact {
                    CompactCollapseRow(title: title)
                } else {
                    Label(title, systemImage: Theme.Icon.collapse).font(Theme.Font.secondaryMedium).lineLimit(1)
                }
            }
            .padding(.leading, nested(indent))
        }
        .listRowInsets(rowInsets(indent: indent))
        .listRowBackground(capsules ? Color.clear : nil)
        .listRowSeparator(capsules ? .hidden : .automatic)
    }

    /// A row's edges in 细列表 and the capsules; `indent`: a level in, under a working tree (按仓库). Nil: the grouped
    /// list's own, the row's content moved in instead (`nested`).
    private func rowInsets(indent: Bool) -> EdgeInsets? { rowInsets(level: indent ? 1 : 0) }

    /// `level` levels in: under a working tree, under a tab's 发起者, or both.
    private func rowInsets(level: Int) -> EdgeInsets? {
        if compact { return CompactList.rowInsets(level: level) }
        if capsules { return Self.capsuleInsets(level: level) }
        return nil
    }

    /// How far a row's content moves in under a working tree in the grouped list (its row keeps the list's insets, so
    /// its line starts where its text does).
    private func nested(_ indent: Bool) -> CGFloat { nested(indent ? 1 : 0) }
    private func nested(_ level: Int) -> CGFloat { !compact && !capsules ? Theme.Space.l * CGFloat(level) : 0 }

    /// 细列表: a quiet space as one row like an agent's, its agents under it when opened (a DisclosureGroup's own row
    /// is taller and its arrow heavier than everything else in the list).
    @ViewBuilder private func compactQuietSpace(_ g: SpaceGroup, namesMachine: Bool, repo: Bool) -> some View {
        let open = expandedQuiet.contains(g.id) || !query.isEmpty
        Button {
            withAnimation(Theme.Motion.standard) {
                if open { expandedQuiet.remove(g.id) } else { expandedQuiet.insert(g.id) }
            }
        } label: {
            CompactQuietSpaceRow(group: g, machine: machine(g.machineRef), showMachine: namesMachine, expanded: open, repo: repo)
        }
        .buttonStyle(.plain)
        .listRowInsets(CompactList.rowInsets)
        .accessibilityIdentifier("agents.quietSpace")
        if open {
            let roles = TabPanes.roles(g.sessions)
            ForEach(TabPanes.grouped(g.sessions)) { s in slotRow(s, pane: roles[s.key]) }
        }
    }

    /// The agents as BeautifulUI's capsules: its style, cards (not 细列表).
    private var capsules: Bool { !compact && AppStyle.current.usesBeautifulUI }

    #if os(macOS)
    /// A folded space's header, a plain row, where an open section's header sits: the row's own insets on the Mac
    /// (a fixed 20 put it 20pt in on both sides and cut its name short, #405).
    private static let foldedHeaderInsets: EdgeInsets? = nil
    #else
    /// A folded space's header, a plain row, where an open section's header sits: 20 is the system's inset for an
    /// inset-grouped section header on iOS (matching it, not a design value).
    private static let foldedHeaderInsets: EdgeInsets? = EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20)
    #endif

    /// A capsule's room: the page's edges, a little air above and below.
    private static let capsuleInsets = EdgeInsets(top: Theme.Space.xs, leading: Theme.Space.page, bottom: Theme.Space.xs, trailing: Theme.Space.page)
    /// A capsule `level` levels in, under a working tree's row or a tab's 发起者: a level is as far in as a quiet space's
    /// opened card sets its agents.
    private static func capsuleInsets(level: Int) -> EdgeInsets {
        EdgeInsets(top: Theme.Space.xs, leading: Theme.Space.page + Theme.Space.l * CGFloat(level), bottom: Theme.Space.xs, trailing: Theme.Space.page)
    }
    /// A tab's row among the capsules: a line of text on the page over its agents' capsules, as a working tree's.
    private static func tabCapsuleInsets(level: Int) -> EdgeInsets {
        EdgeInsets(top: Theme.Space.s, leading: Theme.Space.page + Theme.Space.l * CGFloat(level), bottom: Theme.Space.xxs, trailing: Theme.Space.page)
    }
    /// A working tree's row among the capsules: a line of text on the page, over its agents' capsules.
    private static let worktreeCapsuleInsets = EdgeInsets(top: Theme.Space.s, leading: Theme.Space.page, bottom: Theme.Space.xxs,
                                                          trailing: Theme.Space.page)

    /// A section. The capsules are a plain list (every row sets its own edges, all on `Theme.Space.page`): its title is
    /// its first row on the page's edge (a plain list's headers stick to the top, and a grouped list pads its first
    /// section about 35 pt). Elsewhere the grouped list's header.
    @ViewBuilder private func listSection<Content: View, Header: View>(@ViewBuilder content: () -> Content,
                                                                      @ViewBuilder header: () -> Header) -> some View {
        if capsules {
            Section {
                header()
                    .font(Theme.Font.sectionHeader)
                    .foregroundStyle(.secondary)
                    .textCase(nil)
                    .listRowInsets(EdgeInsets(top: Theme.Space.l, leading: Theme.Space.page, bottom: Theme.Space.xs, trailing: Theme.Space.page))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                content()
            }
        } else {
            Section { content() } header: { header() }
        }
    }

    /// The capsules: a quiet space is one card, its title on top and, opened, its agents under it, set in a level.
    @ViewBuilder private func quietSpaceCard(_ g: SpaceGroup, namesMachine: Bool, repo: Bool) -> some View {
        let open = expandedQuiet.contains(g.id) || !query.isEmpty
        Button {
            withAnimation(Theme.Motion.standard) {
                if open { expandedQuiet.remove(g.id) } else { expandedQuiet.insert(g.id) }
            }
        } label: {
            QuietSpaceCardHead(group: g, machine: machine(g.machineRef), showMachine: namesMachine, open: open, repo: repo)
        }
        .buttonStyle(.plain)
        .listRowInsets(EdgeInsets(top: Theme.Space.xs, leading: Theme.Space.page, bottom: open ? 0 : Theme.Space.xs, trailing: Theme.Space.page))
        .listRowBackground(Color.clear)
        .listRowSeparator(.hidden)
        .accessibilityIdentifier("agents.quietSpace")
        if open {
            // A card is one surface: a tab's agents sit together in it, named and marked, without a row of the tab's own.
            let rows = TabPanes.grouped(g.sessions), roles = TabPanes.roles(g.sessions)
            ForEach(rows) { s in slotRow(s, cardLast: s.id == rows.last?.id, pane: roles[s.key]) }
        }
    }

    /// `context`: also name the space (and machine) — when the list is not grouped by space.
    /// `cardLast`: a row of a quiet space's card (`quietSpaceCard`), and whether it ends the card.
    /// `indent`: a level in, under a working tree's row (按仓库).
    /// `pane`, `branch`: an agent of a tab with several, and one hanging off its 发起者 (a level further in).
    private func slotRow(_ s: Session, context: Bool = false, inStateSection: Bool = false, cardLast: Bool? = nil,
                         showHost: Bool = false, indent: Bool = false, pane: TabPanes.Role? = nil, branch: Bool = false) -> some View {
        let level = (indent ? 1 : 0) + (branch ? 1 : 0)
        return Group {
            if searching {
                // A search result is a button, not a link: a link pushed at once, the push was refused (the search has
                // to close first) and slid back, and the push made after it was lost (2026-10-31).
                Button { openFromSearch(s.key) } label: {
                    HStack(spacing: Theme.Space.s) {
                        slotRowContent(s, context: context, inStateSection: inStateSection, cardLast: cardLast, showHost: showHost, pane: pane)
                            .padding(.leading, nested(level))
                        // 胶囊 draws its own chevron; 列表 and Persona' own rows get the one a link would show.
                        if !capsules { Image(systemName: Theme.Icon.disclosure).font(Theme.Font.captionStrong).foregroundStyle(.tertiary) }
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            } else {
                NavigationLink(value: s.key) {
                    slotRowContent(s, context: context, inStateSection: inStateSection, cardLast: cardLast, showHost: showHost, pane: pane)
                        .padding(.leading, nested(level))
                        #if os(macOS)
                        // The open one shaded as a selected row: the list does not select (#2213), ↑ / ↓ go from it.
                        .background {
                            if s.key == macOpenNow {
                                RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                                    .fill(Theme.Fill.macSelected).padding(-Theme.Space.xs)
                            }
                        }
                        #endif
                        .macHoverShade(in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous), outset: Theme.Space.xs)
                }
            }
        }
            .accessibilityIdentifier(compact ? "agents.compactRow" : "agents.row")
            // BeautifulUI's 胶囊: each agent its own capsule on the page (no list card, line or system chevron); 列表 and
            // Persona' own rows: the list's card.
            .navigationLinkIndicatorVisibility(capsules ? .hidden : .automatic)
            .listRowBackground(capsules ? Color.clear : nil)
            .listRowSeparator(capsules ? .hidden : .automatic)
            .listRowInsets(capsules ? cardLast.map { EdgeInsets(top: 0, leading: Theme.Space.page, bottom: $0 ? Theme.Space.xs : 0, trailing: Theme.Space.page) }
                               ?? rowInsets(level: level)
                           : rowInsets(level: level)) // a card's rows touch (the card is one surface); capsules keep a little air between them
            .swipeActions(edge: .trailing) {
                if s.state.isLive && s.canTerminal {
                    Button { closingSession = s } label: { Label("关闭", systemImage: Theme.Icon.close) }.tint(Theme.danger)
                }
            }
            .swipeActions(edge: .leading) {
                Button { model.togglePin(s.key) } label: {
                    Label(model.isPinned(s.key) ? "取消置顶" : "置顶", systemImage: model.isPinned(s.key) ? "pin.slash.fill" : Theme.Icon.pinned)
                }
                .tint(Theme.accent)
                if (s.state == .working || s.state == .needsYou) && s.canInterrupt {
                    Button { Task { await model.interrupt(s.key) } } label: { Label("中断", systemImage: Theme.Icon.interrupt) }
                        .tint(Theme.busy)
                }
                if s.state == .unread {
                    Button { model.markSeen(s.key) } label: { Label("标为已看", systemImage: Theme.Icon.view) }
                        .tint(.gray)
                }
            }
            .contextMenu {
                AgentRowMenu(key: s.key, pinned: model.isPinned(s.key), muted: model.isMuted(s), looping: s.loop != nil,
                             live: s.state.isLive, terminal: s.canTerminal, attachable: s.canAttach, deletable: s.canDelete,
                             actions: rowActions)
                    .equatable()
            }
    }
}

/// One row of the Agents list, drawn from its agent as it is now (`AgentsFeed.Row`). It tells `reader` where it is,
/// so the row being read stays put when the list's structure changes around it.
private struct FeedRow<Content: View>: View {
    let row: AgentsFeed.Row
    let reader: ReaderAnchor
    @ViewBuilder let content: (Session, AgentTime) -> Content

    var body: some View {
        let id = row.session.id
        content(row.session, row.time)
            .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minY } action: { reader.row(id, top: $0) }
            .onDisappear { reader.gone(id) }
    }
}

/// A fold's header in the Agents list: tells `reader` where it is (as a row does, `FeedRow`) and is a target for its
/// `reveal`, so the header tapped stays put while its section opens or folds (`ReaderAnchor.keep`).
private struct FoldAnchor: ViewModifier {
    let id: String
    let reader: ReaderAnchor

    func body(content: Content) -> some View {
        content
            .id(id)
            .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minY } action: { reader.row(id, top: $0) }
            .onDisappear { reader.gone(id) }
    }
}

/// Keeps the row the reader is on where it is on screen when the Agents list's structure lands at rest (#339): a space
/// moving up above it, a card put in at the top. The list keeps its offset, so without this everything below the
/// change slid by its height, and the reader's row went off and another space took its place.
/// - At the very top nothing is kept: what came in shows there.
/// - Not while a finger or a glide moves the list: the change waits for it then (`AgentsFeed.hold`), except what is
///   critical, which may move the list (docs/ios-state.md「及时性」).
@MainActor final class ReaderAnchor {
    /// The list's window band as shown (below the bars), and whether it is at its very top; nil without a list.
    var shown: () -> (band: ClosedRange<CGFloat>, atTop: Bool)? = { nil }
    /// Scrolls the list by that many points (down for more); the list keeps it within its content.
    var scroll: (CGFloat) -> Void = { _ in }
    /// A finger or a glide is moving the list.
    var moving: () -> Bool = { false }
    /// Scrolls the list to a row with that point of it at the same point of the list (0 top, 1 bottom).
    var reveal: (String, CGFloat) -> Void = { _, _ in }
    /// Each drawn row's top in the window, as last laid out.
    private var tops: [String: CGFloat] = [:]
    private var kept: (id: String, top: CGFloat, at: CGFloat, until: Date)?
    /// The row being read since the list last rested from a scroll: kept again for the next change while it shows,
    /// so a row that drifted a little does not hand the place over to its neighbour.
    private var reading: String?

    func row(_ id: String, top: CGFloat) {
        tops[id] = top
        // Every report while it is kept: the list lays out the rows around it again a moment after the change (their
        // real heights), not only once.
        guard let k = kept, k.id == id else { return }
        guard Date.now < k.until else { kept = nil; return }
        guard abs(top - k.top) >= 0.5, !moving() else { return }
        scroll(top - k.top)
    }

    /// A row off screen. The one being kept, taken off by the change (its space went far down): brought back near
    /// where it was, then put exactly where it was once it reports where it is (`row`).
    func gone(_ id: String) {
        tops[id] = nil
        guard let k = kept, k.id == id, Date.now < k.until, !moving() else { return }
        DispatchQueue.main.async { [self] in
            guard let k = kept, k.id == id, tops[id] == nil, !moving() else { return }
            reveal(id, k.at)
        }
    }

    /// What the list shows is about to change: the row being read is the one to keep where it is.
    func willChange() {
        kept = nil
        guard !moving(), let (band, atTop) = shown(), !atTop else { return }
        let mid = (band.lowerBound + band.upperBound) / 2
        // The row being read if it shows, else the row the middle line falls in: the lowest one starting above it
        // (else the first below it).
        let inBand = tops.filter { band.contains($0.value) }
        guard let (id, top) = reading.flatMap({ r in inBand[r].map { (r, $0) } })
            ?? inBand.filter({ $0.value <= mid }).max(by: { $0.value < $1.value }) ?? inBand.min(by: { $0.value < $1.value })
        else { return }
        reading = id
        let at = (top - band.lowerBound) / (band.upperBound - band.lowerBound)
        kept = (id, top, at, .now + 1) // laid out on the next frames; a later move is not this change's
    }

    /// A fold's header was tapped: it stays where it is while its section opens or folds under it. Opening 已看 at the
    /// bottom of 按状态 (59 rows) made the list measure the rows above anew, and the header went ~3000 pt off the top
    /// (#2062).
    func keep(_ id: String) {
        kept = nil
        guard let top = tops[id], let (band, _) = shown(), band.contains(top) else { return }
        reading = id
        kept = (id, top, (top - band.lowerBound) / (band.upperBound - band.lowerBound), .now + 1)
    }

    /// A finger or a glide took over: what moves now is theirs, and what is read afterwards is wherever it stops.
    func cancel() {
        kept = nil
        reading = nil
    }
}

#if DEBUG || PERF_SIGNPOST
/// `probe.agents-list`: the rows as drawn (each follows its agent apart from the list), not the list's structure.
private struct AgentsListProbe: View {
    let feed: AgentsFeed
    let sessions: [Session]
    let asks: [Interaction]
    let page: String?

    var body: some View {
        let drawn = feed.following ? sessions.map { feed.row($0).session } : sessions
        Color.clear.allowsHitTesting(false).screenProbe("agents-list", page: page, AgentsListShown(drawn, asks: asks))
    }
}

/// What the Agents list shows (`probe.agents-list`, docs/ios-state.md「真实性」): each agent's state and last line, and
/// the questions it lists, to be checked against the hub's.
nonisolated struct AgentsListShown: Encodable, Equatable {
    var states: [String: String] = [:]
    var last: [String: String] = [:]
    var asks: [String] = []

    init() {}

    init(_ sessions: [Session], asks: [Interaction]) {
        for s in sessions {
            let k = s.machineID + "/" + s.sessionID
            states[k] = s.state.rawValue
            last[k] = s.lastText ?? ""
        }
        self.asks = asks.map(\.id).sorted()
    }
}
#endif

/// What the Agents list shows of the hub (docs/ios-state.md): the agents, the questions and the machines, passed on
/// from the model as they change, except while the list is held (`Hold`: scrolled, or out of sight); then what came
/// meanwhile lands at once when it lets go.
///
/// Its own object: the list reads only this, so holding is a flag nobody observes. A busy hub pushes the agents every
/// two seconds, and any update of a `List` mid-glide (the same rows or not) has it measure its rows anew: the content
/// jumped by their difference from its estimate. Holding by the list's own state drew it again at every scroll's start.
///
/// How long anything waits is `Hold`'s (docs/ios-state.md「及时性」); what may not wait is this feed's: what needs the
/// user, or says something is wrong, is never held: a question or approval coming or going, an agent starting or
/// stopping to need the user, one going offline, a machine going offline or failing. It lands at once, even mid-glide,
/// alone (`landCritical`): the list may still move under the finger (a card put in at the top, a space moving up),
/// which is the price of showing it now.
@MainActor @Observable final class AgentsFeed {
    /// The agents as the list's structure last changed (`shape`): the body reads this, each row its own `Row`.
    private(set) var sessions: [Session] = []
    private(set) var interactions: [Interaction] = []
    private(set) var machines: [Machine] = []
    /// Following a model: until then the list reads the model itself.
    private(set) var following = false
    /// The row being read, kept in place when what the list shows changes.
    @ObservationIgnored let reader = ReaderAnchor()
    /// Scrolled or out of sight: the list says which (`Hold.set`), the hold says when what waits lands.
    @ObservationIgnored let hold = Hold("Agents list")
    @ObservationIgnored private weak var model: AppModel?
    @ObservationIgnored private var watching = false
    /// What the rows show: the latest that landed (`sessions` may be older in what only rows show).
    @ObservationIgnored private var shown: [Session] = []
    @ObservationIgnored private var shaped = Shape()
    @ObservationIgnored private var rows: [String: Row] = [:]
    /// The list filters by the agents' last lines while it is searched: they are structure then.
    @ObservationIgnored var searching = false {
        didSet { if searching != oldValue { restructure(force: true) } }
    }
    init() {
        hold.land = { [weak self] in self?.land() }
    }

    func follow(_ model: AppModel) {
        guard self.model == nil else { return }
        self.model = model
        pull()
        following = true
    }

    /// One agent as its row shows it. The row reads this: a change only rows show (the last line, the time, a new
    /// step) draws that row again, not the list.
    @Observable final class Row {
        /// The agent as the row shows it: its `rev` and `updatedAt` are as of its last other change.
        fileprivate(set) var session: Session
        /// When it last moved, for the row's time alone: a working agent's moves on every step.
        let time: AgentTime
        init(_ session: Session) {
            self.session = session
            time = AgentTime(session.updatedAt)
        }
    }

    func row(_ s: Session) -> Row {
        if let r = rows[s.id] { return r }
        let r = Row(s)
        rows[s.id] = r
        return r
    }

    /// Copies what changed.
    private func pull() {
        hold.landed()
        guard let (s, i, m) = current() else { return }
        show(s)
        if i != interactions { reader.willChange(); interactions = i }
        if !Self.sameListing(m, machines) { reader.willChange(); machines = m }
    }

    /// The same machines as the list shows them: a machine's heartbeat (`lastSeen`, on every push of a busy hub) is
    /// shown only on the machines page, and drew the whole list again each time (#1495).
    nonisolated static func sameListing(_ a: [Machine], _ b: [Machine]) -> Bool {
        a.count == b.count && zip(a, b).allSatisfy { x, y in
            guard x.lastSeen != y.lastSeen else { return x == y }
            var y = y
            y.lastSeen = x.lastSeen
            return x == y
        }
    }

    /// The rows that changed get their agent; the list gets the new list only if its structure changed.
    private func show(_ s: [Session]) {
        guard s != shown else { return }
        shown = s
        reader.willChange() // a row above the one being read may grow or shrink (its last line), not only the structure
        for x in s {
            if let r = rows[x.id] {
                if !Session.sameApartFromTicks(r.session, x) { r.session = x }
                if r.time.updatedAt != x.updatedAt { r.time.updatedAt = x.updatedAt }
            } else {
                rows[x.id] = Row(x)
            }
        }
        if rows.count > s.count {
            let ids = Set(s.map(\.id))
            rows = rows.filter { ids.contains($0.key) }
        }
        restructure(force: false)
    }

    private func restructure(force: Bool) {
        let shape = Self.shape(shown, searching: searching)
        guard force || shape != shaped else { return }
        shaped = shape
        if shown != sessions { reader.willChange(); sessions = shown }
    }

    /// What the list's structure reads of the agents: the agents with what only rows show taken out, and the order
    /// their times put them in.
    struct Shape: Equatable {
        var sessions: [Session] = []
        /// The agents' ids, latest first by `sortDate`: what the time decides in the list (the order of rows and of
        /// spaces), without the time itself, which moves on every step of a working agent and which only rows (and a
        /// quiet space's line, from the rows') show.
        var byTime: [String] = []
    }

    /// What the list's structure reads of the agents (the groups, the order, the folds, the counts, the headers; the
    /// last lines while searching), with what only rows show taken out (docs/ios-state.md「第 3 步」).
    /// - The time: only the order it puts agents in (`Shape.byTime`); a minute passing changes nothing else.
    /// - Subagents and background work: only whether any runs (a row with one stays out of the idle fold).
    nonisolated static func shape(_ list: [Session], searching: Bool) -> Shape {
        let byTime = list.map { (id: $0.id, at: $0.sortDate) }
            .sorted { $0.at != $1.at ? $0.at > $1.at : $0.id < $1.id }
            .map(\.id)
        return Shape(sessions: list.map { s in
            var x = s
            x.rev = nil
            x.suggestion = nil
            x.ports = nil
            x.queued = nil
            x.permissionMode = nil
            x.effort = nil
            x.model = nil
            x.seenAt = nil
            if !searching { x.lastText = nil }
            x.updatedAt = nil
            x.subagents = (s.subagents ?? []).contains(where: \.running) ? [] : nil
            x.background = (s.background ?? []).isEmpty ? nil : []
            return x
        }, byTime: byTime)
    }

    /// When an agent last moved, as its row shows it (nil before the list has seen it): a quiet space's line reads
    /// its agents' times from here, not from the list's structure.
    func time(_ id: String) -> Date? { rows[id]?.time.updatedAt }

    /// The model's now, watching it for the next change if nothing is watching yet (`withObservationTracking` tells
    /// once; watched again at every change, held or not, never twice at a time).
    private func current() -> ([Session], [Interaction], [Machine])? {
        guard let model else { return nil }
        guard !watching else { return (model.allSessions, model.pendingInteractions, model.machines) }
        watching = true
        return withObservationTracking {
            (model.allSessions, model.pendingInteractions, model.machines)
        } onChange: { [weak self] in
            Task { @MainActor in // told before the change: read it after
                self?.watching = false
                self?.changed()
            }
        }
    }

    /// What was held lands now, while the list may still be held.
    private func land() {
        #if (os(iOS) || os(visionOS)) && (DEBUG || PERF_SIGNPOST)
        if hold.held { PerfScroll.mark("Agents list lands while held") }
        PerfScroll.shared.released()
        #endif
        pull()
    }

    private func changed() {
        guard hold.wait() else { return pull() }
        guard let (s, i, m) = current() else { return }
        if Self.critical(s, i, m) != Self.critical(shown, interactions, machines) { landCritical(s, i, m) }
    }

    /// While held, only what must show at once, onto the rows as shown: the states of the agents that start or stop
    /// needing the user or go offline, the questions, the machines. Last lines, times and order wait for the rest:
    /// landing all of it mid-glide changed the rows' heights under the finger (the list jumped by 100 pt and more).
    private func landCritical(_ s: [Session], _ i: [Interaction], _ m: [Machine]) {
        #if (os(iOS) || os(visionOS)) && (DEBUG || PERF_SIGNPOST)
        PerfScroll.mark("Agents list lands what is critical while held")
        PerfScroll.shared.released()
        #endif
        func urgent(_ x: Session) -> Bool { x.state == .needsYou || x.state == .offline }
        let now = Dictionary(s.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        let listed = Set(shown.map(\.id))
        var next = shown.compactMap { old -> Session? in
            guard let new = now[old.id] else { return urgent(old) ? nil : old } // gone: the rest goes when it lands
            guard new.state != old.state, urgent(new) || urgent(old) else { return old }
            var x = old
            x.state = new.state
            x.pendingInteractions = new.pendingInteractions
            return x
        }
        next += s.filter { urgent($0) && !listed.contains($0.id) }
        show(next)
        if i != interactions { reader.willChange(); interactions = i }
        if !Self.sameListing(m, machines) { reader.willChange(); machines = m }
        if next == s { hold.landed() }
    }

    /// What must show at once: who needs the user, which questions wait, what is offline or failing.
    nonisolated static func critical(_ sessions: [Session], _ interactions: [Interaction], _ machines: [Machine]) -> Set<String> {
        var out = Set<String>()
        for s in sessions where s.state == .needsYou || s.state == .offline { out.insert("\(s.state.rawValue) \(s.id)") }
        for i in interactions { out.insert("ask " + i.uid) }
        for m in machines where !m.online || !(m.error ?? "").isEmpty { out.insert("down \(m.ref.path) \(m.error ?? "")") }

        return out
    }
}

/// 全部 / 需要你 / 未读 / 工作中 under the search field, each with its count; one tap switches.
struct QuickFilterBar: View, Equatable {
    nonisolated let selection: AgentFilter
    nonisolated let counts: [AgentFilter: Int]
    var choose: (AgentFilter) -> Void

    nonisolated static func == (a: Self, b: Self) -> Bool { a.selection == b.selection && a.counts == b.counts }

    /// No sideways scroll view: the navigation stack can take one at the top of the page for the page's own and
    /// inset it by the whole top bar. It did behind a chat reopened at launch (the chips drawn ~170 pt under the
    /// search field, blurred list between: TestFlight 35 and 38), and in the split's sidebar column. Element frames
    /// do not show it (UIKit moves the content), the pixels do: `AgentsListTests.drawn`. Chips that do not fit
    /// tighten, then go to a second row.
    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("QuickFilterBar")
        #endif
        #if os(macOS)
        if !AppStyle.current.usesBeautifulUI {
            // The Mac's segmented control (#2113), every filter always there with its count: segments that came and
            // went with the counts moved under the pointer.
            Picker("筛选", selection: Binding(get: { selection }, set: { if $0 != selection { choose($0) } })) {
                ForEach(AgentFilter.allCases) { f in
                    Text(f == .all ? f.title : "\(f.title) \(counts[f] ?? 0)").tag(f)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Theme.Space.page)
            .padding(.vertical, Theme.Space.sm)
            .accessibilityIdentifier("agents.filter")
        } else {
            chipsBar
        }
        #else
        chipsBar
        #endif
    }

    private var chipsBar: some View {
        ViewThatFits(in: .horizontal) {
            if AppStyle.current.usesBeautifulUI {
                HStack(spacing: Theme.Space.xs) { chips }
                LarkFlowLayout(spacing: Theme.Space.xs, lineSpacing: Theme.Space.sm) { chips }
            } else {
                HStack(spacing: Theme.Space.s) { plainChips(padding: 12) }
                HStack(spacing: Theme.Space.sm) { plainChips(padding: 8) }
                LarkFlowLayout(spacing: 6, lineSpacing: 8) { plainChips(padding: 8) }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, Theme.Space.page)
        .padding(.vertical, Theme.Space.sm)
    }

    /// 全部, and the states that have agents right now; the chosen one stays while it empties, so it can be left.
    private var shown: [AgentFilter] {
        AgentFilter.allCases.filter { $0 == .all || $0 == selection || (counts[$0] ?? 0) > 0 }
    }

    /// Persona' own chips: the state's dot, the name and the count on a tinted capsule.
    @ViewBuilder private func plainChips(padding: CGFloat) -> some View {
        ForEach(shown) { f in
            let on = f == selection
            let tone = f.state?.tone
            let tint = tone?.text ?? Theme.accent
            Button { if !on { choose(f) } } label: {
                HStack(spacing: Theme.Space.sm) {
                    if let tone { Circle().fill(tone.dot).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS) }
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

    /// BeautifulUI's filter chips: the state's dot, the name and the count; the chosen one is the raised capsule.
    @ViewBuilder private var chips: some View {
        ForEach(shown) { f in
            let on = f == selection
            BUIFilterChip(f.title, dot: f.state?.tone.dot, count: counts[f], isActive: on) { if !on { choose(f) } }
                .accessibilityIdentifier("agents.filter." + f.rawValue)
                .accessibilityLabel(counts[f].map { "\(f.title) \($0)" } ?? f.title)
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
    @Environment(\.compactList) private var compact

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            if compact {
                // Color.primary: a section header dims hierarchical styles a level (docs/lessons/ios.md).
                Text(title).font(CompactList.sectionTitle).foregroundStyle(Color.primary).textCase(nil).lineLimit(1)
            } else {
                Text(title)
            }
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
    /// A mark before the name: a repository's folder (按仓库).
    var symbol: String? = nil
    @Environment(\.compactList) private var compact

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("SpaceHeader")
        #endif
        HStack(spacing: Theme.Space.s) {
            // 细列表's titles are big enough to tell a repository from a space: no mark (it cut a short name, "weat…").
            if let symbol, !compact {
                Image(systemName: symbol).font(Theme.Font.label).foregroundStyle(Color.secondary)
            }
            if compact {
                // 细列表: the space is the title over its rows (Color.primary: a header dims `.primary` a level).
                Text(group.name).font(CompactList.sectionTitle).foregroundStyle(Color.primary).textCase(nil).lineLimit(1)
            } else {
                Text(group.name).font(Theme.Font.sectionHeader).foregroundStyle(.secondary).textCase(nil).lineLimit(1)
            }
            let small = compact ? CompactList.meta : Theme.Font.label
            if pinned { Image(systemName: Theme.Icon.pinned).font(small).foregroundStyle(Theme.accent).accessibilityLabel("已置顶") }
            if muted { Image(systemName: Theme.Icon.muted).font(small).foregroundStyle(Color.secondary).accessibilityLabel("已屏蔽") }
            if showMachine, let machine {
                Text(machine.title).font(small).foregroundStyle(Color.secondary).textCase(nil).lineLimit(1)
            }
            Spacer(minLength: 4)
            StateCounts(group: group).opacity(muted ? 0.6 : 1)
            FoldChevron(folded: folded)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([group.name, folded ? "已折叠" : "已展开", pinned ? "已置顶" : "", muted ? "已屏蔽" : "",
                             showMachine ? machine?.title ?? "" : "", StateCounts.spoken(group)]
            .filter { !$0.isEmpty }.joined(separator: "，"))
        .accessibilityAddTraits(.isButton)
    }
}

struct StateCounts: View {
    var group: SpaceGroup
    @Environment(\.compactList) private var compact

    /// "3 个需要你，12 个工作中，35 个完成未看，4 个空闲" for VoiceOver (and the UI tests).
    static func spoken(_ group: SpaceGroup) -> String {
        let idle = group.count(.idle) + group.count(.unknown)
        return ([AgentState.needsYou, .working, .unread].compactMap { st in group.count(st) > 0 ? "\(group.count(st)) 个\(st.label)" : nil }
            + (idle > 0 ? ["\(idle) 个空闲"] : [])).joined(separator: "，")
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("StateCounts")
        #endif
        HStack(spacing: Theme.Space.sm) {
            ForEach([AgentState.needsYou, .working, .unread], id: \.self) { st in
                let n = group.count(st)
                if n > 0 {
                    HStack(spacing: Theme.Space.xs) {
                        Circle().fill(st.color).scaledFrame(width: Theme.Size.dotXS, height: Theme.Size.dotXS)
                        Text("\(n)").font((compact ? CompactList.metaStrong : Theme.Font.labelHeavy).monospacedDigit())
                            .foregroundStyle(Color.secondary)
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
                    Circle().fill(Color.secondary.opacity(0.45)).scaledFrame(width: Theme.Size.dotXS, height: Theme.Size.dotXS)
                    Text("\(idle)").font((compact ? CompactList.metaStrong : Theme.Font.labelStrong).monospacedDigit())
                        .foregroundStyle(Color.secondary)
                        .contentTransition(.numericText(value: Double(idle)))
                }
                .accessibilityLabel("\(idle) 个空闲")
            }
        }
        .lineLimit(1)
        .fixedSize()
    }
}

/// Where an agent was opened, before its title (#2526): 飞书群 (botmux; a tap opens its Feishu topic) or 终端 (herdr),
/// each on its own tint.
struct SourceLabel: View {
    var source: SessionSource
    var link: URL?
    @Environment(\.openURL) private var openURL

    var body: some View {
        let chip = Chip(text: source.label, look: .tint(source == .feishu ? Theme.sourceFeishu : Theme.sourceTerminal))
        if source == .feishu, let link {
            Button { openURL(link) } label: { chip }
                .buttonStyle(.plain)
                .accessibilityLabel("飞书群，在飞书里打开这个话题")
                .accessibilityIdentifier("agents.row.source")
        } else {
            chip.accessibilityIdentifier("agents.row.source")
        }
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
    /// The host it runs in (botmux…), named when the list mixes hosts (by todo, #1836).
    var host: String? = nil
    /// The section already says the state (grouped by state): no state word on the row.
    var hideState = false
    /// The state dot; off where something beside the row already shows the state in its color (SessionRow's avatar).
    var dot = true
    /// What the BeautifulUI style's row is drawn on.
    var surface: Surface = .none
    /// When it last moved, followed apart from the row (`AgentsFeed.Row.time`); nil: the session's own.
    var time: AgentTime? = nil
    /// Its place in a tab of several agents (#2320): named by its agent, the 发起者 marked; the tab's row above says
    /// which tab, so the row does not.
    var pane: TabPanes.Role? = nil

    enum Surface: Equatable {
        /// Bare: a List's row that has its own background and chevron.
        case none
        /// The Agents list's own capsule with its chevron.
        case capsule
        /// A row of a quiet space's card, set in a level under the card's title; `last` ends the card.
        case cardRow(last: Bool)
    }

    private func taskShape<L: View, T: View, D: View>() -> TaskRow<L, T, D>.Shape {
        switch surface {
        case .none: .plain
        case .capsule: .capsule
        case .cardRow(let last): .list(first: false, last: last)
        }
    }

    private var indented: Bool { if case .cardRow = surface { true } else { false } }

    /// BeautifulUI's task row: the state's badge, the title over the latest line, where it runs under that; when, and
    /// 需要你 as a pill, on the right.
    @MainActor
    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("SlotRow")
        #endif
        Group {
            if AppStyle.current.usesBeautifulUI {
                TaskRow(TaskItem(id: session.key.sessionID, title: session.displayTitle,
                                 subtitle: session.listPreview, footnote: meta,
                                 status: .pending(step: 0)),
                        shape: taskShape(), accessory: surface == .none ? .none : .disclosure, titleLines: 2, subtitleLines: 1) {
                    HStack(spacing: Theme.Space.sm) {
                        if dot { AgentStateBadge(state: session.state).padding(.leading, indented ? Theme.Space.l : 0) }
                        if !session.assistant, let source = session.source { SourceLabel(source: source, link: session.externalLink) }
                    }
                } trailing: {
                    VStack(alignment: .trailing, spacing: Theme.Space.xs) {
                        AgentTimeText(time: time, fallback: session.updatedAt, font: Theme.Font.label)
                        #if os(macOS)
                        // Questions and approvals waiting, a red count at the row's end, as the web's (#2695).
                        if pendingCount { CountBadge(count: session.pendingInteractions, look: .solid(Theme.needsYou)) }
                        #endif
                        if session.state == .needsYou && !muted && !hideState && !pendingCount { BUIPill(session.state.label, tone: .red) }
                        if muted {
                            Image(systemName: Theme.Icon.muted).font(Theme.Font.label).foregroundStyle(.secondary).accessibilityLabel("已屏蔽通知")
                        }
                    }
                } detail: {
                    EmptyView()
                }
            } else {
                HStack(alignment: .top, spacing: Theme.Space.m) {
                    if dot { StatusDot(state: session.state, size: 9).padding(.top, Theme.Space.sm) }
                    VStack(alignment: .leading, spacing: Theme.Space.xs) {
                        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.sm) {
                            if !session.assistant, let source = session.source { SourceLabel(source: source, link: session.externalLink) }
                            Text(session.displayTitle)
                                .font(session.state == .unread ? Theme.Font.bodyHeavy : Theme.Font.bodyStrong)
                                .lineLimit(2)
                                .fixedSize(horizontal: false, vertical: true)
                            Spacer(minLength: 4)
                            AgentTimeText(time: time, fallback: session.updatedAt, font: Theme.Font.label)
                            #if os(macOS)
                            // Questions and approvals waiting, a red count at the row's end, as the web's (#2695).
                            if pendingCount { CountBadge(count: session.pendingInteractions, look: .solid(Theme.needsYou)) }
                            #endif
                        }
                        if let last = session.listPreview {
                            Text(last).font(Theme.Font.secondary).lineHeight(.secondary).foregroundStyle(.secondary).lineLimit(2)
                        }
                        HStack(spacing: Theme.Space.xs) {
                            metaText.lineLimit(1)
                            if let todo = session.todo {
                                ViewThatFits(in: .horizontal) {
                                    Chip(text: todo.title, symbol: Theme.Icon.todo, look: .outline)
                                    Image(systemName: Theme.Icon.todo)
                                        .font(Theme.Font.caption)
                                        .foregroundStyle(.secondary)
                                        .help(todo.title)
                                }
                            }
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
            }
        }
        .opacity(muted ? 0.8 : 1) // quieter than live agents, never fainter than the idle spaces below
    }

    /// Mac: the red count of the questions and approvals waiting at the row's end, as the web's (#2695); it says 需要你
    /// on its own, so the state word and pill give way to it. Not for a muted agent (it should not catch the eye).
    private var pendingCount: Bool {
        #if os(macOS)
        session.pendingInteractions > 0 && !muted
        #else
        false
        #endif
    }

    /// Persona' own row's line: "需要你 · Claude · app › tests · MacBook": the end gives way when the line is short.
    @MainActor private var metaText: Text {
        var parts: [Text] = []
        if session.state == .needsYou && !muted && !hideState && !pendingCount {
            parts.append(Text(session.state.label).fontWeight(.semibold).foregroundStyle(session.state.tone.text))
        }
        parts.append(Text(pane?.name ?? session.sourceSubtitle ?? session.agentLabel))
        if pane?.launcher == true { parts.append(Text("发起者").fontWeight(.semibold)) }
        if let host { parts.append(Text(host)) }
        let slot = pane == nil ? session.slotLabel : nil
        if let space, !space.isEmpty {
            parts.append(Text(slot.map { space + " › " + $0 } ?? space))
        } else if let slot {
            parts.append(Text(slot))
        }
        if let machine { parts.append(Text(machine)) }
        return parts.dropFirst().reduce(parts[0]) { $0 + Text(" · ") + $1 }
    }

    /// "Claude · app › tests · MacBook": the end gives way when the line is short.
    @MainActor private var meta: String {
        var parts = [pane?.name ?? session.sourceSubtitle ?? session.agentLabel]
        if pane?.launcher == true { parts.append("发起者") }
        if let host { parts.append(host) }
        let slot = pane == nil ? session.slotLabel : nil
        if let space, !space.isEmpty {
            parts.append(slot.map { space + " › " + $0 } ?? space)
        } else if let slot {
            parts.append(slot)
        }
        if let machine { parts.append(machine) }
        if let todo = session.todo { parts.append("待办：" + todo.title) }
        return parts.joined(separator: " · ")
    }
}

/// When an agent last moved (`updatedAt`), followed apart from the rest of it: a working agent's moves on every step,
/// and only its row's time shows it (docs/ios-state.md「第 2 步」).
@Observable final class AgentTime: Equatable {
    var updatedAt: Date?
    init(_ updatedAt: Date?) { self.updatedAt = updatedAt }
    nonisolated static func == (a: AgentTime, b: AgentTime) -> Bool { a === b }
}

/// A row's 「3 分钟前」, kept current every 30 s: the one view a step of a working agent draws again (`AgentTime`).
struct AgentTimeText: View {
    let time: AgentTime?
    let fallback: Date?
    let font: Font

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AgentTimeText")
        #endif
        let at = time?.updatedAt ?? fallback
        TimelineView(.periodic(from: .now, by: 30)) { ctx in
            Text(Fmt.relative(at, now: ctx.date)).font(font).foregroundStyle(.secondary).lineLimit(1)
        }
        .fixedSize()
    }
}

/// 细列表: one line per agent — state dot, title and where (the tab, or space › tab when the section is not the space),
/// when on the right (alone there, so the times line up). `CompactList`'s scale.
nonisolated struct CompactSlotRow: View, Equatable {
    var session: Session
    var muted = false
    var space: String? = nil
    /// As SlotRow's `host`.
    var host: String? = nil
    var time: AgentTime? = nil
    /// As SlotRow's `pane`.
    var pane: TabPanes.Role? = nil

    @MainActor
    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("CompactSlotRow")
        #endif
        HStack(spacing: CompactList.gap) {
            Group {
                if AppStyle.current.usesBeautifulUI { AgentStateBadge(state: session.state).scaleEffect(0.8) } else { StatusDot(state: session.state, size: Theme.Size.dot) }
            }
            .scaledFrame(width: CompactList.mark)
            HStack(alignment: .firstTextBaseline, spacing: CompactList.gap) {
                Text(session.displayTitle)
                    .font(session.state == .unread ? CompactList.titleUnread : CompactList.title)
                    .lineLimit(1)
                    .layoutPriority(1)
                // The 发起者's mark stays whole: the line's other words give way to the title.
                if pane?.launcher == true { Text("发起者").font(CompactList.metaStrong).foregroundStyle(.secondary).lineLimit(1).fixedSize() }
                if let place, !place.isEmpty {
                    // Tail: "photo-sy…" still says where; the head cut to "…to-sync › #1".
                    Text(place).font(CompactList.meta).foregroundStyle(.secondary).lineLimit(1)
                }
                if let todo = session.todo {
                    ViewThatFits(in: .horizontal) {
                        Text(todo.title).font(CompactList.meta).foregroundStyle(Theme.accent).lineLimit(1)
                        Image(systemName: Theme.Icon.todo).font(CompactList.meta).foregroundStyle(Theme.accent).help(todo.title)
                    }
                }
            }
            Spacer(minLength: Theme.Space.xs)
            if muted { Image(systemName: Theme.Icon.muted).font(CompactList.meta).foregroundStyle(.secondary).accessibilityLabel("已屏蔽通知") }
            AgentTimeText(time: time, fallback: session.updatedAt, font: CompactList.meta.monospacedDigit())
            #if os(macOS)
            // Questions and approvals waiting, a red count at the row's end, as the web's (#2695).
            if pendingCount { CountBadge(count: session.pendingInteractions, look: .solid(Theme.needsYou)) }
            #endif
        }
        .opacity(muted ? 0.8 : 1)
    }

    /// As SlotRow's: the red count on Mac, not for a muted agent.
    private var pendingCount: Bool {
        #if os(macOS)
        session.pendingInteractions > 0 && !muted
        #else
        false
        #endif
    }

    @MainActor private var place: String? {
        let slot = pane == nil ? session.slotLabel : nil
        let location = space.flatMap { $0.isEmpty ? nil : $0 }.map { name in slot.map { name + " › " + $0 } ?? name } ?? slot
        // The 发起者's mark stands in for its agent's name (one line has no room for both; the web's phone strip does so).
        let name = pane.map { $0.launcher ? nil : $0.name } ?? session.sourceSubtitle
        let parts = [name, host, location].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
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
        // One element: on the stack alone the label went to each of its three children (read out three times, #405).
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("展开 \(sessions.count) 个空闲的 agent")
    }
}

/// A space where every agent is idle: one compact line, expandable.
struct QuietSpaceRow: View {
    var group: SpaceGroup
    var machine: Machine?
    var showMachine: Bool
    /// A repository (按仓库's 空闲的仓库), not a space: its folder.
    var repo = false
    @Environment(AgentsFeed.self) private var feed: AgentsFeed?

    var body: some View {
        HStack(spacing: Theme.Space.m) {
            Image(systemName: repo ? Theme.Icon.folder : Theme.Icon.space).foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(group.name).font(Theme.Font.heading).lineLimit(1)
                Text([showMachine ? machine?.title : nil, "\(group.sessions.count) 个 Agent",
                      group.sessions.compactMap(\.slotLabel).prefix(3).joined(separator: " · ")]
                    .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer()
            Text(Fmt.relative(group.updatedAt(feed))).font(Theme.Font.label).foregroundStyle(.secondary)
        }
    }
}

/// The capsules (the BeautifulUI style): a plain list, every row on the page's edges (the grouped list's own top
/// padding left about 100 pt under the filter chips); otherwise the grouped list.
private struct AgentsListStyle: ViewModifier {
    let capsules: Bool

    func body(content: Content) -> some View {
        if capsules { content.listStyle(.plain) } else { content.listStyle(.insetGrouped) }
    }
}

/// The capsules' plain list over the grouped page's color (the color the grouped list had); otherwise the style's page.
private struct AgentsListPage: ViewModifier {
    let capsules: Bool

    func body(content: Content) -> some View {
        if capsules { content.scrollContentBackground(.hidden).background(Theme.groupedPage) } else { content.themedPage() }
    }
}

/// A quiet space's card title (`quietSpaceCard`, the BeautifulUI style): the space, where and how many, when, and the
/// chevron that turns as it opens; the card's top when open, the whole card when closed.
struct QuietSpaceCardHead: View {
    var group: SpaceGroup
    var machine: Machine?
    var showMachine: Bool
    var open: Bool
    /// A repository (按仓库's 空闲的仓库), not a space: its folder.
    var repo = false
    @Environment(AgentsFeed.self) private var feed: AgentsFeed?

    var body: some View {
        TaskRow(TaskItem(id: group.id, title: group.name,
                         subtitle: [showMachine ? machine?.title : nil, "\(group.sessions.count) 个 Agent",
                                    group.sessions.compactMap(\.slotLabel).prefix(3).joined(separator: " · ")]
                            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "),
                         status: .pending(step: 0)),
                shape: .list(first: true, last: !open), accessory: .none, titleLines: 1, subtitleLines: 1) {
            Image(systemName: repo ? Theme.Icon.folder : Theme.Icon.space).font(Theme.Font.body).foregroundStyle(.secondary)
                .scaledFrame(width: Theme.Size.icon)
        } trailing: {
            HStack(spacing: Theme.Space.s) {
                Text(Fmt.relative(group.updatedAt(feed))).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(open ? 90 : 0))
            }
        } detail: {
            EmptyView()
        }
        .accessibilityElement(children: .combine)
        .accessibilityValue(open ? "已展开" : "已折叠")
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
            // The avatar's color is the state: no dot on its corner nor beside the title (2026-10-31, three said it once).
            SessionAvatar(machine: machine, state: session.state, dot: false)
            SlotRow(session: session, dot: false).equatable()
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
    var linkTodo: (() -> Void)? = nil

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
        if let linkTodo {
            SpaceTodoMenu(group: group, link: linkTodo)
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
    @Environment(\.compactList) private var compact

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Image(systemName: machine.symbol).foregroundStyle(machine.online ? Theme.accent : .secondary)
            if compact {
                Text(machine.title).font(CompactList.sectionTitle).foregroundStyle(Color.primary).textCase(nil).lineLimit(1)
            } else {
                Text(machine.title).font(Theme.Font.sectionHeader).foregroundStyle(.secondary).textCase(nil).lineLimit(1)
            }
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
            .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key).splitDetailPage(ownsBack: true) }
            .navigationDestination(for: SpaceRoute.self) { r in SpaceView(spaceKey: r.id).splitDetailPage() }
            .navigationDestination(for: MachineRoute.self) { r in MachineSpacesView(ref: r.ref).splitDetailPage() }
            .navigationDestination(for: MachineListRoute.self) { _ in MachinesView().splitDetailPage() }
            .navigationDestination(for: MachineDetailRoute.self) { r in MachineDetailView(ref: r.ref).splitDetailPage() }
            .navigationDestination(for: PersonalAgentsRoute.self) { _ in PersonalAgentListView().splitDetailPage() }
            .navigationDestination(for: PersonalAgentSession.self) { s in PersonalAgentSessionView(session: s).id(s.id).splitDetailPage() }
            .navigationDestination(for: TodoDetailRoute.self) { r in TodoDetailView(todoID: r.id).splitDetailPage() }
    }
}

/// A bar above a list in a stack (the split's sidebar column). Otherwise nothing: on a phone the bar is the list's
/// first row, so the folding search field leaves no gap above it.
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
            content
        }
    }
}


#if os(iOS) || os(visionOS)
/// Runs something once the system search has really closed. Closing it (`isPresented = false`) only starts the fold:
/// a push made while it still folds away is dropped by the navigation stack, which keeps the path it was given (the
/// chat in the path, the list on screen). A fixed wait from the tap was too short whenever the list took a while to
/// redraw for the closed search (SearchOpenTests, 2026-10-31).
@MainActor final class SearchCloseWatcher {
    fileprivate weak var view: UIView?

    func whenClosed(_ done: @escaping () -> Void) {
        let start = Date.now
        func check() {
            let busy = searchController.map { $0.isActive || $0.isBeingDismissed || $0.transitionCoordinator != nil } ?? false
            if !busy || Date.now.timeIntervalSince(start) > 2 {
                done()
            } else {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.03, execute: check)
            }
        }
        DispatchQueue.main.async(execute: check)
    }

    private var folded = false

    /// Starts the page with the search field folded away (2026-11-01 user request: it shows on a pull, like Mail).
    /// SwiftUI has no switch for that; the drawer (`.automatic`) folds once the list is scrolled by its height, so
    /// the list is moved down by the search bar's height once, while it still sits at its very top. Tried again
    /// for a moment until the list and the bar have been laid out; never again after (coming back from a chat
    /// keeps where the list was).
    func foldOnce() {
        guard !folded else { return }
        var tries = 0
        func attempt() {
            tries += 1
            guard !folded else { return }
            if let sc = searchController, !sc.isActive, let list = listScrollView,
               list.contentSize.height > 0, sc.searchBar.bounds.height > 0 {
                let bar = sc.searchBar.bounds.height
                // A list shorter than the screen cannot scroll, and the drawer only folds on a scroll (checked on a
                // device with three sessions: it sprang back open): give it just enough room at the end to scroll by
                // the bar's height; nothing when the list is long enough.
                let room = list.bounds.height - list.adjustedContentInset.top - list.adjustedContentInset.bottom
                let short = room + bar - list.contentSize.height
                if short > 0 { list.contentInset.bottom += short }
                let top = -list.adjustedContentInset.top
                if abs(list.contentOffset.y - top) < 1 {
                    // x kept as it is: the list's side margins live in its content offset.
                    list.setContentOffset(CGPoint(x: list.contentOffset.x, y: top + bar), animated: false)
                    settleInitialFold(list, search: sc, initialInset: -top, requestedOffset: top + bar, barHeight: bar)
                }
                folded = true
                return
            }
            if tries < 20 {
                DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(50), execute: attempt)
            }
        }
        DispatchQueue.main.async(execute: attempt)
    }

    /// UIKit shrinks the navigation inset after the drawer folds, and can add that same delta to the offset we
    /// just set (-168 -> -116 -> -64 on iOS 27). That scrolls the first row under the navigation buttons. Once the
    /// inset settles, align the first row with its new top, only if UIKit made that exact extra adjustment and no
    /// user scroll or search has taken over. Other UIKit versions that keep the requested offset need no correction.
    private func settleInitialFold(_ list: UIScrollView, search: UISearchController, initialInset: CGFloat,
                                   requestedOffset: CGFloat, barHeight: CGFloat, attempts: Int = 20) {
        guard !search.isActive, !list.isTracking, !list.isDragging, !list.isDecelerating else { return }
        let inset = list.adjustedContentInset.top
        if initialInset - inset >= barHeight - 1 {
            let adjustedOffset = requestedOffset + initialInset - inset
            if abs(list.contentOffset.y - adjustedOffset) < 1 {
                list.setContentOffset(CGPoint(x: list.contentOffset.x, y: -inset), animated: false)
                return
            }
        }
        // Inset and offset changes can arrive on separate layout turns; keep the same bounded, exact-match check.
        if attempts > 0 {
            DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(50)) {
                self.settleInitialFold(list, search: search, initialInset: initialInset,
                                       requestedOffset: requestedOffset, barHeight: barHeight, attempts: attempts - 1)
            }
        }
    }

    private weak var foundList: UIScrollView?

    /// The page came back to the front: it starts with the search folded away again, as on its first appearance, when
    /// the list still sits at its very top with the field out and not in use (#740). Scrolled down, or searching,
    /// nothing moves. A short list's added bottom room is counted in its inset already, so none is added twice.
    func refold() {
        folded = false
        foldOnce()
    }

    /// The list's scroll view: the largest collection view in the page the probe is in (List is one). Kept once found
    /// while it is in a window (`ReaderAnchor` asks on every push).
    var listScrollView: UIScrollView? {
        if let l = foundList, l.window != nil { return l }
        foundList = findListScrollView
        return foundList
    }

    private var findListScrollView: UIScrollView? {
        var r: UIResponder? = view
        while let next = r?.next {
            if let vc = next as? UIViewController {
                return Self.largestScrollView(in: vc.view)
            }
            r = next
        }
        return nil
    }

    private static func largestScrollView(in root: UIView?) -> UIScrollView? {
        guard let root else { return nil }
        var best: UIScrollView?
        var stack = [root]
        while let v = stack.popLast() {
            if let c = v as? UICollectionView, !c.isHidden,
               c.bounds.height > (best?.bounds.height ?? 0) { best = c }
            stack.append(contentsOf: v.subviews)
        }
        return best
    }

    /// The search controller SwiftUI put on the navigation item of the page the list is in.
    private var searchController: UISearchController? {
        var r: UIResponder? = view
        while let next = r?.next {
            if let vc = next as? UIViewController {
                var c: UIViewController? = vc
                while let v = c {
                    if let sc = v.navigationItem.searchController { return sc }
                    c = v.parent
                }
                return vc.navigationController?.topViewController?.navigationItem.searchController
            }
            r = next
        }
        return nil
    }
}

/// Runs something once the navigation stack the probe is in has stopped moving: no push or pop on its way, no back
/// swipe under the finger or still settling after it was let go. A path set meanwhile was taken by SwiftUI and lost by
/// the stack (#1941). Still now: at once (a launch, a notification tapped on the list), so nothing waits a turn.
@MainActor final class StackStillWatcher {
    fileprivate weak var view: UIView?
    private static let log = Logger(subsystem: "jarvis", category: "stack")

    func whenStill(_ done: @escaping () -> Void) {
        let start = Date.now
        func check() {
            if navigation?.transitionCoordinator == nil || Date.now.timeIntervalSince(start) > 2 {
                let waited = Int(Date.now.timeIntervalSince(start) * 1000)
                if waited > 0 { Self.log.notice("an open waited \(waited) ms for the stack (found: \(self.navigation != nil))") }
                done()
            } else {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.03, execute: check)
            }
        }
        check()
    }

    /// The stack the list is the root of: the navigation controller of the first view controller up from the probe.
    private var navigation: UINavigationController? {
        var r: UIResponder? = view
        while let next = r?.next {
            if let vc = next as? UIViewController { return vc.navigationController }
            r = next
        }
        return nil
    }
}

/// Where `StackStillWatcher` finds the stack from.
private struct StackStillProbe: UIViewRepresentable {
    let watcher: StackStillWatcher

    func makeUIView(context: Context) -> UIView {
        let v = UIView()
        v.isUserInteractionEnabled = false
        watcher.view = v
        return v
    }

    func updateUIView(_ uiView: UIView, context: Context) { watcher.view = uiView }
}

/// Where `SearchCloseWatcher` finds the page's search controller from.
private struct SearchCloseProbe: UIViewRepresentable {
    let watcher: SearchCloseWatcher

    func makeUIView(context: Context) -> UIView {
        let v = UIView()
        v.isUserInteractionEnabled = false
        watcher.view = v
        return v
    }

    func updateUIView(_ uiView: UIView, context: Context) { watcher.view = uiView }
}
#endif

/// The Agents page's top bar in the shell (docs/ios.md「新外壳」): the same face and switch as 助手's, in the same places,
/// over the navigation bar (kept for its search field), 新建会话 on the right; tapping the selected Agents opens the list's
/// options (分组, 显示, 机器…).
struct AgentsTopBar: View, Equatable {
    @Environment(AppModel.self) private var model
    let shell: ShellState
    var menuActions: AgentsListMenu.Actions
    var newSession: () -> Void // sets the page's state: the same each time

    /// The bar holds the list's options menu: built again with the page (a new `newSession` each time), an open menu
    /// went back to its top (see `ShellSwitch`).
    nonisolated static func == (a: Self, b: Self) -> Bool { a.shell === b.shell && a.menuActions === b.menuActions }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AgentsTopBar")
        #endif
        @Bindable var model = model
        HStack(alignment: .top, spacing: Theme.Space.s) {
            ShellPersonaButton(id: "work-sidebar-button") { shell.drawer.open() }
            Spacer(minLength: 0)
            ShellCircleButton(symbol: Theme.Icon.compose, label: "新建会话", id: "agents-new", action: newSession)
        }
        .overlay(alignment: .top) {
            ShellSwitch(tab: $model.selectedTab) { EmptyView() } workMenu: {
                AgentsListMenu(actions: menuActions, inline: true)
            }
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.top, Theme.Space.xxs)
        .shellTopBarScaled()
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("agents-top-bar") // ShellSearchTests: nothing of it over the search field
        .modifier(ShellBarAtRest(page: 1)) // ShellMovingBar stands in while the strip slides
    }
}
