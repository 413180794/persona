#if os(macOS)
import SwiftUI

/// 刷新 ⌘R in the window's toolbar for a page iOS refreshes by pulling down (a Mac list has no pull to refresh): the
/// session's 文件 and Git panes. Only one pane is shown at a time, so ⌘R is never on two buttons (收件箱 has its own,
/// in another column of the sidebar).
struct MacRefreshItem: ToolbarContent {
    let action: () async -> Void

    var body: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) { MacRefreshButton(action: action) }
    }
}

private struct MacRefreshButton: View {
    let action: () async -> Void
    @State private var busy = false

    var body: some View {
        Button {
            Task {
                busy = true
                await action()
                busy = false
            }
        } label: {
            Label("刷新", systemImage: Theme.Icon.retry)
        }
        .keyboardShortcut("r", modifiers: .command)
        .disabled(busy)
        .help("刷新（⌘R）")
    }
}
#endif
