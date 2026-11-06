import SwiftUI

nonisolated extension ComponentSpec {
    /// Island: floating white islands with deep soft shadows on a misty wash of the color, generous corners.
    static let island = ComponentSpec(
        title: "Island", subtitle: String(localized: "Floating cards"),
        surface: .floating, buttons: .soft, fontDesign: .default, previewCorner: 26, components: .persona, canvas: .islands,
        colors: .teal)
}

nonisolated extension ColorSpec {
    /// Teal (Island's own) that leans cyan and darker, away from the Unread green and readable as a link on the mist;
    /// an ink bubble (light on dark in dark mode).
    static let teal = ColorSpec(
        title: String(localized: "Teal", comment: "Theme color name: teal"), subtitle: "Island",
        accent: Pair(light: 0x27747F, dark: 0x2A9798),
        bubble: Color(light: 0x14201E, dark: 0xE8F3F1), bubbleText: Color(light: 0xFFFFFF, dark: 0x0B1211), deepensBubble: true,
        wash: {
            var w = Wash(accent: Pair(light: 0x27747F, dark: 0x2A9798))
            w.page = Pair(light: 0xE6F1EF, dark: 0x0A1110)
            w.card.dark = 0x16201E
            return w
        }())
}
