#if os(macOS)
import SwiftUI

/// 刷新 in the window's toolbar for a page iOS refreshes by pulling down (a Mac list has no pull to refresh): the
/// session's 文件 and Git panes. ⌘R is 显示 › 刷新's (one shortcut for the whole app); the pane registers its load with
/// it (`macRefreshes`), so ⌘R and this button do the same.
struct MacRefreshItem: ToolbarContent {
    let action: () async -> Void

    var body: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) { MacRefreshButton(action: action).macRefreshes(action) }
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
        .disabled(busy)
        .help("刷新（⌘R）")
    }
}
#endif
