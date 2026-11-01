import SwiftUI

/// What the app looks like, in one value, made of three choices in 设置 (docs/ios.md「设计语言」): the style (`AppStyle`,
/// 界面风格: how panels, cards and buttons are drawn, the type, the components), the colors (`ThemeColor`, 主题色: what
/// can be tapped, the user's bubble, 飞书's links, the wash of a colored canvas), and the persona, of which only its own
/// primary is here (`Theme.persona`, its face's glow). Theme, Styling, 飞书's chrome and the BeautifulUI bridge read
/// `StyleSpec.current`; nothing else switches on a choice. A new style is a `ComponentSpec` in Styles/ and a case in
/// `AppStyle`, a new color a `ColorSpec` and a case in `ThemeColor`. `StyleSpecTests` checks every style in every color.
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

    /// How an open work line lists its steps.
    enum WorkList: Sendable {
        /// A gray card, the steps its rows with hairlines between them.
        case card
        /// On the page like the chat around it: a thin rail down the left that the tools' icons sit on, the agent's
        /// narration in the quieter type (液态玻璃, 2026-10-31: the card read heavier than the chat).
        case timeline
    }

    /// 飞书's messages and cards inside the app: links, bubbles and card chrome.
    struct LarkChrome: Sendable {
        var blue: Color
        var myBubble: Color
        var otherBubble: Color
        var cardBackground: Color
        var cardStroke: Color
    }

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
    /// A puffy surface's colored shadow below and lit edge above (黏土), from the color's wash.
    var puffyShadow: Color
    var puffyHighlight: Color
    var fontDesign: Font.Design
    var components: Components
    var lark: LarkChrome
    /// The persona's own color (`Theme.persona`), when it brings one (docs/persona.md).
    var personaPrimary: Color?
    var workList: WorkList

    var usesGlass: Bool { surface == .glass }

    /// A style drawn in some colors.
    init(_ style: ComponentSpec, _ colors: ColorSpec, personaPrimary: Color? = nil) {
        accent = colors.accent
        userBubble = colors.userBubble
        userBubbleText = colors.userBubbleText
        deepensBubble = colors.deepensBubble
        switch style.canvas {
        case .system:
            page = Theme.systemPage
            card = Theme.systemCard
        case .neutral(let p, let c):
            page = p
            card = c
        case .wash:
            page = colors.wash.page.color
            card = colors.wash.card.color
        case .islands:
            page = colors.wash.page.color
            card = Color(light: 0xFFFFFF, dark: colors.wash.card.dark)
        }
        cardStroke = style.cardStroke
        surface = style.surface
        buttons = style.buttons
        puffyShadow = Color(light: colors.wash.shadow, dark: 0x000000)
        puffyHighlight = Color(light: 0xFFFFFF, dark: colors.wash.highlight)
        softShadow = style.tintedShadow ? puffyShadow.opacity(0.25) : Self.defaultSoftShadow
        fontDesign = style.fontDesign
        components = style.components
        workList = style.workList
        lark = LarkChrome(blue: colors.larkBlue, myBubble: colors.larkMyBubble, otherBubble: style.larkOtherBubble ?? card,
                          cardBackground: style.larkCard ?? card, cardStroke: style.cardStroke)
        self.personaPrimary = personaPrimary
    }

    /// The three choices together.
    static func compose(style: AppStyle, color: ThemeColor, persona: PersonaPalette?) -> StyleSpec {
        StyleSpec(style.component, color.colors(style: style, persona: persona), personaPrimary: persona?.primary?.color)
    }

    /// The choices on screen (`AppStyle.current`, `ThemeColor.current`, `PersonaPalette.current`), kept up to date by
    /// each of them so the tokens read one value.
    nonisolated(unsafe) static var current = compose(style: .current, color: .current, persona: PersonaPalette.current)

    static func recompose() {
        current = compose(style: .current, color: .current, persona: PersonaPalette.current)
    }

    /// 飞书's own gray for other people's bubbles, used by the styles close to the system look.
    static let larkGrayBubble = Color(light: 0xEBECED, dark: 0x262626)
    static let defaultStroke = Color.primary.opacity(0.08)
    static let defaultSoftShadow = Color.black.opacity(0.12)
}

/// One 界面风格 (`AppStyle`): how things are drawn, whatever the color (that is 主题色, `ColorSpec`).
nonisolated struct ComponentSpec: Sendable {
    /// The page behind lists and chats, and the cards on it.
    enum Canvas: Sendable {
        /// The system's (the Liquid Glass styles).
        case system
        /// Its own, the same in every color (飞书's light gray and white cards).
        case neutral(page: Color, card: Color)
        /// The color's pale wash, cards a step lighter (黏土).
        case wash
        /// The color's pale wash with white islands on it; in dark, the wash's cards (Island).
        case islands
    }

    var title: String
    var subtitle: String
    var surface: StyleSpec.Surface
    var buttons: StyleSpec.Buttons
    var fontDesign: Font.Design
    /// The corner of the cards in the style picker's preview.
    var previewCorner: CGFloat
    var components: StyleSpec.Components
    var workList: StyleSpec.WorkList = .card
    var canvas: Canvas
    var cardStroke = StyleSpec.defaultStroke
    /// Soft buttons cast the color's shadow (黏土) instead of a plain one.
    var tintedShadow = false
    /// 飞书's other people's bubbles and its cards: the style's own, or (nil) its cards.
    var larkOtherBubble: Color? = nil
    var larkCard: Color? = nil
    /// The colors it came with: 主题色「跟随界面风格」.
    var colors: ColorSpec
}

nonisolated extension AppStyle {
    var component: ComponentSpec {
        switch self {
        case .glass: .glass
        case .clay: .clay
        case .island: .island
        case .lark: .lark
        case .beautiful: .beautiful
        }
    }
}
