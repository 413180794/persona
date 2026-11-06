import SwiftUI

/// Content that may be wider than the column, scrolling sideways: its right edge fades while there is more to the
/// right, so a cut-off line reads as "scroll", not as the end (a notebook's traceback, a wide diagram).
struct SidewaysScroll<Content: View>: View {
    @ViewBuilder var content: Content
    @State private var more = false

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) { content }
            .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
            .onScrollGeometryChange(for: Bool.self) { g in
                g.visibleRect.maxX < g.contentSize.width - 1 // as FittedTable's: right under an inset (#5737)
            } action: { _, now in more = now }
            .mask {
                LinearGradient(stops: [.init(color: .black, location: 0), .init(color: .black, location: more ? 0.85 : 1),
                                       .init(color: more ? .clear : .black, location: 1)],
                               startPoint: .leading, endPoint: .trailing)
            }
    }
}
