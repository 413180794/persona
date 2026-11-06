import SwiftUI

extension View {
    /// A round button as tall as a text field (the composer's +, send and Interrupt). `prominent` fills it with the tint.
    func macRoundButton(prominent: Bool = false) -> some View { buttonStyle(MacRoundButton(prominent: prominent)) }
}

/// The system's glass button styles drew no circle around the composer's + menu and lost the send arrow's fill once
/// clipped (2026-11-02): the circle is drawn here. Prominent ones are filled with the tint (the accent; Interrupt sets red).
struct MacRoundButton: ButtonStyle {
    var prominent: Bool
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        // The frame and the hit shape on the label, inside the button: outside it a plain button only took clicks on
        // the glyph itself.
        let round = configuration.label
            .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
            .contentShape(.circle)
            .opacity(configuration.isPressed ? 0.6 : 1)
        if prominent {
            round
                .foregroundStyle(Theme.macOnTint)
                .background(.tint, in: .circle)
                .opacity(enabled ? 1 : 0.4) // an empty composer's send must not look ready
        } else {
            round.surface(interactive: true, in: .circle)
        }
    }
}
