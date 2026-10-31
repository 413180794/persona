import SwiftUI

/// Everything one style (`AppStyle`, 设置 › 界面风格) changes, in one value. A new style is a file in Styles/ and a case
/// in `AppStyle`; Theme, Styling, 飞书's chrome, the style picker and the BeautifulUI bridge read the spec, and nothing
/// else switches on the style. `StyleSpecTests` checks every spec's contrast.
nonisolated struct StyleSpec: Sendable {
    /// How floating panels, cards and the composer are drawn (`Styling.swift`).
    enum Surface: Sendable {
        /// Liquid Glass.
        case glass
        /// A puffy pastel slab lit from the top left (黏土).
        case puffy
        /// A white island with a deep soft shadow (Island).
        case floating
        /// Flat, with a hairline border (飞书).
        case hairline
    }

    enum Buttons: Sendable {
        case glass
        /// The system's bordered buttons.
        case bordered
        /// Raised pills at least 44 pt tall.
        case soft
    }

    /// Which components draw the chat's work and the composer.
    enum Components: Sendable {
        case jarvis
        /// BeautifulUI's thinking trace, tool chips and prompt bar.
        case beautiful
    }

    /// 飞书's messages and cards inside the app: links, bubbles and card chrome.
    struct LarkChrome: Sendable {
        var blue: Color
        var myBubble: Color
        var otherBubble: Color
        var cardBackground: Color
        var cardStroke: Color
    }

    var title: String
    var subtitle: String
    /// What can be tapped. White words on it (solid buttons, selected chips) need ≥ 3.5:1.
    var accent: Color
    /// The user's own messages, and the words on them (≥ 4.5:1).
    var userBubble: Color
    var userBubbleText: Color
    /// Colored bubbles darken a little toward the bottom; light ones stay flat.
    var deepensBubble: Bool
    /// The page behind lists and chats.
    var page: Color
    /// Cards and panels where the surface is not glass.
    var card: Color
    var cardStroke: Color
    var surface: Surface
    var buttons: Buttons
    /// The soft buttons' shadow.
    var softShadow: Color
    var fontDesign: Font.Design
    /// The corner of the cards in the style picker's preview.
    var previewCorner: CGFloat
    var components: Components
    var lark: LarkChrome

    /// 飞书 inside a style that is not 飞书: the style's accent for chrome, its cards for cards.
    static func larkFollowing(accent: Color, card: Color, cardStroke: Color, otherBubble: Color? = nil) -> LarkChrome {
        LarkChrome(blue: accent, myBubble: accent.opacity(0.16), otherBubble: otherBubble ?? card,
                   cardBackground: card, cardStroke: cardStroke)
    }

    /// 飞书's own gray for other people's bubbles, used by the styles close to the system look.
    static let larkGrayBubble = Color(light: 0xEBECED, dark: 0x262626)
    static let defaultStroke = Color.primary.opacity(0.08)
    static let defaultSoftShadow = Color.black.opacity(0.12)
}

nonisolated extension AppStyle {
    var spec: StyleSpec {
        switch self {
        case .glass: .glass
        case .claude: .claude
        case .clay: .clay
        case .island: .island
        case .lark: .lark
        case .beautiful: .beautiful
        }
    }
}
