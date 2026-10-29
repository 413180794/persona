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
        case .claude: Color(light: 0xD4683F, dark: 0xE8835C)
        case .clay: Color(light: 0x7A5AF8, dark: 0x9D8BFF)
        case .island: Color(light: 0x0E9F8E, dark: 0x2DD4BF)
        case .lark: Color(light: 0x3370FF, dark: 0x4C88FF)
        }
    }
    static var working: Color { accent }
    static let attention = Color(light: 0xE09A1F, dark: 0xF5B23D)
    static let idle = Color(light: 0x2E9E6E, dark: 0x45C38B)
    static let done = Color(light: 0x3B7BE0, dark: 0x5B9BFF)
    static let offline = Color.secondary
    static let danger = Color(light: 0xD93A3A, dark: 0xFF5F57)

    /// The user's own messages.
    static var userBubble: Color { userBubble(style) }
    static func userBubble(_ style: AppStyle) -> Color {
        switch style {
        case .glass: Color(light: 0xDCEAFF, dark: 0x1D3557) // a soft tint: a solid system blue was too heavy
        case .claude: Color(light: 0xD4683F, dark: 0xC9623C)
        case .clay: Color(light: 0x8E7BFA, dark: 0x6E5CE0)
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
    /// Light bubbles (Feishu's) are flat; colored ones get a gentle gradient.
    static var userBubbleFill: AnyShapeStyle {
        style == .lark || style == .glass ? AnyShapeStyle(userBubble) : AnyShapeStyle(userBubble.gradient)
    }

    /// The page behind lists and chats.
    static var pageBackground: Color { pageBackground(style) }
    static func pageBackground(_ style: AppStyle) -> Color {
        switch style {
        case .glass, .claude: systemPage
        case .clay: Color(light: 0xEFEAF7, dark: 0x1D1A23)
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
    // Agent states (docs/agent-states.md).
    static let needsYou = Color(light: 0xE5484D, dark: 0xFF6369)
    static let busy = Color(light: 0xE8890C, dark: 0xFFB224)
    static let unread = Color(light: 0x30A46C, dark: 0x4CC38A)
}

extension Color {
    nonisolated init(light: UInt32, dark: UInt32) {
        #if os(watchOS)
        self.init(hex: dark)
        #else
        self.init(uiColor: UIColor { $0.userInterfaceStyle == .dark ? UIColor(hex: dark) : UIColor(hex: light) })
        #endif
    }

    nonisolated init(hex: UInt32) {
        self.init(red: Double((hex >> 16) & 0xff) / 255, green: Double((hex >> 8) & 0xff) / 255, blue: Double(hex & 0xff) / 255)
    }
}

#if !os(watchOS)
extension UIColor {
    nonisolated convenience init(hex: UInt32) {
        self.init(red: CGFloat((hex >> 16) & 0xff) / 255, green: CGFloat((hex >> 8) & 0xff) / 255, blue: CGFloat(hex & 0xff) / 255, alpha: 1)
    }
}
#endif

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
    var color: Color {
        switch self {
        case .needsYou: Theme.needsYou
        case .working: Theme.busy
        case .unread: Theme.unread
        case .idle, .unknown: Color.secondary.opacity(0.55)
        case .offline: Color.secondary.opacity(0.3)
        }
    }

    var animates: Bool { self == .working || self == .needsYou }
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

    var color: Color {
        switch self {
        case .working: Theme.working
        case .blocked: Theme.attention
        case .idle: Theme.idle
        case .done: Theme.done
        case .unknown, .offline: Theme.offline
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
