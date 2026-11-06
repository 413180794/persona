import SwiftUI

/// The 收件箱 menu (docs/mac.md「菜单和快捷键」): the toolbar's 全部清理 (⇧⌘E) for the open items in view, while the
/// inbox is the section on screen. Many items ask first, in the list (`confirming`).
struct MacInboxMenu: Commands {
    let model: AppModel

    var body: some Commands {
        CommandMenu("收件箱") {
            let state = MacInboxState.shared
            let onScreen = MacSection.shown(override: MacShellState.shared.section, tab: model.selectedTab) == .inbox
            let off = !onScreen || state.openInView.isEmpty
            Button("全部清理") { state.clean() }
                .keyboardShortcut("e", modifiers: [.command, .shift])
                .disabled(off)
        }
    }
}
