import UIKit

/// The terminal's colors: background, text, cursor, selection and the 16 ANSI colors (16–255 are the standard xterm
/// cube and grays). Each theme has a light and a dark side and follows the system unless it is dark-only.
nonisolated struct TerminalTheme: Sendable {
    struct Side: Sendable {
        var background: UInt32
        var foreground: UInt32
        var cursor: UInt32
        var selection: UInt32
        var ansi: [UInt32] // 16
    }

    var id: String
    var name: String
    var light: Side
    var dark: Side

    func side(_ dark: Bool) -> Side { dark ? self.dark : light }

    static let jarvis = TerminalTheme(
        id: "jarvis", name: "Jarvis",
        light: Side(background: 0xFBFAF7, foreground: 0x1F2328, cursor: 0x0A84FF, selection: 0x0A84FF,
                    ansi: [0x24292F, 0xCF222E, 0x116329, 0x9A6700, 0x0550AE, 0x8250DF, 0x1B7C83, 0x6E7781,
                           0x57606A, 0xA40E26, 0x1A7F37, 0x7D4E00, 0x0969DA, 0xA475F9, 0x3192AA, 0x8C959F]),
        dark: Side(background: 0x16171B, foreground: 0xE6E3DA, cursor: 0x4DA3FF, selection: 0x4DA3FF,
                   ansi: [0x3B3F4A, 0xF0646E, 0x8FD18B, 0xE5C07B, 0x61AFEF, 0xC678DD, 0x56B6C2, 0xC8CCD4,
                          0x5C6370, 0xFF7B85, 0xA5E6A0, 0xF2D28E, 0x82C4FF, 0xDA9AEE, 0x7FD3DD, 0xF5F5F5]))

    /// Solarized, dark and light.
    static let solarized = TerminalTheme(
        id: "solarized", name: "Solarized",
        light: Side(background: 0xFDF6E3, foreground: 0x586E75, cursor: 0x268BD2, selection: 0x268BD2,
                    ansi: [0x073642, 0xDC322F, 0x859900, 0xB58900, 0x268BD2, 0xD33682, 0x2AA198, 0xEEE8D5,
                           0x002B36, 0xCB4B16, 0x586E75, 0x657B83, 0x839496, 0x6C71C4, 0x93A1A1, 0xFDF6E3]),
        dark: Side(background: 0x002B36, foreground: 0x93A1A1, cursor: 0x268BD2, selection: 0x268BD2,
                   ansi: [0x073642, 0xDC322F, 0x859900, 0xB58900, 0x268BD2, 0xD33682, 0x2AA198, 0xEEE8D5,
                          0x002B36, 0xCB4B16, 0x586E75, 0x657B83, 0x839496, 0x6C71C4, 0x93A1A1, 0xFDF6E3]))

    /// Always dark, the classic terminal look (the tab's look before themes).
    static let midnight = TerminalTheme(id: "midnight", name: "深夜", light: jarvis.dark, dark: jarvis.dark)

    static let all = [jarvis, solarized, midnight]
    static func named(_ id: String) -> TerminalTheme { all.first { $0.id == id } ?? jarvis }
}

extension TerminalTheme.Side {
    func color(_ c: TermColor, foreground fg: Bool) -> UIColor {
        switch c {
        case .standard: return UIColor(hex: fg ? foreground : background)
        case .indexed(let i): return UIColor(hex: Self.indexed(Int(i), ansi: ansi))
        case .rgb(let r, let g, let b): return UIColor(red: CGFloat(r) / 255, green: CGFloat(g) / 255, blue: CGFloat(b) / 255, alpha: 1)
        }
    }

    /// xterm's 256 colors: 16 from the theme, a 6×6×6 cube, 24 grays.
    static func indexed(_ i: Int, ansi: [UInt32]) -> UInt32 {
        if i < 16 { return ansi[i] }
        if i < 232 {
            let n = i - 16
            let level = { (v: Int) -> UInt32 in v == 0 ? 0 : UInt32(55 + v * 40) }
            return level(n / 36) << 16 | level(n / 6 % 6) << 8 | level(n % 6)
        }
        let g = UInt32(8 + (i - 232) * 10)
        return g << 16 | g << 8 | g
    }
}
