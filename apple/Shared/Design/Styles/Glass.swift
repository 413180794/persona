import SwiftUI

nonisolated extension ComponentSpec {
    /// The picker's line under Liquid Glass: the system's own look on either platform.
    #if os(macOS)
    private static var glassSubtitle: String { String(localized: "系统原生，简洁") }
    #else
    private static var glassSubtitle: String { String(localized: "iOS 原生，简洁") }
    #endif

    /// Liquid Glass: iOS 26's own look, Liquid Glass on the system's pages.
    static let glass = ComponentSpec(
        title: String(localized: "液态玻璃"), subtitle: Self.glassSubtitle,
        surface: .glass, buttons: .glass, fontDesign: .default, previewCorner: 20, components: .jarvis, workList: .timeline,
        canvas: .system,
        larkOtherBubble: StyleSpec.larkGrayBubble, colors: .blue)
}

nonisolated extension ColorSpec {
    /// System blue (Liquid Glass's own).
    static let blue = ColorSpec(
        title: String(localized: "系统蓝"), subtitle: String(localized: "液态玻璃"),
        accent: Pair(light: 0x007AFF, dark: 0x0A84FF),
        // A soft tint: a solid system blue was too heavy.
        bubble: Color(light: 0xDCEAFF, dark: 0x1D3557), bubbleText: .primary, deepensBubble: false)
}
