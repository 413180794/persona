import SwiftUI

/// A page pushed past the first one in a split's detail column (设置 › hub › SSH 隧道, a machine's page under 机器): its
/// way back, a chevron in the title bar's row at the column's own leading edge, as Finder and System Settings have it.
/// The system's back button went to the leading end of the window's toolbar instead, by the traffic lights and over
/// the sections column, drawn as an iPhone's round glass button (#1170). The first page has nothing to go back to.
struct MacDetailBack: ViewModifier {
    /// Whether there is a page under this one.
    var shown: Bool
    @Environment(\.dismiss) private var dismiss

    func body(content: Content) -> some View {
        content
            .navigationBarBackButtonHidden(true)
            .overlay(alignment: .topLeading) {
                if shown {
                    Button { dismiss() } label: {
                        Image(systemName: Theme.Icon.back)
                            .font(Theme.Font.heading)
                            .scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.borderless)
                    .foregroundStyle(.secondary)
                    .help("返回")
                    .accessibilityLabel("返回")
                    .accessibilityIdentifier("mac.detail.back")
                    .padding(.leading, Theme.Space.s)
                    .frame(height: Theme.Size.macToolbarHeight)
                    .ignoresSafeArea(.container, edges: .top)
                }
            }
    }
}
