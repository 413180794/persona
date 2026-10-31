import SwiftUI

nonisolated extension StyleSpec {
    /// Island: floating white islands with deep soft shadows on a misty teal canvas, generous corners.
    static let island: StyleSpec = {
        // Teal that leans cyan and darker, away from the 完成未看 green and readable as a link on the mist.
        let accent = Color(light: 0x27747F, dark: 0x2A9798)
        let card = Color(light: 0xFFFFFF, dark: 0x16201E)
        return StyleSpec(
            title: "Island", subtitle: "悬浮卡片",
            accent: accent,
            userBubble: Color(light: 0x14201E, dark: 0xE8F3F1), userBubbleText: Color(light: 0xFFFFFF, dark: 0x0B1211),
            deepensBubble: true,
            page: Color(light: 0xE6F1EF, dark: 0x0A1110), card: card, cardStroke: defaultStroke,
            surface: .floating, buttons: .soft, softShadow: defaultSoftShadow, fontDesign: .default, previewCorner: 26,
            components: .jarvis,
            lark: larkFollowing(accent: accent, card: card, cardStroke: defaultStroke))
    }()
}
