import SwiftUI

/// 显示 › 字号 (⌘+ ⌘- ⌘0, over 设置 › 字号's 小 / 标准 / 大) and 前往 › 上一个 / 下一个 Agent (⌘[ ⌘]).
enum MacViewCommands {
    /// One step bigger or smaller, stopping at the ends.
    static func step(_ size: TextSize, by delta: Int) -> TextSize {
        let all = TextSize.allCases
        let i = (all.firstIndex(of: size) ?? 1) + delta
        return all[min(max(i, 0), all.count - 1)]
    }

    /// The agent above / below in the Agents list's order (AgentListOrder: needs you → unread → working → seen),
    /// wrapping around; Jarvis conversations are not in that list. From nothing, the first.
    static func neighbour(of key: SessionKey?, by delta: Int, in sessions: [Session], order: AgentListOrder) -> SessionKey? {
        let list = order.ordered(sessions.filter { !$0.assistant }).map(\.key)
        guard !list.isEmpty else { return nil }
        guard let key, let i = list.firstIndex(of: key) else { return list[0] }
        return list[((i + delta) % list.count + list.count) % list.count]
    }

    /// What 显示 › 刷新 (⌘R, the iPhone's pull to refresh) asks the hub for again, by the section on screen.
    enum RefreshTarget: Equatable { case hub, assistant, inbox }

    static func refreshTarget(_ section: MacSection) -> RefreshTarget {
        switch section {
        case .inbox: .inbox
        case .assistant: .assistant
        case .agents, .knowledge, .machines, .settings: .hub
        }
    }
}

struct MacViewMenu: Commands {
    let model: AppModel
    // The same settings as the Agents list's ⋯ menu (AgentsListMenu).
    @AppStorage("dev.grouping") private var grouping: DevGrouping = .space
    @AppStorage("dev.compact") private var compact = false
    @AppStorage(MacSection.sidebarHiddenKey) private var sidebarHidden = false

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
            // The window's sections column (MacShell); the pages' own list columns have their buttons.
            Button(sidebarHidden ? "显示侧边栏" : "隐藏侧边栏") {
                withAnimation(Theme.Motion.layout) { sidebarHidden.toggle() }
            }
            .keyboardShortcut("s", modifiers: [.command, .control])
            .disabled(!model.isPaired)
            Divider()
            Toggle("紧凑列表", isOn: $compact)
            Picker("分组方式", selection: $grouping) {
                ForEach(DevGrouping.allCases) { Text($0.title).tag($0) }
            }
            Button("全部折叠") { MacShellState.shared.agentsList?.collapse() }
                .disabled(MacShellState.shared.agentsList == nil)
            Button("全部展开") { MacShellState.shared.agentsList?.expand() }
                .disabled(MacShellState.shared.agentsList == nil)
            Divider()
            // The iPhone's pull to refresh: a Mac list has no pull, so the menu asks the hub again (收件箱's toolbar
            // button does the same for its list).
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

    private func refresh() {
        let section = MacSection.shown(override: MacShellState.shared.section, tab: model.selectedTab)
        Task {
            switch MacViewCommands.refreshTarget(section) {
            case .inbox: await InboxStore.shared.refresh()
            case .assistant: await model.refreshAssistant(); await model.refresh()
            case .hub: await model.refresh()
            }
        }
    }

    private func go(_ delta: Int) {
        let order = AgentListOrder(isPinned: { model.isPinned($0) }, isMuted: { model.isMuted($0) })
        let current = MacShellState.shared.focusedChat(model).flatMap { model.session($0)?.assistant == true ? nil : $0 }
        if let next = MacViewCommands.neighbour(of: current, by: delta, in: model.allSessions, order: order) {
            MacShellState.shared.open(next, model)
            // The chat shows in the main window: bring it up (a chat window may be the key one, or none open).
            MacShellState.shared.showMain()
        }
    }
}
