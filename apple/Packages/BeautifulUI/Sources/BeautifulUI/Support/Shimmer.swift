import SwiftUI

/// The site's `shimmer-text` effect: a bright band sweeping left to right across text
/// drawn in `ink3` (1.4 s, linear, forever). The sweep is a layer animation, so the text's body is not re-evaluated
/// per frame and nothing runs on the main thread for it.
struct ShimmerModifier: ViewModifier {
    var active: Bool
    var duration: Double
    var base: Color?
    var highlight: Color?
    @Environment(\.buiTheme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        if active && !reduceMotion {
            content
                .foregroundStyle(base ?? theme.colors.ink3)
                .overlay {
                    // The band itself is a Core Animation layer on iOS (`SweepBand`): the text is drawn once.
                    SweepBand(color: highlight ?? theme.colors.ink, duration: duration)
                        .mask(content)
                        .allowsHitTesting(false)
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
