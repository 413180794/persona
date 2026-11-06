import SwiftUI

extension View {
    /// On a horizontal `ScrollView`: each end fades out over `Theme.Size.fadeEdge` only while there is more to scroll to
    /// that way, so a cut-off chip reads as "more", and a row that fits is drawn as it is.
    func edgeFade() -> some View { modifier(EdgeFade()) }
}

private struct EdgeFade: ViewModifier {
    @State private var more = Edges()

    private struct Edges: Equatable {
        var leading = false
        var trailing = false
        var width: CGFloat = 0
    }

    func body(content: Content) -> some View {
        content
            .onScrollGeometryChange(for: Edges.self) { g in
                // At rest the offset is minus the leading margin; at the end the trailing margin shows past the content.
                Edges(leading: g.contentOffset.x > 1 - g.contentInsets.leading,
                      trailing: g.contentOffset.x + g.containerSize.width < g.contentSize.width + g.contentInsets.trailing - 1,
                      width: g.containerSize.width)
            } action: { _, edges in
                more = edges
            }
            // No mask while the row fits: an offscreen pass on every header over the scrolling chat for nothing.
            .mask {
                if more.leading || more.trailing {
                    LinearGradient(stops: stops, startPoint: .leading, endPoint: .trailing)
                } else {
                    Rectangle()
                }
            }
    }

    /// Opaque, fading over the fade's width at a side with more to scroll to; never more than a quarter of a narrow row.
    private var stops: [Gradient.Stop] {
        let f = more.width > 0 ? min(Theme.Size.fadeEdge / more.width, 0.25) : 0
        return [.init(color: .black.opacity(more.leading ? 0 : 1), location: 0), .init(color: .black, location: f),
                .init(color: .black, location: 1 - f), .init(color: .black.opacity(more.trailing ? 0 : 1), location: 1)]
    }
}
