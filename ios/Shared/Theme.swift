import SwiftUI

nonisolated enum Theme {
    /// Claude's terracotta, slightly brighter in dark mode.
    static let accent = Color(light: 0xD4683F, dark: 0xE8835C)
    static let working = Color(light: 0xD4683F, dark: 0xE8835C)
    static let attention = Color(light: 0xE09A1F, dark: 0xF5B23D)
    static let idle = Color(light: 0x2E9E6E, dark: 0x45C38B)
    static let done = Color(light: 0x3B7BE0, dark: 0x5B9BFF)
    static let offline = Color.secondary
    static let danger = Color(light: 0xD93A3A, dark: 0xFF5F57)
    static let userBubble = Color(light: 0xD4683F, dark: 0xC9623C)
    static let codeBackground = Color(light: 0xF3F1EC, dark: 0x1C1D21)
    static let terminalBackground = Color(light: 0x16171B, dark: 0x0E0F12)
    static let terminalText = Color(light: 0xE6E3DA, dark: 0xE6E3DA)
    static let cardStroke = Color.primary.opacity(0.08)
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
        if s < 3600 { return "\(Int(s / 60)) 分钟前" }
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
