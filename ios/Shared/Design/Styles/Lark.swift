import SwiftUI

nonisolated extension StyleSpec {
    /// 飞书: its blue, flat white cards on light gray, hairline borders, like the 飞书 app.
    static let lark: StyleSpec = {
        let blue = Color(light: 0x3370FF, dark: 0x4C88FF)
        let bubble = Color(light: 0xD1E3FF, dark: 0x133063)
        let stroke = Color(light: 0xDEE0E3, dark: 0x373A40)
        return StyleSpec(
            title: "飞书", subtitle: "和飞书一致",
            accent: blue,
            userBubble: bubble, userBubbleText: .primary, deepensBubble: false,
            page: Color(light: 0xF5F6F7, dark: 0x141414), card: Color(light: 0xFFFFFF, dark: 0x1F1F1F), cardStroke: stroke,
            surface: .hairline, buttons: .bordered, softShadow: defaultSoftShadow, fontDesign: .default, previewCorner: 10,
            components: .jarvis,
            // lark-connector's --message-outgoing / --message-incoming and its card chrome.
            lark: LarkChrome(blue: blue, myBubble: bubble, otherBubble: larkGrayBubble,
                             cardBackground: Color(light: 0xFFFFFF, dark: 0x1E1F23), cardStroke: stroke))
    }()
}
