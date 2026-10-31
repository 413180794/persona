import SwiftUI

/// The site's `fade-up` / `fade-in` / `pop-in` entrances, played once when the view appears.
public enum BUIEntrance: Sendable, Hashable {
    /// Opacity 0 → 1 while rising 8 pt.
    case fadeUp
    /// Opacity 0 → 1.
    case fadeIn
    /// Opacity 0 → 1 while scaling from 0.95.
    case popIn
}

struct EntranceModifier: ViewModifier {
    var entrance: BUIEntrance
    var duration: Double
    var delay: Double
    var curve: BUICurve
    var anchor: UnitPoint
    @State private var shown = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.buiTheme.entrances) private var entrances

    func body(content: Content) -> some View {
        let visible = shown || reduceMotion || !entrances
        content
            .opacity(visible ? 1 : 0)
            .offset(y: entrance == .fadeUp && !visible ? 8 : 0)
            .scaleEffect(entrance == .popIn && !visible ? 0.95 : 1, anchor: anchor)
            .onAppear {
                guard !shown else { return }
                withAnimation(curve.animation(duration: duration).delay(delay)) { shown = true }
            }
    }
}

public extension View {
    /// Plays a one-shot entrance when the view appears (skipped under Reduce Motion).
    func buiEntrance(
        _ entrance: BUIEntrance = .fadeUp,
        duration: Double = 0.32,
        delay: Double = 0,
        curve: BUICurve = .outStrong,
        anchor: UnitPoint = .center
    ) -> some View {
        modifier(EntranceModifier(entrance: entrance, duration: duration, delay: delay, curve: curve, anchor: anchor))
    }
}

/// A 12 pt ring spinner: `border-line-strong` track with an `ink-2` head (700 ms per turn).
public struct BUISpinner: View {
    var size: CGFloat
    var lineWidth: CGFloat
    var period: Double
    @Environment(\.buiTheme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var turning = false

    public init(size: CGFloat = 12, lineWidth: CGFloat = 1.5, period: Double = 0.7) {
        self.size = size
        self.lineWidth = lineWidth
        self.period = period
    }

    public var body: some View {
        ZStack {
            Circle().strokeBorder(theme.colors.lineStrong, lineWidth: lineWidth)
            // A CSS border-top on a circle covers the top quarter.
            Circle()
                .inset(by: lineWidth / 2)
                .trim(from: 0.625, to: 0.875)
                .stroke(theme.colors.ink2, lineWidth: lineWidth)
        }
        .frame(width: size, height: size)
        .rotationEffect(.degrees(turning ? 360 : 0))
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.linear(duration: period).repeatForever(autoreverses: false)) { turning = true }
        }
        .accessibilityHidden(true)
    }
}
