import SwiftUI

/// 显示 › 字号 (⌘+ ⌘- ⌘0, over 设置 › 字号's 小 / 标准 / 大) and 前往 › 上一个 / 下一个 Agent (⌘[ ⌘]).
enum MacViewCommands {
    /// One step bigger or smaller, stopping at the ends.
    static func step(_ size: TextSize, by delta: Int) -> TextSize {
        let all = TextSize.allCases
        let i = (all.firstIndex(of: size) ?? 1) + delta
        return all[min(max(i, 0), all.count - 1)]
    }

    /// The agent above / below in the Agents list's order (AgentListOrder: needs you → unread → working → seen, or by
    /// time alone under 最近更新),
    /// wrapping around; Persona conversations are not in that list. From nothing, the first.
    static func neighbour(of key: SessionKey?, by delta: Int, in sessions: [Session], order: AgentListOrder) -> SessionKey? {
        let list = order.ordered(sessions.filter { !$0.assistant }).map(\.key)
        guard !list.isEmpty else { return nil }
        guard let key, let i = list.firstIndex(of: key) else { return list[0] }
        return list[((i + delta) % list.count + list.count) % list.count]
    }

    /// What 显示 › 刷新 (⌘R, the iPhone's pull to refresh) loads again: the key window's page when it loads its own
    /// data (`macRefreshes`: 知识库, 机器, a chat's 文件 / Git pane), else what the section shows from the hub.
    enum RefreshTarget: Equatable { case page, hub, assistant, inbox, todos }

    static func refreshTarget(_ section: MacSection, page: Bool, mainWindow: Bool) -> RefreshTarget {
        if page { return .page }
        // The section is the main window's: with a chat window, a sheet or 设置 in front, only the hub's lists.
        guard mainWindow else { return .hub }
        return switch section {
        case .inbox: .inbox
        case .todos: .todos
        case .assistant: .assistant
        case .agents, .messages, .knowledge, .machines, .settings: .hub
        }
    }
}

struct MacViewMenu: Commands {
    let model: AppModel
    // The same settings as the Agents list's ⋯ menu (AgentsListMenu).
    @StoredValue("dev.grouping") private var grouping: DevGrouping = .standard
    @StoredValue("dev.sort") private var sort: AgentSort = .state
    @StoredValue("dev.compact") private var compact = false
    @StoredValue(MacSection.sidebarHiddenKey) private var sidebarHidden = false

    var body: some Commands {
        CommandGroup(after: .toolbar) {
            Divider()
            Button("放大字号") { model.textSize = MacViewCommands.step(model.textSize, by: 1) }
                .keyboardShortcut("+", modifiers: .command)
                .disabled(model.textSize == TextSize.allCases.last)
            Button("缩小字号") { model.textSize = MacViewCommands.step(model.textSize, by: -1) }
                .keyboardShortcut("-", modifiers: .command)
                .disabled(model.textSize == TextSize.allCases.first)
            Button("标准字号") { model.textSize = .standard }
                .keyboardShortcut("0", modifiers: .command)
                .disabled(model.textSize == .standard)
            Divider()
        }
        CommandGroup(after: .sidebar) {
            // The window's sections column (MacShell), as the toolbar button beside the traffic lights does; the pages'
            // list columns have no button on the Mac (SplitColumns).
            Button(sidebarHidden ? "显示侧边栏" : "隐藏侧边栏") {
                withAnimation(Theme.Motion.layout) { sidebarHidden.toggle() }
            }
            .keyboardShortcut("s", modifiers: [.command, .control])
            .disabled(!model.isPaired)
            Divider()
            Toggle("紧凑列表", isOn: $compact)
            // the grouping in use: a stored 按待办 is 按项目 on a hub without todos
            Picker("分组方式", selection: Binding(get: { DevGrouping.resolved(grouping, supportsTodos: TodoStore.shared.isSupported) },
                                                set: { grouping = $0 })) {
                ForEach(DevGrouping.offered(supportsTodos: TodoStore.shared.isSupported)) { Text($0.title).tag($0) }
            }
            Picker("排序方式", selection: $sort) {
                ForEach(AgentSort.allCases) { Text($0.title).tag($0) }
            }
            .disabled(grouping == .state) // sectioned by state; the rows in a section are by time already
            // The Agents list is kept behind other sections (`MacPageStack`): only while it shows.
            Button("全部折叠") { MacShellState.shared.agentsList?.collapse() }
                .disabled(MacShellState.shared.agentsList == nil || !agentsShown)
            Button("全部展开") { MacShellState.shared.agentsList?.expand() }
                .disabled(MacShellState.shared.agentsList == nil || !agentsShown)
            Divider()
            // The iPhone's pull to refresh: a Mac list has no pull. The one ⌘R in the app (the toolbars' 刷新 buttons
            // have none, so two never claim it).
            Button("刷新") { refresh() }
                .keyboardShortcut("r", modifiers: .command)
                .disabled(!model.isPaired)
            Divider()
            Button("上一个 Agent") { go(-1) }
                .keyboardShortcut("[", modifiers: .command)
                .disabled(!model.isPaired)
            Button("下一个 Agent") { go(1) }
                .keyboardShortcut("]", modifiers: .command)
                .disabled(!model.isPaired)
            Divider()
        }
    }

    private var agentsShown: Bool { MacSection.shown(override: MacShellState.shared.section, tab: model.selectedTab) == .agents }

    private func refresh() {
        let section = MacSection.shown(override: MacShellState.shared.section, tab: model.selectedTab)
        let key = NSApp.keyWindow
        let page = MacPageRefreshRegistry.shared.action(window: key?.windowNumber)
        let main = key?.identifier?.rawValue.hasPrefix("main") == true
        Task {
            switch MacViewCommands.refreshTarget(section, page: page != nil, mainWindow: main) {
            case .page: await page?()
            case .inbox: await InboxStore.shared.refresh()
            case .todos: await TodoStore.shared.refresh()
            case .assistant: await model.refreshAssistant(); await model.refresh()
            case .hub: await model.refresh()
            }
        }
    }

    private func go(_ delta: Int) {
        let order = AgentListOrder(isPinned: { model.isPinned($0) }, isMuted: { model.isMuted($0) },
                                   byRank: sort == .state || grouping == .state)
        let current = MacShellState.shared.focusedChat(model).flatMap { model.session($0)?.assistant == true ? nil : $0 }
        if let next = MacViewCommands.neighbour(of: current, by: delta, in: model.sessionsNow, order: order) {
            MacShellState.shared.open(next, model)
            // The chat shows in the main window: bring it up (a chat window may be the key one, or none open).
            MacShellState.shared.showMain()
        }
    }
}
