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
}

struct MacViewMenu: Commands {
    let model: AppModel

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
            Button("上一个 Agent") { go(-1) }
                .keyboardShortcut("[", modifiers: .command)
                .disabled(!model.isPaired)
            Button("下一个 Agent") { go(1) }
                .keyboardShortcut("]", modifiers: .command)
                .disabled(!model.isPaired)
            Divider()
        }
    }

    private func go(_ delta: Int) {
        let order = AgentListOrder(isPinned: { model.isPinned($0) }, isMuted: { model.isMuted($0) })
        let current = model.visibleSession.flatMap { model.session($0)?.assistant == true ? nil : $0 }
        if let next = MacViewCommands.neighbour(of: current, by: delta, in: model.allSessions, order: order) {
            MacShellState.shared.open(next, model)
        }
    }
}
