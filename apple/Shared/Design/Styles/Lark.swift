import SwiftUI

nonisolated extension ComponentSpec {
    /// 飞书: flat white cards on light gray in every color, hairline borders, bordered buttons, like the 飞书 app.
    static let lark: ComponentSpec = {
        let stroke = Color(light: 0xDEE0E3, dark: 0x373A40)
        return ComponentSpec(
            title: String(localized: "飞书"), subtitle: String(localized: "和飞书一致"),
            surface: .hairline, buttons: .bordered, fontDesign: .default, previewCorner: 10, components: .jarvis,
            canvas: .neutral(page: Color(light: 0xF5F6F7, dark: 0x141414), card: Color(light: 0xFFFFFF, dark: 0x1F1F1F)),
            cardStroke: stroke,
            // The Lark gateway's --message-incoming and its card chrome.
            larkOtherBubble: StyleSpec.larkGrayBubble, larkCard: Color(light: 0xFFFFFF, dark: 0x1E1F23),
            colors: .lark)
    }()
}

nonisolated extension ColorSpec {
    /// 飞书's blue (飞书's own); 飞书's pages keep the Lark gateway's --message-outgoing bubble.
    static let lark = ColorSpec(
        title: String(localized: "飞书蓝"), subtitle: String(localized: "飞书"),
        accent: Pair(light: 0x3370FF, dark: 0x4C88FF),
        bubble: Color(light: 0xD1E3FF, dark: 0x133063), bubbleText: .primary, deepensBubble: false, larksOwn: true)
}
