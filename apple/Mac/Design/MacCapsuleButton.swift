import SwiftUI

/// The Mac's `themedButton()`: a capsule drawn here. The system's glass button filled itself with the accent whenever
/// the window was the active one, and a quick answer's 「推荐」 tag, in the AI's purple, disappeared into the blue
/// (2026-11-02 user: 文字被背景色盖住都看不清); in a window behind it was gray. Prominent: white words on the tint, as
/// 发送 on the inbox's draft card; the others: the tint's words on a light wash of it, as 修改 beside it and the chips
/// are, over the style's surface (glass), so the round ones floating over a chat or a photo (回到最新, 上一张) keep a
/// backdrop. Follows the tint (中断 sets red) and the control size.
struct MacCapsuleButton: ButtonStyle {
    var prominent: Bool
    @Environment(\.isEnabled) private var enabled
    @Environment(\.controlSize) private var size

    func makeBody(configuration: Configuration) -> some View {
        let label = configuration.label
            .padding(.horizontal, horizontal)
            .padding(.vertical, vertical)
            .foregroundStyle(prominent ? AnyShapeStyle(Theme.macOnTint) : AnyShapeStyle(.tint))
            .contentShape(.capsule)
        Group {
            if prominent {
                label.background(.tint, in: .capsule)
            } else {
                label.background(.tint.opacity(Theme.macButtonWash), in: .capsule).surface(interactive: true, in: .capsule)
            }
        }
        .opacity(enabled ? (configuration.isPressed ? Theme.macButtonPressed : 1) : Theme.macButtonDisabled)
    }

    private var horizontal: CGFloat {
        switch size {
        case .mini, .small: Theme.Space.s
        case .large, .extraLarge: Theme.Space.l
        default: Theme.Space.m
        }
    }

    private var vertical: CGFloat {
        switch size {
        case .mini, .small: Theme.Space.xxs
        case .large, .extraLarge: Theme.Space.s
        default: Theme.Space.xs
        }
    }
}

extension Theme {
    /// How much of the tint a plain button's capsule takes (as a tinted chip's fill).
    static let macButtonWash = 0.12
    /// A button held down, and one that cannot be pressed (an empty composer's send must not look ready).
    static let macButtonPressed = 0.7
    static let macButtonDisabled = 0.4
}
