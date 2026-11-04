import SwiftUI

/// A settings page's body: the phone's list as it was; on the Mac a grouped form, as System Settings draws its panes
/// (#1169 B1): rounded groups, switches on the trailing side, footers that wrap. A Mac `List` draws a section's
/// footer on one line, cut off with an ellipsis, and the sidebar style leaves it out (#1212).
struct SettingsForm<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        #if os(macOS)
        Form { content }.formStyle(.grouped)
        #else
        List { content }
        #endif
    }
}

/// A form's own action in a row of its own: the phone's full-width button on a clear row as before; on the Mac a
/// standard button at the row's trailing end (#1221; a Mac grouped form keeps the row's card whatever
/// `listRowBackground` says). The label takes `formActionWidth()`.
struct FormAction<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        #if os(macOS)
        HStack { Spacer(minLength: 0); content }
        #else
        content.listRowBackground(Color.clear)
        #endif
    }
}

extension View {
    /// A `FormAction` button's label: the row's whole width on the phone, its own width on the Mac.
    @ViewBuilder func formActionWidth() -> some View {
        #if os(macOS)
        self
        #else
        frame(maxWidth: .infinity)
        #endif
    }
}
