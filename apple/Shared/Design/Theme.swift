import SwiftUI

/// The look of the whole app, picked in 设置 › 界面风格. Every color, surface and button below follows it; changing it
/// rebuilds the view tree (RootView is keyed by the style), so plain static tokens are enough.
nonisolated enum AppStyle: String, CaseIterable, Identifiable, Sendable {
    /// iOS 26 system look: Liquid Glass, system blue.
    case glass
    /// Claude's terracotta on Liquid Glass (the original Jarvis look).
    case claude
    /// Claymorphism: soft pastel lavender, puffy rounded surfaces with layered shadows, rounded type.
    case clay
    /// Floating white "islands" with deep soft shadows on a misty teal canvas, generous corners.
    case island
    /// Feishu: its blue, flat white cards on light gray, hairline borders — like the Feishu app.
    case lark
    /// 液态玻璃's colors and surfaces with BeautifulUI's components: the thinking trace, tool chips in work cards, and its
    /// prompt bar as the composer. Only this style uses them (CLAUDE.md: a new look is a new style, never a change to one).
    case beautiful

    nonisolated(unsafe) static var current: AppStyle =
        AppStyle(rawValue: UserDefaults.standard.string(forKey: "ui.style") ?? "") ?? .glass {
        didSet { StyleSpec.current = current.spec.tinted(PersonaPalette.current) }
    }

    var id: String { rawValue }
    var title: String { spec.title }
    var subtitle: String { spec.subtitle }
    /// Liquid Glass surfaces and buttons; the other styles draw solid ones of their own.
    var usesGlass: Bool { spec.surface == .glass }
    /// BeautifulUI's components (`.beautiful` only); every other style keeps Jarvis' own.
    var usesBeautifulUI: Bool { spec.components == .beautiful }
    var fontDesign: Font.Design { spec.fontDesign }
    /// Corner radius of the cards in the style picker's preview.
    var corner: CGFloat { spec.previewCorner }
}

nonisolated extension StyleSpec {
    /// The current style's spec with the current persona's colors over it, kept with `AppStyle.current` and
    /// `PersonaPalette.current` so the tokens read one value.
    nonisolated(unsafe) static var current: StyleSpec = AppStyle.current.spec.tinted(PersonaPalette.current)
}

