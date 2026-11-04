import SwiftUI

/// A page in a split's detail column, in the title bar's row at the column's own leading edge: the way back (‹) when
/// there is a page under this one, then the page's title when it gives one (`detailTitle`, 设置's pages), as Finder and
/// System Settings have them (#1224 B2).
/// The system's back button went to the leading end of the window's toolbar instead, by the traffic lights and over
/// the sections column, drawn as an iPhone's round glass button (#1170). The first page has nothing to go back to.
struct MacDetailBack: ViewModifier {
    /// Whether there is a page under this one.
    var shown: Bool
    @Environment(\.dismiss) private var dismiss

    func body(content: Content) -> some View {
        content
            .navigationBarBackButtonHidden(true)
            .overlayPreferenceValue(MacDetailTitle.self, alignment: .topLeading) { title in
                if shown || title != nil {
                    HStack(spacing: Theme.Space.xs) {
                        if shown { back }
                        if let title {
                            // A label: the row still drags the window.
                            Text(title)
                                .font(Theme.Font.heading)
                                .foregroundStyle(Theme.macLabel)
                                .lineLimit(1)
                                .allowsHitTesting(false)
                                .accessibilityAddTraits(.isHeader)
                        }
                    }
                    .padding(.leading, shown ? Theme.Space.s : Theme.Space.l)
                    .frame(height: Theme.Size.macToolbarHeight)
                    .ignoresSafeArea(.container, edges: .top)
                }
            }
    }

    private var back: some View {
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
    }
}

/// The title a page in a split's detail column gives for the column's title row (`detailTitle`).
struct MacDetailTitle: PreferenceKey {
    static let defaultValue: String? = nil
    static func reduce(value: inout String?, nextValue: () -> String?) { value = value ?? nextValue() }
}
