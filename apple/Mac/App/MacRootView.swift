import BeautifulUI
import SwiftUI

/// The window's sidebar sections: the iPhone's four tabs, plus 知识库 and 机器 (on the phone a page inside
/// Persona, and 机器 inside Agents / 设置), which a Mac with room for a sidebar shows as sections of their own
/// (docs/mac.md「主窗口」). ⌘1…⌘7: see `shortcutOrder`.
enum MacSection: String, CaseIterable, Identifiable {
    case assistant, agents, todos, inbox, knowledge, machines, settings

    var id: String { rawValue }

    /// The sections in ⌘ order. ⌘1…⌘6 stay where they were; 「待办」 (added later) takes ⌘7. The sidebar shows `allCases`
    /// (「待办」 after Agents), so the two orders are kept apart.
    static let shortcutOrder: [MacSection] = [.assistant, .agents, .inbox, .knowledge, .machines, .settings, .todos]

    /// The ⌘ number of the section, 1-based.
    var shortcut: Int? { MacSection.shortcutOrder.firstIndex(of: self).map { $0 + 1 } }

    /// The sections the window shows: 「待办」 only when the hub has todos (an old hub answers 404, #397).
    static func visible(supportsTodos: Bool) -> [MacSection] { allCases.filter { supportsTodos || $0 != .todos } }

    /// The iPhone tab it is; nil for the Mac's own sections.
    var tab: AppTab? {
        switch self {
        case .assistant: .assistant
        case .agents: .chats
        case .inbox: .mail
        case .settings: .settings
        case .todos, .knowledge, .machines: nil
        }
    }

    /// The section on screen: one of the Mac's own while picked, else the tab's (a notification, ⌘K or a link that
    /// moves the tab moves the section with it, MacShell clears the pick).
    static func shown(override: MacSection?, tab: AppTab) -> MacSection { override ?? MacSection(tab) }

    /// What picking a section changes: the tab, or the Mac-side pick.
    static func pick(_ s: MacSection) -> (tab: AppTab?, override: MacSection?) {
        if let tab = s.tab { return (tab, nil) }
        return (nil, s)
    }

    /// The Mac-side pick across launches (the tabs are AppModel's to remember).
    static let storageKey = "mac.section"
    /// 显示 › 隐藏侧边栏 (⌃⌘S): the sections column folded away, across launches.
    static let sidebarHiddenKey = "mac.sidebar.hidden"
    static func remember(_ s: MacSection?, in d: UserDefaults = .standard) {
        if let s, s.tab == nil { d.set(s.rawValue, forKey: storageKey) } else { d.removeObject(forKey: storageKey) }
    }
    static func remembered(in d: UserDefaults = .standard) -> MacSection? {
        d.string(forKey: storageKey).flatMap(MacSection.init(rawValue:)).flatMap { $0.tab == nil ? $0 : nil }
    }

    init(_ tab: AppTab) {
        switch tab {
        case .assistant: self = .assistant
        case .chats: self = .agents
        case .mail: self = .inbox
        case .settings: self = .settings
        }
    }

    var title: String {
        switch self {
        case .assistant: "Persona"
        case .agents: "Agents"
        case .todos: "待办"
        case .inbox: "收件箱"
        case .knowledge: "知识库"
        case .machines: "机器"
        case .settings: "设置"
        }
    }

    var icon: String {
        switch self {
        case .assistant: Theme.Icon.ai
        case .agents: Theme.Icon.agents
        case .todos: Theme.Icon.todo
        case .inbox: Theme.Icon.inbox
        case .knowledge: Theme.Icon.knowledge
        case .machines: Theme.Icon.machines
        case .settings: Theme.Icon.settings
        }
    }
}

