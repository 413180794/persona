import SwiftUI

nonisolated extension StyleSpec {
    /// 黏土: soft pastel lavender, puffy rounded surfaces with layered shadows, rounded type.
    static let clay: StyleSpec = {
        let accent = Color(light: 0x7A5AF8, dark: 0x8979EB)
        let card = Color(light: 0xF8F5FD, dark: 0x2A2533)
        return StyleSpec(
            title: "黏土", subtitle: "柔和、圆润",
            accent: accent,
            userBubble: Color(light: 0x6759D3, dark: 0x6756D9), userBubbleText: .white, deepensBubble: true,
            // The dark canvas sits under the system's grouped cells (#1C1C1E): at #1D1A23 they were one color (1.01:1).
            page: Color(light: 0xEFEAF7, dark: 0x0A0510), card: card, cardStroke: defaultStroke,
            surface: .puffy, buttons: .soft, softShadow: Color(light: 0x6B54D6, dark: 0x000000).opacity(0.25),
            fontDesign: .rounded, previewCorner: 24,
            components: .jarvis,
            lark: larkFollowing(accent: accent, card: card, cardStroke: defaultStroke))
    }()
}
