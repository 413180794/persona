import SwiftUI

// Settings' list beside the page it opened (#2318): the row of the page open in the detail column stays selected, the
// rounded gray well the sections column draws (`MacSidebarRow`). The split's own list draws no selection for value
// links, and its path is opaque, so the list is handed the path cut to its first page and each row compares its
// value with it; nothing is stored (the path is the navigation state already).

extension EnvironmentValues {
    /// The settings path cut to its first page (empty when nothing is open): the page the list's selected row opened.
    @Entry var macSettingsOpen = NavigationPath()
}

extension NavigationPath {
    /// The path with only its first page.
    var firstPageOnly: NavigationPath {
        var p = self
        if p.count > 1 { p.removeLast(p.count - 1) }
        return p
    }
}

struct MacSettingsRowSelection<Value: Hashable>: ViewModifier {
    var value: Value
    @Environment(\.macSettingsOpen) private var open

    func body(content: Content) -> some View {
        let selected = !open.isEmpty && open == NavigationPath([value])
        // Only the selected row sets a background: `nil` on the others put back the list's default, over the clear
        // one the tinted styles set around the whole list (`themedPage`).
        if selected {
            content.listRowBackground(shade).accessibilityAddTraits(.isSelected)
        } else {
            content
        }
    }

    /// The row's whole height, inset from the column's edges as the sections column's selection is.
    private var shade: some View {
        RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
            .fill(Theme.Fill.macSelectedInSidebarList)
            .padding(.horizontal, Theme.Space.s)
    }
}
