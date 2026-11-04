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
