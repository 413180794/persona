import SwiftUI

/// A conversation in a window of its own (docs/mac.md「窗口」: `WindowGroup(for: SessionKey.self)`): the same chat page
/// as in Agents / Jarvis, so a few agents can sit side by side or on another screen. The window's title is the chat's.
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
                        .environment(\.splitDetail, SplitDetail(isFirstPage: true))
                }
                .navigationTitle(model.session(key)?.displayTitle ?? "对话")
            } else {
                EmptyStateView(symbol: Theme.Icon.agents, title: "没有对话", message: "这个窗口打开的对话已经不在了。")
            }
        }
        .frame(minWidth: Theme.Size.macWindowMinHeight, minHeight: Theme.Size.macWindowMinHeight)
        .macLook(model)
        .overlay(alignment: .top) { ToastOverlay() }
    }
}

/// 「在新窗口打开」 in a row's menu (the Agents list, ⌘K's results go through MacShellState).
struct MacOpenInWindowButton: View {
    @Environment(\.openWindow) private var openWindow
    var key: SessionKey

    var body: some View {
        Button { openWindow(id: MacChatWindow.id, value: key) } label: {
            Label("在新窗口打开", systemImage: Theme.Icon.newWindow)
        }
    }
}

/// Opens what the menus ask for (`MacShellState.chatWindow`): Commands have no `openWindow` of their own, the main
/// window's view does it for them.
struct MacWindowOpener: ViewModifier {
    @Environment(\.openWindow) private var openWindow

    func body(content: Content) -> some View {
        content.onChange(of: MacShellState.shared.chatWindow) { _, key in
            guard let key else { return }
            MacShellState.shared.chatWindow = nil
            openWindow(id: MacChatWindow.id, value: key)
        }
    }
}
