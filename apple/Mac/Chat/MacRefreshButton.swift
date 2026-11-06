#if os(macOS)
import SwiftUI

/// Refresh in the window's toolbar for a page iOS refreshes by pulling down (a Mac list has no pull to refresh): the
/// session's Files and Git panes. ⌘R is View › Refresh's (one shortcut for the whole app); the pane registers its load with
/// it (`macRefreshes`), so ⌘R and this button do the same.
struct MacRefreshItem: ToolbarContent {
    /// What the action reads that is not state (`macRefreshes`).
    var id: AnyHashable = 0
    let action: () async -> Void

    var body: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) { MacRefreshButton(action: action).macRefreshes(id: id, action) }
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
            Label("Refresh", systemImage: Theme.Icon.retry)
        }
        .disabled(busy)
        .help("Refresh (⌘R)")
    }
}
#endif