nonisolated enum Theme {
    private static var spec: StyleSpec { StyleSpec.current }
    #if os(watchOS)
    static let systemPage = Color.black
    static let systemCard = Color(white: 0.12)
    #else
    static let systemPage = Color(uiColor: .systemBackground)
    static let systemCard = Color(uiColor: .secondarySystemBackground)
    #endif

    static var accent: Color { spec.accent }
    /// A style's accent as it would be now (the persona's over it): the style picker's previews.
    static func accent(_ style: AppStyle) -> Color { style.spec.tinted(PersonaPalette.current).accent }
    /// The persona's own color (主色, docs/persona.md): its face's glow and backdrop, never something to tap (that is
    /// `accent`). The accent when the persona brings none (the built-in Jarvis is drawn in the accent).
    static var persona: Color { spec.personaPrimary ?? spec.accent }
    /// The app icon's coral. Only for Jarvis' own mark (the glyph in 设置 / 对话, the Live Activity's star), never for
    /// something to tap: that is `accent`, whatever the style.
    static let brand = Palette.coral
    static let danger = Color(light: 0xD93A3A, dark: 0xFF5F57)

    // Meanings that are not agent states. Never use a state color (StatusPalette) for these: changing a state color
    // must not recolor diffs, warnings or categories.
    /// Success and "connected / online / usable" outside the agent states (toasts, the hub dot, a finished to-do).
    static let positive = Color(light: 0x248A3D, dark: 0x30D158)
    /// Warnings that are not an agent asking for you (connecting, stale, truncated, a version mismatch): a symbol
    /// beside the words, never a dot. `cautionText` when the words themselves are colored.
    static let caution = Color(light: 0xF08A00, dark: 0xFF9F0A)
    static let cautionText = Color(light: 0xB35400, dark: 0xFF9F0A)
    /// What the AI wrote or suggests (✦ summaries, AI actions): not the accent, which means "tap me".
    static let ai = Color(light: 0x5B4FD6, dark: 0x9C94FF)
    /// /loop and other scheduled wake-ups (待唤醒).
    static let loop = Color(light: 0x2E6FD0, dark: 0x5B9BFF)
    /// Count badges on rows (unread messages): white text on it is ≥ 4.5:1 in both appearances.
    static let countBadge = Color(light: 0x636366, dark: 0x636366)
    /// The disc behind a monogram that has no color of its own (a mail sender).
    #if os(watchOS)
    static let monogramFill = Color.white.opacity(0.16)
    #else
    static let monogramFill = Color(uiColor: .secondarySystemFill)
    #endif

    /// Diffs and Git: their own colors, text strength (≥ 4.5:1 on the code background) for signs and counts.
    enum Diff {
        static let add = Color(light: 0x1D7A4C, dark: 0x4CC38A)
        static let del = Color(light: 0xC62B33, dark: 0xFF6369)
        static let addFill = Color(light: 0x30A46C, dark: 0x4CC38A).opacity(0.14)
        static let delFill = Color(light: 0xE5484D, dark: 0xFF6369).opacity(0.12)
        /// A modified file ("M"), the way editors mark it, kept apart from the working amber.
        static let changed = Color(light: 0x8A6A00, dark: 0xE3B341)
    }

    /// Corner radii: five, plus capsules and circles. Static: the styles differ in color and surface, not in corners
    /// (`AppStyle.corner` only draws the style previews in 设置).
    enum Radius {
        /// Badges, inline code, small marks.
        static let badge: CGFloat = 6
        /// Code blocks, thumbnails, segmented controls, small tiles.
        static let control: CGFloat = 10
        /// Cards, groups, an open work line, option rows.
        static let card: CGFloat = 14
        /// Message bubbles.
        static let bubble: CGFloat = 20
        /// Floating panels over the chat (interactions, strips, the key bar), the terminal card, the composer's field.
        static let panel: CGFloat = 22
        /// A whole page pushed aside as a card (the Jarvis tab's page beside its sidebar).
        static let page: CGFloat = 34
    }

    /// Spacing on a 4 pt grid, BeautifulUI's (`BUISpacing`): 2 4 6 8 12 16 24 32, nothing in between. Pages and the
    /// terminal card keep 16 from the screen's edges.
    enum Space {
        static let xxs: CGFloat = 2
        static let xs: CGFloat = 4
        static let sm: CGFloat = 6
        static let s: CGFloat = 8
        static let m: CGFloat = 12
        static let l: CGFloat = 16
        static let xl: CGFloat = 24
        static let xxl: CGFloat = 32
        /// Between the lines of one row: title, preview, meta.
        static let lines: CGFloat = xs
        /// From a title to the content under it.
        static let titleToContent: CGFloat = s
        /// Between blocks on a page or in a card.
        static let block: CGFloat = l
        /// Between sections.
        static let section: CGFloat = xl
    }

    /// Three grays for backgrounds instead of `Color.primary.opacity(…)`: they are the system fills, so they read in
    /// dark mode too (5 % white on black was all but invisible). `well` behind icons and quiet rows, `chip` behind
    /// labels and small controls, `pressed` for a selected or pressed state.
    enum Fill {
        #if os(watchOS)
        static let well = Color.white.opacity(0.08)
        static let chip = Color.white.opacity(0.12)
        static let pressed = Color.white.opacity(0.18)
        #else
        static let well = Color(uiColor: .quaternarySystemFill)
        static let chip = Color(uiColor: .tertiarySystemFill)
        static let pressed = Color(uiColor: .secondarySystemFill)
        #endif
    }

    /// The type scale: every text in the app is one of these levels, all Dynamic Type text styles, so they follow the
    /// system's text size and the app's own 小 / 标准 / 大 (`TextSize`, which moves the Dynamic Type size a step).
    /// As in BeautifulUI, levels differ more by weight and color than by size: a row's title is the body's size in
    /// semibold, a section header the caption's size in semibold secondary. Sizes at the default setting in brackets.
    /// The terminal grid (its own size setting), the Live Activity (system limits), the share card (a rendered
    /// image) and glyphs drawn to a frame are the exceptions.
    enum Font {
        /// The home page's greeting, the welcome page (28 bold).
        static let display = SwiftUI.Font.title.bold()
        /// A detail page's or a sheet's title: a mail's subject, a document, a profile, the settings header (22).
        static let title = SwiftUI.Font.title2.weight(.semibold)
        /// Titles of rows, cards, panels and Markdown headings below the first (17 semibold).
        static let heading = SwiftUI.Font.headline
        /// Messages, replies, mail and documents (17).
        static let body = SwiftUI.Font.body
        /// Previews, subtitles, descriptions in cards (15).
        static let secondary = SwiftUI.Font.subheadline
        /// Times, places, states, meta lines (13).
        static let caption = SwiftUI.Font.footnote
        /// A list's section header: the caption's size, semibold, in the secondary color (13).
        static let sectionHeader = SwiftUI.Font.footnote.weight(.semibold)
        /// Chips, badges, counts, small labels on controls (12).
        static let label = SwiftUI.Font.caption
        /// Code, diffs, paths, terminal snippets (13).
        static let code = SwiftUI.Font.footnote.monospaced()

        // Emphasis inside a level: views use these, never `.weight(` on a level (TypeScaleTests). Strong is semibold,
        // the usual emphasis; medium and heavy (bold) are the weights in use today, kept so nothing moves.
        static let bodyStrong = body.weight(.semibold)
        static let bodyHeavy = body.weight(.bold)
        static let secondaryStrong = secondary.weight(.semibold)
        static let secondaryMedium = secondary.weight(.medium)
        static let captionStrong = caption.weight(.semibold)
        static let captionMedium = caption.weight(.medium)
        static let captionHeavy = caption.weight(.bold)
        static let labelStrong = label.weight(.semibold)
        static let labelMedium = label.weight(.medium)
        static let labelHeavy = label.weight(.bold)
    }

    /// The levels' sizes at the default text size and their line heights (a multiple of the size, as in CSS), for
    /// text that runs over several lines: `.lineHeight(_:)` turns them into line spacing at the current size.
    enum Level {
        case display, title, heading, body, secondary, caption, label, code

        var size: CGFloat {
            switch self {
            case .display: 28
            case .title: 22
            case .heading, .body: 17
            case .secondary: 15
            case .caption, .code: 13
            case .label: 12
            }
        }

        var lineHeight: CGFloat {
            switch self {
            case .display: 1.2
            case .title: 1.25
            case .heading: 1.3
            case .body: 1.45
            case .secondary: 1.4
            case .caption, .code: 1.35
            case .label: 1.3
            }
        }

        /// SF's own line height is about 1.19 × the size; line spacing adds the rest.
        var extraLeading: CGFloat { max(0, lineHeight - 1.19) * size }
    }

    /// The watch's list rows: rounded cards with room around and inside them, like the system's lists (rows at least
    /// 44 pt). The phone does not use these.
    enum Watch {
        /// Between two cards, as seen (it was 5).
        static let cardGap: CGFloat = 10
        /// What watchOS's List already leaves between two rows (measured on a 46 mm simulator): the cards add the rest.
        static let listRowSpacing: CGFloat = 5
        /// Inside a card, around its lines.
        static let cardPadding = EdgeInsets(top: 10, leading: 12, bottom: 10, trailing: 12)
        /// Between a row's lines (kind, title, place).
        static let lineSpacing: CGFloat = 4
        static let minRowHeight: CGFloat = 44
        static let cardRadius: CGFloat = 16
        /// The system list rows' gray.
        static let cardFill = Color(red: 0x22 / 255, green: 0x22 / 255, blue: 0x23 / 255)
        /// Blocks on a detail page (the question, its options, the buttons).
        static let blockSpacing: CGFloat = 10
        /// Inside a message bubble.
        static let bubblePadding: CGFloat = 8
    }

    /// Sizes of what is drawn next to text, at the default text size. Use them with `scaledFrame` so they follow
    /// 小 / 标准 / 大 and the system's text size (up to 1.6 ×, `Theme.sizeScale`); the lines stay as they are, in a
    /// plain `.frame`. Nothing on a page writes a size as a number (`TokenRatchetTests`).
    enum Size {
        // Lines, rules and bars: do not scale.
        static let hairline: CGFloat = 0.5
        static let line: CGFloat = 1
        static let lineMedium: CGFloat = 1.5
        static let rule: CGFloat = 2
        static let ruleWide: CGFloat = 2.5
        static let bar: CGFloat = 3
        static let barWide: CGFloat = 4
        // Dots and small marks.
        static let grabber: CGFloat = 5
        static let dotXS: CGFloat = 6
        static let dotS: CGFloat = 7
        static let dot: CGFloat = 8
        static let dotL: CGFloat = 9
        static let dotXL: CGFloat = 10
        static let markXS: CGFloat = 12
        static let markS: CGFloat = 14
        static let mark: CGFloat = 15
        static let markM: CGFloat = 16
        static let markL: CGFloat = 17
        static let badge: CGFloat = 18
        // Icons and the wells behind them.
        static let iconXS: CGFloat = 20
        static let iconS: CGFloat = 22
        static let icon: CGFloat = 24
        static let iconL: CGFloat = 26
        static let wellXS: CGFloat = 28
        static let wellS: CGFloat = 30
        static let well: CGFloat = 32
        static let wellM: CGFloat = 34
        static let wellL: CGFloat = 36
        static let wellXL: CGFloat = 38
        // Faces and touch.
        static let avatar: CGFloat = 40
        static let avatarM: CGFloat = 42
        /// The smallest thing a finger should hit.
        static let tapTarget: CGFloat = 44
        static let avatarL: CGFloat = 46
        static let avatarXL: CGFloat = 48
        // Boxes: label columns, thumbnails, charts, previews and panels, by their size at the default text size.
        static let box50: CGFloat = 50
        static let box56: CGFloat = 56
        static let box58: CGFloat = 58
        static let box60: CGFloat = 60
        static let box64: CGFloat = 64
        static let box72: CGFloat = 72
        static let box76: CGFloat = 76
        static let box80: CGFloat = 80
        static let box110: CGFloat = 110
        static let box120: CGFloat = 120
        static let box140: CGFloat = 140
        static let box150: CGFloat = 150
        static let box180: CGFloat = 180
        static let box200: CGFloat = 200
        static let box240: CGFloat = 240
        static let box260: CGFloat = 260
        static let box280: CGFloat = 280
        static let box300: CGFloat = 300
        static let box380: CGFloat = 380
        // Wide screens (iPad).
        /// A page meant to be read, not a list (the welcome page): no wider than this, centered.
        static let readable: CGFloat = 560
        /// The list column of a split on iPad: wide enough for the agents' cards and the inbox rows.
        static let sidebarMin: CGFloat = 340
        static let sidebar: CGFloat = 400
        static let sidebarMax: CGFloat = 480
    }

    /// The system's sidebar column on iPad (iOS 26, the glass styles), measured from its pixels: the split fills the
    /// status bar above the column with it (`SidebarTopFill`), which the column itself cannot draw into.
    static let systemSidebar = Color(light: 0xE6E9EE, dark: 0x191C20)

    /// Motion: a state change snaps, a layout change glides. Nothing repeats except the 需要你 dot, the work spinner and
    /// the persona's dot in the Jarvis tab's top bar (docs/persona.md), and those stop with Reduce Motion (the spinner
    /// is the system's).
    enum Motion {
        static let state = Animation.snappy(duration: 0.25)
        static let layout = Animation.smooth(duration: 0.3)
        /// Opening and closing things (a work card, a menu, a section): a state change, 0.25 s (it was the system's
        /// default snappy, 0.5 s, which trailed behind the finger).
        static let standard = state
        /// One way of a blink (the built-in persona's eyes): a blink is shut and open again, well under a quarter second.
        static let blink = Animation.easeInOut(duration: 0.07)
        /// What opens in the chat shows itself (a mask growing down from its header, and a fade); its height does not
        /// animate (`unfolding`). Closing folds it away first, quicker, then removes it.
        static let revealTime = 0.28
        static let concealTime = 0.2
        static let reveal = Animation.easeOut(duration: revealTime)
        static let conceal = Animation.easeIn(duration: concealTime)
        /// A drawer let go by the finger (the Jarvis tab's sidebar): no bounce, carrying on at the finger's speed.
        /// `velocity` is the share of the remaining way covered per second when it lets go (0 from a button).
        static func drawer(velocity: Double = 0) -> Animation {
            .interpolatingSpring(duration: 0.32, bounce: 0, initialVelocity: velocity)
        }
    }

    /// Categories with no meaning beyond telling them apart (memory types).
    enum Hue {
        static let blue = Color(light: 0x2E6FD0, dark: 0x5B9BFF)
        static let orange = Color(light: 0xB35400, dark: 0xFF9F0A)
        static let green = Color(light: 0x1D7A4C, dark: 0x4CC38A)
        static let purple = Color(light: 0x7A5AF8, dark: 0x9D8BFF)
    }

    /// The user's own messages.
    static var userBubble: Color { spec.userBubble }
    static func userBubble(_ style: AppStyle) -> Color { style.spec.tinted(PersonaPalette.current).userBubble }
    static var userBubbleText: Color { spec.userBubbleText }
    /// Light bubbles (Feishu's) are flat; colored ones get a gentle gradient that only darkens toward the bottom
    /// (`Color.gradient` lightened the top, and the white words on it fell to 3:1).
    static var userBubbleFill: AnyShapeStyle {
        spec.deepensBubble ? AnyShapeStyle(deepening(userBubble)) : AnyShapeStyle(userBubble)
    }

    /// A color that darkens a little toward the bottom: depth without lightening what words sit on.
    static func deepening(_ c: Color) -> LinearGradient {
        LinearGradient(colors: [c, c.mix(with: .black, by: 0.12)], startPoint: .top, endPoint: .bottom)
    }

    /// The ChatGPT-style shell (docs/ios.md「新外壳」, iPhone without 旧布局): neutral like ChatGPT's, in every style.
    enum Shell {
        /// The user's own messages: a soft gray bubble with ordinary text, not the style's colored one.
        static let userBubble = Color(light: 0xF2F2F2, dark: 0x2F2F2F)
        /// The 助手 | Agents switch's selected segment, inside its glass capsule.
        static let segment = Color(light: 0xE6E6E6, dark: 0x3A3A3C)
        /// The composer's field and its expanded card.
        static let composer = Color(light: 0xFFFFFF, dark: 0x1F1F21)
        static let composerStroke = Color(light: 0xE3E3E3, dark: 0x3A3A3C)
        /// Send / dictation's round button: ink on paper, like ChatGPT's (the style's accent tints nothing here).
        static let ink = Color(light: 0x0D0D0D, dark: 0xF2F2F2)
        static let inkText = Color(light: 0xFFFFFF, dark: 0x0D0D0D)
        /// The drawer behind the page.
        static let drawer = Color(light: 0xF9F9F9, dark: 0x171717)
        /// The 新对话 pill at the drawer's foot.
        static let newChat = Color(light: 0x2F6FED, dark: 0x3B7BF2)
        /// The composer: its field's corner when it is one line (a capsule) and when it has grown into a card.
        static let composerRadius: CGFloat = 28
        /// The switch's segments: width of each, and its height.
        static let segmentWidth: CGFloat = 72
        static let switchHeight: CGFloat = 44
    }

    /// The page behind lists and chats.
    static var pageBackground: Color { spec.page }
    static func pageBackground(_ style: AppStyle) -> Color { style.spec.page }
    /// Cards / panels in styles without glass.
    static var card: Color { spec.card }
    static func card(_ style: AppStyle) -> Color { style.spec.card }
    static var cardStroke: Color { spec.cardStroke }

    static let codeBackground = Color(light: 0xF3F1EC, dark: 0x1C1D21)

    /// Syntax colors in the file viewer (docs/ios.md「文件查看器」), on `codeBackground` in every style (it does not
    /// change with the style, so neither do they): GitHub's palette, every one ≥ 4.5:1 on that background in light
    /// and dark (`SyntaxPaletteTests`). Words that are none of these stay `.primary`.
    enum Syntax {
        static let keyword = Color(light: 0xCF222E, dark: 0xFF7B72)
        /// Types, and variables in languages that mark them ($HOME, @name).
        static let type = Color(light: 0x953800, dark: 0xFFA657)
        static let string = Color(light: 0x0A3069, dark: 0xA5D6FF)
        /// Numbers and constants (true, nil), links in Markdown.
        static let number = Color(light: 0x0550AE, dark: 0x79C0FF)
        static let function = Color(light: 0x7547CF, dark: 0xD2A8FF)
        static let comment = Color(light: 0x57606A, dark: 0x8B949E)
        /// Markup tags and keys: HTML tags, JSON / YAML / TOML keys, CSS properties.
        static let tag = Color(light: 0x116329, dark: 0x7EE787)
        /// Decorators, annotations, preprocessor lines, HTML attribute names.
        static let attribute = Color(light: 0x7D4E00, dark: 0xD29922)
        static let all: [(String, Color)] = [("keyword", keyword), ("type", type), ("string", string), ("number", number),
                                             ("function", function), ("comment", comment), ("tag", tag), ("attribute", attribute)]
    }
    static let terminalBackground = Color(light: 0x16171B, dark: 0x0E0F12)
    static let terminalText = Color(light: 0xE6E3DA, dark: 0xE6E3DA)
    // Agent states (docs/agent-states.md): the dots. Text and soft backgrounds come from `AgentState.tone`
    // (StatusPalette.swift, shared with the watch and the Live Activity).
    static var needsYou: Color { StatusPalette.needsYou.dot }
    static var busy: Color { StatusPalette.working.dot }
    static var unread: Color { StatusPalette.unread.dot }
}

