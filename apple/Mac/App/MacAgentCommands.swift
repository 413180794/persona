import AppKit
import SwiftUI

/// The Agent menu (docs/mac.md「菜单和快捷键」): what the key window's chat (`MacShellState.focusedChat`) can be told
/// from the keyboard. Questions and terminal dialogs need their choices, a plan sent back needs words: those stay in
/// the chat's card; the menu says yes to a tool or a plan, no to a tool.
enum MacAgentCommands {
    /// ⇧⌘⏎: allow the tool, or approve the plan with edits auto-accepted (commands still ask; the same as the
    /// notification's 批准 — 批准并自动执行 stays a choice in the card).
    static func approve(_ ix: Interaction) -> Resolution? {
        switch ix.kind {
        case .permission: Resolution(allow: true)
        case .plan: Resolution(approve: true, mode: "acceptEdits")
        case .question, .screen: nil
        }
    }

    /// ⇧⌘⌫: deny the tool.
    static func reject(_ ix: Interaction) -> Resolution? {
        ix.kind == .permission ? Resolution(allow: false) : nil
    }

    /// ⌃⌘N: the next agent or Jarvis conversation that needs you, longest waiting first, wrapping around.
    static func nextNeedsYou(after current: SessionKey?, in sessions: [Session], muted: (Session) -> Bool) -> SessionKey? {
        let order = sessions.filter { $0.state == .needsYou && !muted($0) }
            .sorted { ($0.stateSince ?? .distantPast) < ($1.stateSince ?? .distantPast) }
            .map(\.key)
        guard !order.isEmpty else { return nil }
        guard let current, let i = order.firstIndex(of: current) else { return order[0] }
        return order[(i + 1) % order.count]
    }
}

/// The menu itself.
struct MacAgentMenu: Commands {
    let model: AppModel

    var body: some Commands {
        CommandMenu("Agent") {
            let key = MacShellState.shared.focusedChat(model)
            let ix = key.flatMap { model.interactions(for: $0).first }
            Button("中断") { if let key { Task { await model.interrupt(key) } } }
                .keyboardShortcut(".", modifiers: .command)
                .disabled(key == nil || model.session(key!)?.state != .working)
            Button("批准当前待处理") {
                if let ix, let r = MacAgentCommands.approve(ix) { Task { await model.resolve(ix, r) } }
            }
            // ⇧⌘⏎: ⌘⏎ is the composer's send (ComposerParts).
            .keyboardShortcut(.return, modifiers: [.command, .shift])
            .disabled(ix.flatMap(MacAgentCommands.approve) == nil)
            // ⇧⌘⌫, not the docs' first idea ⌘⌫: that one deletes to the line's start while typing in the composer,
            // and a menu shortcut would take it.
            Button("拒绝") {
                if let ix, let r = MacAgentCommands.reject(ix) { Task { await model.resolve(ix, r) } }
            }
            .keyboardShortcut(.delete, modifiers: [.command, .shift])
            .disabled(ix.flatMap(MacAgentCommands.reject) == nil)
            Divider()
            Button("下一个需要你的") {
                if let next = MacAgentCommands.nextNeedsYou(after: key, in: model.allSessions, muted: { model.isMuted($0) }) {
                    MacShellState.shared.open(next, model)
                    // The chat shows in the main window: bring it up (a chat window may be the key one, or none open).
                    MacShellState.shared.showMain()
                }
            }
            .keyboardShortcut("n", modifiers: [.command, .control]) // ⌥⌘N is 收件箱's 写邮件
            .disabled(!model.allSessions.contains { $0.state == .needsYou && !model.isMuted($0) })
            Button("在新窗口打开") { if let key { MacShellState.shared.chatWindow = key } }
                .disabled(key == nil)
            // Only an Agent in a herdr pane has a terminal (the Jarvis conversation runs headless).
            Button("在新窗口打开终端") { if let key { MacShellState.shared.terminalWindow = key } }
                .disabled(key.flatMap { model.session($0) }.map { $0.assistant || $0.paneID == nil } ?? true)
            // The row menu's 交给 Jarvis: the Agent's state and last answer go to the main conversation (AgentHandoff).
            Button("交给 Jarvis") {
                if let key, let s = model.session(key) {
                    MacShellState.shared.willOpen() // 知识库 / 机器 make way for the Jarvis page
                    Task { await model.askJarvis(AgentHandoff.prompt(s, machine: model.machine(s.machineID)?.name)) }
                }
            }
            .keyboardShortcut("j", modifiers: .command)
            .disabled(key.flatMap { model.session($0) } == nil || key.flatMap { model.session($0)?.assistant } == true)
            Button("显示会话信息") { ask(.inspector) }
                .keyboardShortcut("i", modifiers: [.command, .option])
                .disabled(key == nil)
            Divider()
            // The chat page's own dialogs (ChatView's ⋯ menu, the composer's /clear), so only for an Agent's chat.
            let agent = key.flatMap { model.session($0) }.flatMap { $0.assistant ? nil : $0 }
            Button("重命名…") { ask(.rename) }
                .disabled(agent == nil)
            Button("清空上下文…") { ask(.clear) }
                .disabled(agent.map { $0.paneID == nil || $0.isCodex || $0.isTraex || $0.state == .working } ?? true)
            Button("关闭 Agent…") { ask(.close) }
                .disabled(agent?.status.isLive != true)
            Divider()
            Button("标为已读") { if let key { model.markSeen(key) } }
                .disabled(key == nil || model.session(key!)?.state != .unread)
            Button(key.map { model.isPinned($0) } == true ? "取消置顶" : "置顶") { if let key { model.togglePin(key) } }
                .disabled(key == nil)
            Button(key.map { model.isMuted($0) } == true ? "开启通知" : "屏蔽") { if let key { model.toggleMute(key) } }
                .disabled(key == nil)
            Divider()
            Button("拷贝会话 ID") { if let key { copy(key.sessionID, "已拷贝会话 ID") } }
                .disabled(key == nil)
            Button("拷贝恢复命令") {
                if let key, let s = model.session(key) {
                    copy(MacAgentCommands.resumeLine(cwd: s.cwd, command: s.resumeCommand, id: key.sessionID), "已拷贝恢复命令")
                }
            }
            .disabled(key == nil)
        }
    }

    private func ask(_ kind: MacShellState.ChatRequest.Kind) {
        let shell = MacShellState.shared
        if let key = shell.focusedChat(model) { shell.chatRequest = .init(key: key, kind: kind, window: shell.keyWindow) }
    }

    private func copy(_ text: String, _ toast: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        model.toast = Toast(toast, style: .success)
    }
}

extension MacAgentCommands {
    /// `cd '<folder>' && claude --resume <id>`: the folder single-quoted, so a space, `;` or `$(…)` in it stays a path.
    static func resumeLine(cwd: String, command: String, id: String) -> String {
        "cd '" + cwd.replacingOccurrences(of: "'", with: #"'\''"#) + "' && \(command) \(id)"
    }
}
