import SwiftUI

/// The Mac's `SearchCloseWatcher` (iOS: `Views/Chats/ChatsView.swift`). iOS has to wait for its search controller
/// to finish folding away before a push sticks; a Mac's toolbar search field does not fold, so the push goes on
/// the next run loop turn, after SwiftUI has applied the closed search.
@MainActor final class SearchCloseWatcher {
    func whenClosed(_ done: @escaping () -> Void) {
        DispatchQueue.main.async(execute: done)
    }
}

/// Nothing to find on a Mac (see `SearchCloseWatcher`).
struct SearchCloseProbe: View {
    let watcher: SearchCloseWatcher
    var body: some View { EmptyView() }
}
