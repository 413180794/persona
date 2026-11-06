import SwiftUI

/// The semantic palette from beautifului.dev (`:root` / `.dark` custom properties),
/// converted from oklch to sRGB. Every color resolves light/dark on its own.
public struct BUIColors: Sendable {
    /// App background behind everything (`--page`).
    public var page: Color
    /// Recessed area a component sits on (`--canvas`).
    public var canvas: Color
    /// Card and control fill (`--surface`).
    public var surface: Color
    /// Slightly recessed fill inside a card (`--inset`).
    public var inset: Color
    public var hover: Color
    public var hover2: Color
    /// Primary text (`--ink`).
    public var ink: Color
    /// Secondary text (`--ink-2`).
    public var ink2: Color
    /// Tertiary text, placeholders, meta (`--ink-3`).
    public var ink3: Color
    public var line: Color
    public var lineStrong: Color
    public var lineSoft: Color
    /// Text field and segmented-control track (`--field`).
    public var field: Color
    public var accent: Color
    public var accentInk: Color
    public var accentTint: Color
    public var green: Color
    public var greenTint: Color
    public var orange: Color
    public var orangeTint: Color
    public var red: Color
    public var redTint: Color
    public var tooltipBackground: Color
    public var tooltipForeground: Color
    public var tooltipMuted: Color
    public var tooltipBorder: Color
    /// Diagonal page stripes (`--stripe`, `--stripe-bg`).
    public var stripe: Color
    public var stripeBackground: Color

    public init(
        page: Color, canvas: Color, surface: Color, inset: Color, hover: Color, hover2: Color,
        ink: Color, ink2: Color, ink3: Color, line: Color, lineStrong: Color, lineSoft: Color,
        field: Color, accent: Color, accentInk: Color, accentTint: Color,
        green: Color, greenTint: Color, orange: Color, orangeTint: Color, red: Color, redTint: Color,
        tooltipBackground: Color, tooltipForeground: Color, tooltipMuted: Color, tooltipBorder: Color,
        stripe: Color, stripeBackground: Color
    ) {
        self.page = page; self.canvas = canvas; self.surface = surface; self.inset = inset
        self.hover = hover; self.hover2 = hover2
        self.ink = ink; self.ink2 = ink2; self.ink3 = ink3
        self.line = line; self.lineStrong = lineStrong; self.lineSoft = lineSoft
        self.field = field; self.accent = accent; self.accentInk = accentInk; self.accentTint = accentTint
        self.green = green; self.greenTint = greenTint; self.orange = orange; self.orangeTint = orangeTint
        self.red = red; self.redTint = redTint
        self.tooltipBackground = tooltipBackground; self.tooltipForeground = tooltipForeground
        self.tooltipMuted = tooltipMuted; self.tooltipBorder = tooltipBorder
        self.stripe = stripe; self.stripeBackground = stripeBackground
    }
}

public extension BUIColors {
    /// The original Beautiful UI palette.
    static let standard = BUIColors(
        page: Color(light: 0xFAFAFB, dark: 0x17181A),
        canvas: Color(light: 0xF1F2F3, dark: 0x1C1D1F),
        surface: Color(light: 0xFFFFFF, dark: 0x232427),
        inset: Color(light: 0xF7F8F9, dark: 0x1F2022),
        hover: Color(light: 0xF4F5F6, dark: 0x2A2B2E),
        hover2: Color(light: 0xE7E9EB, dark: 0x313236),
        ink: Color(light: 0x1F2124, dark: 0xF2F3F4),
        ink2: Color(light: 0x62656B, dark: 0xA5A8AD),
        ink3: Color(light: 0x9A9DA3, dark: 0x6C6F75),
        line: Color(light: 0xECEDEF, dark: 0x2E3033),
        lineStrong: Color(light: 0xE0E2E5, dark: 0x3A3C40),
        lineSoft: Color(light: 0xF3F4F5, dark: 0x27282B),
        field: Color(light: 0xF2F2F3, dark: 0x2B2C2F),
        accent: Color(light: 0x0285FF, dark: 0x3D9AFF),
        accentInk: Color(light: 0x0070DD, dark: 0x7EC0FF),
        accentTint: Color(light: 0xE9F3FF, dark: 0x3D9AFF, darkAlpha: 0.16),
        green: Color(light: 0x199A4D, dark: 0x3CBB72),
        greenTint: Color(light: 0xE8F5ED, dark: 0x3CBB72, darkAlpha: 0.14),
        orange: Color(light: 0xEF720D, dark: 0xF68F3C),
        orangeTint: Color(light: 0xFDF1E5, dark: 0xF68F3C, darkAlpha: 0.14),
        red: Color(light: 0xE3474C, dark: 0xEE5C61),
        redTint: Color(light: 0xFCECEC, dark: 0xEE5C61, darkAlpha: 0.14),
        tooltipBackground: Color(light: 0x25272B, dark: 0x111214),
        tooltipForeground: Color(light: 0xF6F7F8, dark: 0xF2F3F4),
        tooltipMuted: Color(light: 0xA5A8AD, dark: 0xA5A8AD),
        tooltipBorder: Color(light: 0x3A3C40, dark: 0x2E3033),
        stripe: Color(light: 0x494949, dark: 0xFFFFFF, lightAlpha: 0.075, darkAlpha: 0.055),
        stripeBackground: Color(light: 0xF5F5F5, dark: 0x1B1C1E)
    )
}

/// Internal on purpose: host apps (Persona included) tend to define the same initialisers,
/// and exporting these would make their calls ambiguous. Hosts build `BUIColors` from their
/// own colours instead.
extension Color {
    /// A color that resolves to `light` or `dark` with the current appearance.
    init(light: UInt32, dark: UInt32, lightAlpha: Double = 1, darkAlpha: Double = 1) {
        #if canImport(UIKit)
        self.init(uiColor: UIColor { traits in
            traits.userInterfaceStyle == .dark
                ? UIColor(hex: dark, alpha: darkAlpha)
                : UIColor(hex: light, alpha: lightAlpha)
        })
        #else
        self.init(nsColor: NSColor(name: nil) { appearance in
            appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
                ? NSColor(hex: dark, alpha: darkAlpha)
                : NSColor(hex: light, alpha: lightAlpha)
        })
        #endif
    }

    /// A fixed sRGB color from `0xRRGGBB`.
    init(hex: UInt32, alpha: Double = 1) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255,
            opacity: alpha
        )
    }
}

#if canImport(UIKit)
extension UIColor {
    convenience init(hex: UInt32, alpha: Double) {
        self.init(
            red: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: alpha
        )
    }
}
#else
extension NSColor {
    convenience init(hex: UInt32, alpha: Double) {
        self.init(
            srgbRed: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: alpha
        )
    }
}
#endif
