import SwiftUI

/// A pushed page's title in the title bar's row (`pageTitle`, #855), drawn as the shell draws a section's name there
/// (#830): past the traffic lights and the toolbar's buttons when the page sits at the window's leading edge (the
/// sections column hidden, a one-column page), at the page's own edge otherwise (beside a list). Measured here rather
/// than read from `macTitleRowLeading`: a page pushed in a split's detail column got the page's value, not the
/// column's. A label, so the row still drags the window.
/// It tells the shell the row is taken (`MacTitleRowTaken`), so the section's name does not show under it.
struct MacTitleRowTitle: ViewModifier {
    let title: String
    let subtitle: String?
    /// The page starts at the window's leading edge.
    @State private var atEdge = false

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .topLeading) {
                VStack(alignment: .leading, spacing: 0) {
                    Text(title)
                        .font(subtitle == nil ? Theme.Font.title : Theme.Font.heading)
                        .foregroundStyle(Theme.macLabel)
                    if let subtitle, !subtitle.isEmpty {
                        Text(subtitle).font(Theme.Font.caption).foregroundStyle(.secondary)
                    }
                }
                .lineLimit(1)
                .padding(.leading, atEdge ? Theme.Size.macTitleAfterControls : Theme.Space.l)
                .frame(height: Theme.Size.macToolbarHeight)
                .ignoresSafeArea(.container, edges: .top)
                .allowsHitTesting(false)
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isHeader)
            }
            .onGeometryChange(for: Bool.self) { $0.frame(in: .global).minX < 1 } action: { atEdge = $0 }
            .preference(key: MacTitleRowTaken.self, value: true)
    }
}

/// Whether a page in front draws its own title in the title bar's row (`MacTitleRowTitle`): the shell then leaves out
/// the section's name there.
struct MacTitleRowTaken: PreferenceKey {
    static let defaultValue = false
    static func reduce(value: inout Bool, nextValue: () -> Bool) { value = value || nextValue() }
}