/// The window: unpaired, the welcome page (pairing, 体验模式); paired, a sidebar with the four sections beside the
/// section's page (the iOS pages, `horizontalSizeClass` regular).
struct MacRootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("MacRootView")
        #endif
        Group {
            if let ref = model.pairingEngineMachine {
                PairingEngineView(ref: ref)
            } else if model.isPaired {
                MacShell()
            } else {
                WelcomeView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(minWidth: Theme.Size.macWindowMinWidth, minHeight: Theme.Size.macWindowMinHeight)
        .macLook(model)
        .overlay(alignment: .top) { ToastOverlay() }
        .alert("Claude 想在这台 Mac 上执行", isPresented: Binding(
            get: { model.pendingConfirmation != nil },
            set: { if !$0 { model.pendingConfirmation?.respond(false); model.pendingConfirmation = nil } }
        ), presenting: model.pendingConfirmation) { c in
            Button("允许") { c.respond(true); model.pendingConfirmation = nil }
            Button("拒绝", role: .cancel) { c.respond(false); model.pendingConfirmation = nil }
        } message: { c in
            Text(c.summary)
        }
        .sendRefusalAlert(model)
        .overlay { if model.isPaired { MacQuickOpenLayer() } }
        .sheet(isPresented: Binding(get: { MacShellState.shared.newAgent }, set: { MacShellState.shared.newAgent = $0 })) {
            // 新建 Agent (⌘N, the menu bar, ⌘K): the iOS form in a window sheet; the new agent opens in Agents.
            NavigationStack {
                NewChatView { session in
                    MacShellState.shared.newAgent = false
                    MacShellState.shared.open(session.key, model)
                }
            }
            .macSheet()
            .environment(model)
        }
        #if DEBUG
        .preferredColorScheme(MacSnapshot.colorScheme)
        #endif
    }
}

extension View {
    /// What RootView sets on the whole tree on iOS: the spec's tint, font design and theme, the text size, and a
    /// rebuild when the style, color or persona changes (tokens are static). The text size rebuilds it too on the
    /// Mac: the fonts read `Theme.Font.scale`, which SwiftUI does not watch (#833).
    func macLook(_ model: AppModel) -> some View {
        id(model.look)
            .id(model.textSize)
            .tint(Theme.accent)
            .fontDesign(model.style.fontDesign)
            .dynamicTypeSize(model.textSize.applied(to: .large))
            .buiTheme(.jarvis())
            .scrollIndicators(.automatic)
            // Every iOS Form in the window grouped (System Settings' look; a sheet is its own window and needs
            // `macSheet()`, 2026-11-02): the Mac's default two-column
            // form put the iPhone's section rows (pickers, editors) in the label column and cut them off.
            .formStyle(.grouped)
    }
}