extension AgentState {
    var label: String {
        switch self {
        case .needsYou: "需要你"
        case .working: "工作中"
        case .unread: "完成未看"
        case .idle: "已看"
        case .offline: "未运行"
        case .unknown: "未知"
        }
    }

    /// Red needs you, amber works, green finished-unseen, gray seen — same meaning as herdr's sidebar dots.
    var tone: StatusTone {
        switch self {
        case .needsYou: StatusPalette.needsYou
        case .working: StatusPalette.working
        case .unread: StatusPalette.unread
        case .idle, .unknown: StatusPalette.idle
        case .offline: StatusPalette.offline
        }
    }

    var color: Color { tone.dot }
}

extension SessionStatus {
    var label: String {
        switch self {
        case .working: "工作中"
        case .blocked: "等你处理"
        case .idle: "空闲"
        case .done: "已完成"
        case .unknown: "未知"
        case .offline: "未运行"
        }
    }
}

nonisolated enum Fmt {
    static func relative(_ date: Date?, now: Date = .now) -> String {
        guard let date, date.isMeaningful else { return "" }
        let s = now.timeIntervalSince(date)
        if s < 45 { return "刚刚" }
        if s < 3600 { return "\(max(1, Int(s / 60))) 分钟前" }
        let cal = Calendar.current
        if cal.isDateInToday(date) { return date.formatted(.dateTime.hour().minute()) }
        if cal.isDateInYesterday(date) { return "昨天" }
        if s < 6 * 86400 { return date.formatted(.dateTime.weekday(.wide)) }
        return date.formatted(.dateTime.month().day())
    }

    /// Shortens ~/ paths for display.
    static func path(_ p: String, home: String?) -> String {
        if let home, !home.isEmpty, p.hasPrefix(home) { return "~" + p.dropFirst(home.count) }
        for prefix in ["/Users/", "/home/", "/data00/home/"] where p.hasPrefix(prefix) {
            let rest = p.dropFirst(prefix.count)
            if let slash = rest.firstIndex(of: "/") { return "~" + rest[slash...] }
            return "~"
        }
        return p
    }

    /// Markdown → plain one-liner for previews.
    private static let plainMemo = TextMemo<String>()

    static func plain(_ s: String) -> String {
        plainMemo(s) { s in
            var t = s
            for token in ["**", "__", "`", "### ", "## ", "# ", "> "] { t = t.replacingOccurrences(of: token, with: "") }
            return t
        }
    }

    /// Model names like "deepseek/v4.1-flash-chain" → "v4.1-flash-chain".
    static func model(_ m: String?) -> String? {
        guard let m, !m.isEmpty else { return nil }
        return m.split(separator: "/").last.map(String.init) ?? m
    }
}
