import SwiftUI

/// The site's ring-plus-shadow elevations (`--shadow-hairline`, `-btn`, `-card`, `-raised`, `-overlay`).
///
/// CSS stacks up to six shadow layers; the port keeps the 1 pt ring and at most two
/// shadows so cards stay cheap to composite in scrolling lists.
public enum BUIElevation: Sendable, Hashable {
    case none
    /// 1 pt ring in `line`.
    case hairline
    /// Ring in `lineStrong` plus a faint 4 pt glow — buttons.
    case button
    /// Ring plus a soft drop — cards.
    case card
    /// Ring plus a medium drop — popovers attached to content.
    case raised
    /// Ring plus a large drop — menus and floating panels.
    case overlay

    struct Shadow {
        var color: Color
        var radius: CGFloat
        var y: CGFloat
    }

    func ring(_ colors: BUIColors) -> Color? {
        switch self {
        case .none: nil
        case .hairline: colors.line
        case .button: Color(light: 0xE0E2E5, dark: 0xFFFFFF, darkAlpha: 0.10)
        case .card: Color(light: 0xECEDEF, dark: 0xFFFFFF, darkAlpha: 0.11)
        case .raised: Color(light: 0xECEDEF, dark: 0xFFFFFF, darkAlpha: 0.13)
        case .overlay: Color(light: 0xECEDEF, dark: 0xFFFFFF, darkAlpha: 0.15)
        }
    }

    var shadows: [Shadow] {
        switch self {
        case .none, .hairline:
            []
        case .button:
            [Shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.04, darkAlpha: 0.3), radius: 1.5, y: 0.5)]
        case .card:
            [
                Shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.035, darkAlpha: 0.2), radius: 1, y: 1),
                Shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.035, darkAlpha: 0.2), radius: 10, y: 5),
            ]
        case .raised:
            [
                Shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.04, darkAlpha: 0.0), radius: 2, y: 1.5),
                Shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.05, darkAlpha: 0.22), radius: 8, y: 5),
            ]
        case .overlay:
            [
                Shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.04, darkAlpha: 0.0), radius: 4, y: 3),
                Shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.07, darkAlpha: 0.34), radius: 16, y: 10),
            ]
        }
    }
}

struct BUISurfaceModifier<S: InsettableShape>: ViewModifier {
    let shape: S
    let fill: Color?
    let elevation: BUIElevation
    @Environment(\.buiTheme) private var theme

    func body(content: Content) -> some View {
        let shadows = elevation.shadows
        let first = shadows.first
        let second = shadows.dropFirst().first
        content
            .clipShape(shape)
            .background {
                shape.fill(fill ?? theme.colors.surface)
                    .shadow(color: first?.color ?? .clear, radius: first?.radius ?? 0, y: first?.y ?? 0)
                    .shadow(color: second?.color ?? .clear, radius: second?.radius ?? 0, y: second?.y ?? 0)
            }
            .overlay {
                if let ring = elevation.ring(theme.colors) {
                    shape.strokeBorder(ring, lineWidth: 1)
                }
            }
    }
}

public extension View {
    /// Clips the view to `shape`, fills it (default `surface`) and elevates it the way the
    /// site's `bg-surface shadow-card` classes do.
    func buiSurface<S: InsettableShape>(
        _ shape: S,
        fill: Color? = nil,
        elevation: BUIElevation = .card
    ) -> some View {
        modifier(BUISurfaceModifier(shape: shape, fill: fill, elevation: elevation))
    }

    /// `buiSurface` in a continuous rounded rectangle.
    func buiSurface(radius: CGFloat, fill: Color? = nil, elevation: BUIElevation = .card) -> some View {
        modifier(BUISurfaceModifier(shape: RoundedRectangle(cornerRadius: radius, style: .continuous), fill: fill, elevation: elevation))
    }
}
