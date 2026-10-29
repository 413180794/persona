import SwiftUI

// Surfaces and buttons that follow the chosen style (AppStyle): Liquid Glass in 液态玻璃 / Claude, the style's own
// solid look in 黏土 / Island / 飞书. Use these instead of `glassEffect` / `.themedButton()` directly.

extension View {
    /// A floating panel (composer, interaction panel, status strips, toasts).
    func surface<S: Shape>(tint: Color? = nil, interactive: Bool = false, in shape: S) -> some View {
        modifier(Surface(shape: shape, tint: tint, interactive: interactive))
    }

    /// A list row on the style's canvas (plain lists paint rows white otherwise).
    @ViewBuilder func themedRow() -> some View {
        if AppStyle.current.usesGlass { self } else { listRowBackground(Color.clear) }
    }

    /// The page behind a list / scroll view: the style's canvas; the glass styles keep the system's.
    @ViewBuilder func themedPage() -> some View {
        if AppStyle.current.usesGlass {
            self
        } else {
            scrollContentBackground(.hidden).background(Theme.pageBackground)
        }
    }
}

private struct Surface<S: Shape>: ViewModifier {
    var shape: S
    var tint: Color?
    var interactive: Bool

    func body(content: Content) -> some View {
        switch AppStyle.current {
        case .glass, .claude:
            content.glassEffect(glass, in: shape)
        case .clay:
            // Clay: a puffy pastel slab — light from the top left, a soft colored shadow below.
            content.background {
                ZStack {
                    shape.fill(Theme.card)
                    if let tint { shape.fill(tint) }
                }
                .shadow(color: Color(light: 0xFFFFFF, dark: 0x3A3445).opacity(0.8), radius: 6, x: -4, y: -4)
                .shadow(color: Color(light: 0x6B54D6, dark: 0x000000).opacity(0.22), radius: 12, x: 5, y: 8)
            }
            .overlay(shape.stroke(Color.white.opacity(0.45), lineWidth: 1))
        case .island:
            content.background {
                ZStack {
                    shape.fill(Theme.card)
                    if let tint { shape.fill(tint) }
                }
                .shadow(color: .black.opacity(0.13), radius: 20, y: 10)
            }
        case .lark:
            content.background {
                ZStack {
                    shape.fill(Theme.card)
                    if let tint { shape.fill(tint) }
                }
            }
            .overlay(shape.stroke(Theme.cardStroke, lineWidth: 0.5))
        }
    }

    private var glass: Glass {
        var g = Glass.regular
        if let tint { g = g.tint(tint) }
        if interactive { g = g.interactive() }
        return g
    }
}

extension View {
    /// Glass buttons in the glass styles, bordered ones in 飞书, soft raised pills in 黏土 / Island. Picks the system
    /// style directly: wrapping `Button(configuration)` in a PrimitiveButtonStyle that re-styles it recursed until the
    /// stack overflowed (crash on opening a chat in the glass styles).
    @ViewBuilder func themedButton(prominent: Bool = false) -> some View {
        switch AppStyle.current {
        case .glass, .claude:
            // .glassProminent keeps its full color when disabled (iOS 26.0): an empty composer's send looked tappable.
            if prominent { buttonStyle(.glassProminent).modifier(DimWhenDisabled()) } else { buttonStyle(.glass) }
        case .lark:
            if prominent { buttonStyle(.borderedProminent) } else { buttonStyle(.bordered) }
        case .clay, .island:
            buttonStyle(SoftButtonStyle(prominent: prominent))
        }
    }
}

/// Fades a control whose style does not show "disabled" by itself.
private struct DimWhenDisabled: ViewModifier {
    @Environment(\.isEnabled) private var enabled

    func body(content: Content) -> some View {
        content.opacity(enabled ? 1 : 0.4)
    }
}

/// A raised pill for the clay / island styles.
private struct SoftButtonStyle: ButtonStyle {
    var prominent: Bool
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .foregroundStyle(prominent ? Color.white : Theme.accent)
            .background {
                Capsule()
                    .fill(prominent ? AnyShapeStyle(Theme.accent.gradient) : AnyShapeStyle(Theme.card))
                    .shadow(color: AppStyle.current == .clay ? Color(light: 0x6B54D6, dark: 0x000000).opacity(0.25) : .black.opacity(0.12),
                            radius: configuration.isPressed ? 2 : 6, y: configuration.isPressed ? 1 : 3)
            }
            .opacity(enabled ? 1 : 0.45)
            .scaleEffect(configuration.isPressed ? 0.96 : 1)
            .animation(.snappy(duration: 0.15), value: configuration.isPressed)
    }
}

extension View {
    /// Mirrors vertically: applied to the chat's scroll view (newest first) and again to each row to stand it up.
    func flippedBack() -> some View { scaleEffect(x: 1, y: -1) }
}

