import SwiftUI

/// A conversation in a window of its own (docs/mac.md「窗口」: `WindowGroup(for: SessionKey.self)`): the same chat page
/// as in Agents / Persona, so a few agents can sit side by side or on another screen. The window's title is the chat's.
struct MacChatWindow: View {
    static let id = "chat"
    @Environment(AppModel.self) private var model
    var key: SessionKey?

    var body: some View {
        Group {
            if let key {
                NavigationStack {
                    ChatView(key: key).id(key)
                        // The first page of its own window: nothing to go back to (no back arrow).
                        .environment(\.splitDetail, SplitDetail(isFirstPage: true, ownWindow: true))
                }
                .navigationTitle(model.session(key)?.displayTitle ?? String(localized: "对话"))
            } else {
                EmptyStateView(symbol: Theme.Icon.agents, title: String(localized: "没有对话"), message: String(localized: "这个窗口打开的对话已经不在了。"))
            }
        }
        .frame(minWidth: Theme.Size.macWindowMinWidth, minHeight: Theme.Size.macWindowMinHeight)
        .macLook(model)
        .overlay(alignment: .top) { ToastOverlay() }
    }
}

/// 「在新窗口打开」 in a row's menu (the Agents list, ⌘K's results go through MacShellState).
struct MacOpenInWindowButton: View {
    @Environment(\.openWindow) private var ownOpenWindow
    @Environment(\.shellOpenWindow) private var shellOpenWindow
    /// The window scene's: the page is in a hosting view of its own (`MacPageStack`), whose own one opens nothing.
    private var openWindow: OpenWindowAction { shellOpenWindow ?? ownOpenWindow }
    var key: SessionKey

    var body: some View {
        Button { openWindow(id: MacChatWindow.id, value: key) } label: {
            Label("在新窗口打开", systemImage: Theme.Icon.newWindow)
        }
    }
}

/// Opens what the menus ask for (`MacShellState.chatWindow`): Commands have no `openWindow` of their own, the menu
/// bar icon's view (always there, unlike the main window) does it for them.
struct MacWindowOpener: ViewModifier {
    @Environment(\.openWindow) private var openWindow

    func body(content: Content) -> some View {
        content.onChange(of: MacShellState.shared.chatWindow) { _, key in
            guard let key else { return }
            MacShellState.shared.chatWindow = nil
            openWindow(id: MacChatWindow.id, value: key)
        }
        .onChange(of: MacShellState.shared.terminalWindow) { _, key in
            guard let key else { return }
            MacShellState.shared.terminalWindow = nil
            openWindow(id: MacTerminalWindow.id, value: key)
        }
    }
}

/// An Agent's terminal in a window of its own (docs/mac.md「窗口」: 终端也可以单独开窗口), beside its chat or on another
/// screen. The same panel the chat's 终端 sheet shows; dragging it down does nothing here (the window closes with ⌘W).
struct MacTerminalWindow: View {
    static let id = "terminal"
    @Environment(AppModel.self) private var model
    var key: SessionKey?

    var body: some View {
        Group {
            if let key {
                TerminalPanel(key: key, switchTo: nil, grab: TerminalGrab(dragged: { _ in }, released: { _, _ in }, close: { NSApp.keyWindow?.close() }))
                    .padding(Theme.Space.xs)
                    .navigationTitle(String(localized: "终端 · \(model.session(key)?.displayTitle ?? key.sessionID)"))
            } else {
                EmptyStateView(symbol: Theme.Icon.terminal, title: String(localized: "没有终端"), message: String(localized: "这个窗口打开的 Agent 已经不在了。"))
            }
        }
        .frame(minWidth: Theme.Size.macWindowMinWidth, minHeight: Theme.Size.macWindowMinHeight)
        .macLook(model)
        .overlay(alignment: .top) { ToastOverlay() }
    }
}
