import SwiftUI

nonisolated extension StyleSpec {
    /// Claude's terracotta on Liquid Glass (the original Jarvis look).
    static let claude = StyleSpec(
        title: "Claude", subtitle: "原来的珊瑚橙",
        // Dark enough for white words on solid buttons and selected chips (≥ 3.5:1).
        accent: Color(light: 0xD4683F, dark: 0xCD6E45),
        // White words on the bubble need ≥ 4.5:1: deeper than the accent.
        userBubble: Color(light: 0xB24C25, dark: 0xAF4C28), userBubbleText: .white, deepensBubble: true,
        page: Theme.systemPage, card: Theme.systemCard, cardStroke: defaultStroke,
        surface: .glass, buttons: .glass, softShadow: defaultSoftShadow, fontDesign: .default, previewCorner: 20,
        components: .jarvis,
        lark: larkFollowing(accent: Color(light: 0xD4683F, dark: 0xCD6E45), card: Theme.systemCard, cardStroke: defaultStroke,
                            otherBubble: larkGrayBubble))
}
