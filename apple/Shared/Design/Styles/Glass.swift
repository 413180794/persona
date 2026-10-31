import SwiftUI

nonisolated extension StyleSpec {
    /// The picker's line under 液态玻璃: the system's own look on either platform.
    #if os(macOS)
    private static let glassSubtitle = "系统原生，简洁"
    #else
    private static let glassSubtitle = "iOS 原生，简洁"
    #endif

    /// 液态玻璃: iOS 26's own look, Liquid Glass and system blue.
    static let glass = StyleSpec(
        title: "液态玻璃", subtitle: Self.glassSubtitle,
        accent: Palette.systemBlue,
        // A soft tint: a solid system blue was too heavy.
        userBubble: Color(light: 0xDCEAFF, dark: 0x1D3557), userBubbleText: .primary, deepensBubble: false,
        page: Theme.systemPage, card: Theme.systemCard, cardStroke: defaultStroke,
        surface: .glass, buttons: .glass, softShadow: defaultSoftShadow, fontDesign: .default, previewCorner: 20,
        components: .jarvis,
        lark: larkFollowing(accent: Palette.systemBlue, card: Theme.systemCard, cardStroke: defaultStroke,
                            otherBubble: larkGrayBubble),
        workList: .timeline)
}