/// The sections column and the section's page side by side. Not a `NavigationSplitView`: the pages are split views
/// themselves (Agents, 收件箱, 设置 in regular width; Persona lays its list beside it, `MacListBeside`), and a split nested in another one's detail column laid its
/// own detail out as if the outer sidebar were still in front of it (the chat sat a sidebar's width to the right,
/// its composer cut off). The page's split is the window's; this column sits at its leading edge.
private struct MacShell: View {
    @Environment(AppModel.self) private var model
    private var shell: MacShellState { .shared }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("MacShell")
        #endif
        HStack(spacing: 0) {
            // 显示 › 隐藏侧边栏 folds it away; ⌘1…6 still move between the sections.
            // The divider runs up into the title bar's row too: stopping under it, the page's white showed past the
            // column's edge there, a step the user saw as a white bump (#830).
            if !sidebarHidden { sections; Divider().ignoresSafeArea(.container, edges: .top) }
            page
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                // The section's title in the title bar's row, at this column's edge (not over the sections column):
                // the row was left empty when the system's title went (#830). Persona's page puts its own header there.
                // A pushed page that draws its own title there (`pageTitle`, #855) takes the row instead.
                .overlayPreferenceValue(MacTitleRowTaken.self, alignment: .topLeading) { taken in
                    if shown != .assistant || pluginPlace != nil, !taken {
                        MacPageTitle(title: pluginPlace?.title ?? shown.title, leading: sidebarHidden ? Theme.Size.macTitleAfterControls : Theme.Space.l)
                    }
                }
                // Pages that draw their own header in that row (Persona's) start it here.
                .environment(\.macTitleRowLeading, sidebarHidden ? Theme.Size.macTitleAfterControls : 0)
                // The connection state lives at the sidebar's foot; folded away, it comes to the page's (only while
                // the hub is out of reach: 知识库 and 机器 have no title spot of their own that says so).
                .safeAreaInset(edge: .bottom, spacing: 0) {
                    if sidebarHidden { MacConnectionFooter().background(Theme.systemSidebar) }
                }
        }
        // No toolbar band across the window: the sections column reaches up behind the traffic lights, and the bar's
        // backdrop and hairline cut it off at the toolbar (2026-11-02).
        .toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
        // The plugins' sidebar entries and cards (docs/plugins.md「UI 插槽」): with a hub that has them.
        .task(id: PluginSlots.source(model)) { await PluginSlots.shared.reload(for: PluginSlots.source(model).hub) }
        // A page whose plugin took it away (disabled, uninstalled) goes back to the section.
        .onChange(of: PluginSlots.shared.pages) { if shell.pluginPage != nil, pluginPlace == nil { shell.pluginPage = nil } }
        // No page title in the bar: the system puts it after the items beside the traffic lights, over this column, and
        // a long one ran across the column's edge on a white backing (2026-11-02 user: 文字都超出去了). The selected
        // section and the selected row say where you are; the window keeps its title for the Window menu.
        .toolbar(removing: .title)
        // Next to the traffic lights, over the sections column: shows or hides that column (显示 › 显示/隐藏侧边栏,
        // ⌃⌘S). The pages' lists have no button of their own: one there sat over this column and hid the list.
        .toolbar {
            ToolbarItemGroup(placement: .navigation) {
                Button { withAnimation(Theme.Motion.layout) { sidebarHidden.toggle() } } label: {
                    Label(sidebarHidden ? "显示侧边栏" : "隐藏侧边栏", systemImage: Theme.Icon.sidebar)
                }
                .help(sidebarHidden ? "显示侧边栏" : "隐藏侧边栏")
                // The Agents list's menu (filter, grouping, folding) right after it, in this group. As the list's own
                // `.navigation` item it jumped in front of this button whenever a page in the detail column brought an
                // item of its own (a space's ⋯, a machine's ⓘ): the window's toolbar is rebuilt then, list items first.
                // Only while Agents shows.
                if shown == .agents, let actions = shell.agentsList {
                    AgentsListMenu(actions: actions)
                }
            }
            // A plain glyph beside the traffic lights: in the system's glass circle it stood out of the sections column as
            // a white bump (2026-11-03 user).
            .sharedBackgroundVisibility(.hidden)
            // What the title's room did: the pages' actions stay at the trailing end, by the search field (without
            // it they lined up at the leading edge, over this column).
            ToolbarSpacer(.flexible)
        }
        // Something moved the tab (a notification, ⌘K, a link): its section comes to the front.
        .onChange(of: model.selectedTab) { shell.section = nil; shell.pluginPage = nil }
        .onChange(of: model.openChat) { if model.openChat != nil { shell.section = nil; shell.pluginPage = nil } }
        .onChange(of: TodoStore.shared.isSupported) { _, supported in
            // An old hub has no todos: a window still on the todos section (or remembering it) goes back to the tab's.
            if !supported, shell.section == .todos {
                shell.section = nil
                MacSection.remember(nil)
            }
        }
        .onChange(of: model.assistantCurrent) { shell.section = nil; shell.pluginPage = nil }
        // Let go of the Agents list's menu actions with the section: their closures hold the list's state, and its
        // AgentsFeed went on copying and sorting every agent on each push from every other section (#1495).
        .onChange(of: shown) { _, now in if now != .agents { shell.agentsList = nil } }
    }

    @StoredValue(MacSection.sidebarHiddenKey) private var sidebarHidden = false

    /// The section on screen: the one picked in the column, else the tab's.
    private var shown: MacSection { MacSection.shown(override: shell.section, tab: model.selectedTab) }

    /// The sections, with the rows the Persona page's own list draws (`AssistantSidebar`): the same icon box, type and
    /// rounded selection (2026-11-02 user: the system's source list beside it looked unstyled). Flush with the window's
    /// edges, up behind the traffic lights; inset from them it looked like a card on a backing board (same day).
    private var sections: some View {
        return VStack(spacing: Theme.Space.xxs) {
            ForEach(MacSection.visible(supportsTodos: TodoStore.shared.isSupported)) { section in
                // The plugins' pages go with 机器 and 设置, right before 设置 (docs/plugins.md「UI 插槽」).
                if section == .settings { pluginRows }
                MacSidebarRow(icon: section.icon, title: section.title, selected: section == shown && shell.pluginPage == nil,
                              badge: badge(section), identifier: "mac.section.\(section.rawValue)") { shell.go(section, model) }
            }
            Spacer(minLength: 0)
            // The sidebar's foot: while the hub is out of reach, 等待网络 / 连接中… (a click explains), as the
            // iPhone's title spot does (docs/mac.md「主窗口」).
            MacConnectionFooter()
        }
        // Below the traffic lights at a fixed height: the title bar is taller on pages with toolbar items, and rows
        // that followed it jumped up and down between sections.
        .padding(.top, Theme.Size.macToolbarHeight)
        .padding(.horizontal, Theme.Space.s)
        .padding(.bottom, Theme.Space.s)
        .frame(width: Theme.Size.macSidebarIdeal)
        .frame(maxHeight: .infinity, alignment: .top)
        .ignoresSafeArea(.container, edges: .top)
        .background(Theme.systemSidebar)
        .transition(.move(edge: .leading))
    }

    /// The plugins' main-navigation entries (nav_page): icon, title on one line, the plugin's badge.
    private var pluginRows: some View {
        ForEach(PluginSlots.shared.pages) { p in
            let key = PluginSlots.key(p)
            MacSidebarRow(icon: PluginSlots.icon(p.icon), title: p.title, selected: shell.pluginPage == key,
                          badge: p.badge.map { Int($0.text).map(MacSidebarRow.Badge.count) ?? .word($0.text) },
                          identifier: "mac.plugin.\(key)") { shell.pluginPage = key }
        }
    }

    /// The plugin page picked in the sidebar, while its plugin still has it.
    private var pluginPlace: PluginUIPlace? {
        shell.pluginPage.flatMap { k in PluginSlots.shared.pages.first { PluginSlots.key($0) == k } }
    }

    /// The iOS tab bar's badges (`RootView`).
    private func badge(_ section: MacSection) -> MacSidebarRow.Badge? {
        let n: Int
        switch section {
        case .knowledge, .machines: return nil
        case .todos: n = TodoStore.shared.openCount
        case .assistant: n = model.badges.assistant
        case .agents: n = model.badges.agents
        case .inbox: n = InboxStore.shared.actionCount
        case .settings: return model.isDemo ? .word("体验") : nil
        }
        return n > 0 ? .count(n) : nil
    }

    @ViewBuilder private var page: some View {
        if let p = pluginPlace {
            NavigationStack {
                PluginPageView(plugin: p.plugin, ui: p.id, title: p.title)
                    .environment(\.macBelowTitleRow, true)
            }
            .id(PluginSlots.key(p))
        } else {
            sectionPage
        }
    }

    @ViewBuilder private var sectionPage: some View {
        switch shown {
        case .assistant: AssistantView() // its own headers are the title bar's row (#830, `AssistantView`)
        case .agents: ChatsView()
        case .todos: MacTodosPage()
        case .inbox: MacInboxPage()
        case .knowledge: NavigationStack { KnowledgeView().environment(\.macBelowTitleRow, true) } // below the title (#1243)
        case .machines: MacMachinesPage()
        case .settings: SettingsView()
        }
    }
}

