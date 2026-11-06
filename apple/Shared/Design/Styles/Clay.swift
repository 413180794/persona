import SwiftUI

nonisolated extension ComponentSpec {
    /// Clay: puffy rounded surfaces with layered shadows on a pastel wash of the color, rounded type, soft buttons.
    static let clay = ComponentSpec(
        title: String(localized: "Clay", comment: "Interface style name: clay"), subtitle: String(localized: "Soft and rounded"),
        surface: .puffy, buttons: .soft, fontDesign: .rounded, previewCorner: 24, components: .persona, canvas: .wash,
        tintedShadow: true, colors: .lavender)
}

nonisolated extension ColorSpec {
    /// Soft pastel lavender (Clay's own), with the wash chosen by hand.
    static let lavender = ColorSpec(
        title: String(localized: "Lavender"), subtitle: String(localized: "Clay", comment: "Interface style name: clay"),
        accent: Pair(light: 0x7A5AF8, dark: 0x8979EB),
        bubble: Color(light: 0x6759D3, dark: 0x6756D9), bubbleText: .white, deepensBubble: true,
        // The dark canvas sits under the system's grouped cells (#1C1C1E): at #1D1A23 they were one color (1.01:1).
        wash: Wash(page: Pair(light: 0xEFEAF7, dark: 0x0A0510), card: Pair(light: 0xF8F5FD, dark: 0x2A2533),
                   shadow: 0x6B54D6, highlight: 0x3A3445))
}
