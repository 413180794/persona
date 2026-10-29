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

    nonisolated(unsafe) static var current: AppStyle =
        AppStyle(rawValue: UserDefaults.standard.string(forKey: "ui.style") ?? "") ?? .glass

    var id: String { rawValue }

    var title: String {
        switch self {
        case .glass: "液态玻璃"
        case .claude: "Claude"
        case .clay: "黏土"
        case .island: "Island"
        case .lark: "飞书"
        }
    }

    var subtitle: String {
        switch self {
        case .glass: "iOS 原生，简洁"
        case .claude: "原来的珊瑚橙"
        case .clay: "柔和、圆润"
        case .island: "悬浮卡片"
        case .lark: "和飞书一致"
        }
    }

    /// Liquid Glass surfaces and buttons; the other styles draw solid ones of their own.
    var usesGlass: Bool { self == .glass || self == .claude }
    var fontDesign: Font.Design { self == .clay ? .rounded : .default }
    /// Corner radius of cards / panels.
    var corner: CGFloat {
        switch self {
        case .glass, .claude: 20
        case .clay: 24
        case .island: 26
        case .lark: 10
        }
    }
}

nonisolated enum Theme {
    private static var style: AppStyle { AppStyle.current }
    #if os(watchOS)
    private static let systemPage = Color.black
    private static let systemCard = Color(white: 0.12)
    #else
    private static let systemPage = Color(uiColor: .systemBackground)
    private static let systemCard = Color(uiColor: .secondarySystemBackground)
    #endif

    static var accent: Color { accent(style) }
    static func accent(_ style: AppStyle) -> Color {
        switch style {
        case .glass: Color(light: 0x007AFF, dark: 0x0A84FF)
        // Dark accents deep enough for white words on solid buttons and selected chips (≥ 3.5:1, like system blue's
        // 3.65); Island's teal leans cyan and darker, away from the 完成未看 green and readable as a link on its mist.
        case .claude: Color(light: 0xD4683F, dark: 0xCD6E45)
        case .clay: Color(light: 0x7A5AF8, dark: 0x8979EB)
        case .island: Color(light: 0x27747F, dark: 0x2A9798)
        case .lark: Color(light: 0x3370FF, dark: 0x4C88FF)
        }
    }
    /// The app icon's coral. Only for Jarvis' own mark (the glyph in 设置 / 对话, the Live Activity's star), never for
    /// something to tap: that is `accent`, whatever the style.
    static let brand = Color(light: 0xD4683F, dark: 0xE8835C)
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
    }

    /// Spacing steps. Pages and the terminal card keep 16 from the screen's edges.
    enum Space {
        static let xxs: CGFloat = 2
        static let xs: CGFloat = 4
        static let s: CGFloat = 8
        static let m: CGFloat = 12
        static let l: CGFloat = 16
        static let xl: CGFloat = 20
        static let xxl: CGFloat = 24
        static let xxxl: CGFloat = 32
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

    /// Type roles, all Dynamic Type text styles (the size at the default setting in the comment). The terminal grid
    /// (its own size setting) and the Live Activity (system limits) are the exceptions.
    enum Font {
        /// Panels and sheets (20).
        static let panelTitle = SwiftUI.Font.title3.weight(.semibold)
        /// A row's title (17); bold for 完成未看.
        static let rowTitle = SwiftUI.Font.headline
        /// Messages, Markdown and the user's bubble (17).
        static let body = SwiftUI.Font.body
        /// Text in cards (16).
        static let cardBody = SwiftUI.Font.callout
        /// A row's preview, tool rows (15).
        static let preview = SwiftUI.Font.subheadline
        /// Times, machines, state words (13).
        static let meta = SwiftUI.Font.footnote
        /// Labels and counts (12).
        static let chip = SwiftUI.Font.caption.weight(.medium)
        /// The smallest, for badges only (11).
        static let micro = SwiftUI.Font.caption2
        /// Code, diffs, paths (13 / 12).
        static let code = SwiftUI.Font.footnote.monospaced()
        static let codeSmall = SwiftUI.Font.caption.monospaced()
        /// The assistant's greeting (34).
        static let greeting = SwiftUI.Font.largeTitle.bold()
    }

    /// Motion: a state change snaps, a layout change glides. Nothing repeats except the 需要你 dot and the work spinner,
    /// and those stop with Reduce Motion (the spinner is the system's).
    enum Motion {
        static let state = Animation.snappy(duration: 0.25)
        static let layout = Animation.smooth(duration: 0.3)
    }

    /// Categories with no meaning beyond telling them apart (memory types).
    enum Hue {
        static let blue = Color(light: 0x2E6FD0, dark: 0x5B9BFF)
        static let orange = Color(light: 0xB35400, dark: 0xFF9F0A)
        static let green = Color(light: 0x1D7A4C, dark: 0x4CC38A)
        static let purple = Color(light: 0x7A5AF8, dark: 0x9D8BFF)
    }

    /// The user's own messages.
    static var userBubble: Color { userBubble(style) }
    static func userBubble(_ style: AppStyle) -> Color {
        switch style {
        case .glass: Color(light: 0xDCEAFF, dark: 0x1D3557) // a soft tint: a solid system blue was too heavy
        // White words on the colored bubbles need ≥ 4.5:1 (body text): deeper than the accents.
        case .claude: Color(light: 0xB24C25, dark: 0xAF4C28)
        case .clay: Color(light: 0x6759D3, dark: 0x6756D9)
        case .island: Color(light: 0x14201E, dark: 0xE8F3F1)
        case .lark: Color(light: 0xD1E3FF, dark: 0x133063)
        }
    }
    static var userBubbleText: Color {
        switch style {
        case .island: Color(light: 0xFFFFFF, dark: 0x0B1211)
        case .glass, .lark: .primary
        default: .white
        }
    }
    /// Light bubbles (Feishu's) are flat; colored ones get a gentle gradient that only darkens toward the bottom
    /// (`Color.gradient` lightened the top, and the white words on it fell to 3:1).
    static var userBubbleFill: AnyShapeStyle {
        style == .lark || style == .glass ? AnyShapeStyle(userBubble) : AnyShapeStyle(deepening(userBubble))
    }

    /// A color that darkens a little toward the bottom: depth without lightening what words sit on.
    static func deepening(_ c: Color) -> LinearGradient {
        LinearGradient(colors: [c, c.mix(with: .black, by: 0.12)], startPoint: .top, endPoint: .bottom)
    }

    /// The page behind lists and chats.
    static var pageBackground: Color { pageBackground(style) }
    static func pageBackground(_ style: AppStyle) -> Color {
        switch style {
        case .glass, .claude: systemPage
        // Clay's dark canvas sits under the system's grouped cells (#1C1C1E): at #1D1A23 they were one color (1.01:1).
        case .clay: Color(light: 0xEFEAF7, dark: 0x0A0510)
        case .island: Color(light: 0xE6F1EF, dark: 0x0A1110)
        case .lark: Color(light: 0xF5F6F7, dark: 0x141414)
        }
    }
    /// Cards / panels in styles without glass.
    static var card: Color { card(style) }
    static func card(_ style: AppStyle) -> Color {
        switch style {
        case .glass, .claude: systemCard
        case .clay: Color(light: 0xF8F5FD, dark: 0x2A2533)
        case .island: Color(light: 0xFFFFFF, dark: 0x16201E)
        case .lark: Color(light: 0xFFFFFF, dark: 0x1F1F1F)
        }
    }
    static var cardStroke: Color { style == .lark ? Color(light: 0xDEE0E3, dark: 0x373A40) : Color.primary.opacity(0.08) }

    static let codeBackground = Color(light: 0xF3F1EC, dark: 0x1C1D21)
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
    static func plain(_ s: String) -> String {
        var t = s
        for token in ["**", "__", "`", "### ", "## ", "# ", "> "] { t = t.replacingOccurrences(of: token, with: "") }
        return t
    }

    /// Model names like "deepseek/v4.1-flash-chain" → "v4.1-flash-chain".
    static func model(_ m: String?) -> String? {
        guard let m, !m.isEmpty else { return nil }
        return m.split(separator: "/").last.map(String.init) ?? m
    }
}