extension EnvironmentValues {
    /// Where a page's own header in the title bar's row may start (#830): past the traffic lights and the toolbar's
    /// buttons when the sections column is hidden and the page is at the window's edge, else 0.
    @Entry var macTitleRowLeading: CGFloat = 0
}

/// A section's title in the title bar's row, over the page's leading edge (#830): a label, so the row still drags the
/// window.
private struct MacPageTitle: View {
    let title: String
    let leading: CGFloat

    var body: some View {
        Text(title)
            .font(Theme.Font.title)
            .foregroundStyle(Theme.macLabel)
            .lineLimit(1)
            .padding(.leading, leading)
            .frame(height: Theme.Size.macToolbarHeight)
            .ignoresSafeArea(.container, edges: .top)
            .allowsHitTesting(false)
            .accessibilityAddTraits(.isHeader)
    }
}

/// The sidebar's 机器: the machines page, pushing a machine's page; its agents open their chats here too
/// (SessionKey), as on the phone.
struct MacMachinesPage: View {
    @State private var path = NavigationPath()

    var body: some View {
        NavigationStack(path: $path) {
            MachinesView()
                .environment(\.macBelowTitleRow, true) // below the section's title, not scrolling through it (#1243)
                .navigationDestination(for: MachineDetailRoute.self) { r in MachineDetailView(ref: r.ref) }
                .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key) }
        }
        #if DEBUG
        // `-macSection machines -openMonitor <machine id>` (or `-openMachineDetail <id>`): that machine's page.
        .onAppear {
            guard path.isEmpty else { return }
            let d = UserDefaults.standard
            if let m = d.string(forKey: "openMonitor"), m != "all" { path.append(MachineDetailRoute(id: m)) }
            else if let m = d.string(forKey: "openMachineDetail") { path.append(MachineDetailRoute(id: m)) }
        }
        #endif
    }
}

/// The connection state at the sidebar's foot; nothing while connected.
struct MacConnectionFooter: View {
    @Environment(AppModel.self) private var model
    @State private var details = false

    var body: some View {
        let phase = model.connection.shown
        if phase != .online {
            Button { details = true } label: {
                ConnectionLabel(phase: phase, compact: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, Theme.Space.l)
                    .padding(.vertical, Theme.Space.s)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .help("查看连接详情")
            .accessibilityLabel("\(phase.title)，查看连接详情")
            .connectionDetails($details)
        }
    }
}
