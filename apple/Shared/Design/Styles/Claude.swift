import SwiftUI

nonisolated extension ColorSpec {
    /// Claude's terracotta, the original Persona look. Once a style of its own (Liquid Glass in this color); since
    /// Theme Color is a choice of its own it is only a color (`AppearanceChoice` moves a saved Claude to Liquid Glass + this).
    static let coral = ColorSpec(
        title: String(localized: "Coral"), subtitle: "Claude",
        // Dark enough for white words on solid buttons and selected chips (≥ 3.5:1).
        accent: Pair(light: 0xD4683F, dark: 0xCD6E45),
        // White words on the bubble need ≥ 4.5:1: deeper than the accent.
        bubble: Color(light: 0xB24C25, dark: 0xAF4C28), bubbleText: .white, deepensBubble: true)
}
