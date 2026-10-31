import SwiftUI

/// The site's `shimmer-text` effect: a bright band sweeping left to right across text
/// drawn in `ink3` (1.4 s, linear, forever). The sweep is a single animated offset, so
/// the text's body is not re-evaluated per frame.
struct ShimmerModifier: ViewModifier {
    var active: Bool
    var duration: Double
    var base: Color?
    var highlight: Color?
    @Environment(\.buiTheme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var phase: CGFloat = 0

    func body(content: Content) -> some View {
        if active && !reduceMotion {
            content
                .foregroundStyle(base ?? theme.colors.ink3)
                .overlay {
                    GeometryReader { proxy in
                        // background-size: 200%; background-position 150% → -50%
                        // moves the band's centre from -0.5w to 1.5w.
                        LinearGradient(
                            stops: [
                                .init(color: (highlight ?? theme.colors.ink).opacity(0), location: 0.35),
                                .init(color: highlight ?? theme.colors.ink, location: 0.5),
                                .init(color: (highlight ?? theme.colors.ink).opacity(0), location: 0.65),
                            ],
                            startPoint: .leading,
                            endPoint: .trailing
                        )
                        .frame(width: proxy.size.width * 2)
                        .offset(x: proxy.size.width * (phase * 2 - 1.5))
                    }
                    .mask(content)
                    .allowsHitTesting(false)
                }
                .onAppear {
                    phase = 0
                    withAnimation(.linear(duration: duration).repeatForever(autoreverses: false)) {
                        phase = 1
                    }
                }
        } else {
            content.foregroundStyle(active ? (base ?? theme.colors.ink2) : theme.colors.ink)
        }
    }
}

public extension View {
    /// Sweeps a highlight across the view's text while `active` (default `ink-3` → `ink`).
    func buiShimmer(_ active: Bool = true, duration: Double = 1.4, base: Color? = nil, highlight: Color? = nil) -> some View {
        modifier(ShimmerModifier(active: active, duration: duration, base: base, highlight: highlight))
    }
}
