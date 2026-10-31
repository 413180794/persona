import SwiftUI

/// Spacing on the site's 4 pt grid (`--spacing: .25rem`); `BUISpacing.s(2.5)` is 10 pt.
public enum BUISpacing {
    public static func s(_ steps: CGFloat) -> CGFloat { steps * 4 }

    public static let hairline: CGFloat = 1
    public static let xxs: CGFloat = 2
    public static let xs: CGFloat = 4
    public static let sm: CGFloat = 6
    public static let md: CGFloat = 8
    public static let lg: CGFloat = 12
    public static let xl: CGFloat = 16
    public static let xxl: CGFloat = 24
}

/// Corner radii (`--radius-*`).
public struct BUIRadii: Sendable {
    /// 4 — small tags and checkboxes.
    public var small: CGFloat
    /// 6 — chips.
    public var chip: CGFloat
    /// 8 — buttons, inputs, rows.
    public var control: CGFloat
    /// 10 — cards.
    public var card: CGFloat
    /// 14 — windows and large panels.
    public var window: CGFloat

    public init(small: CGFloat = 4, chip: CGFloat = 6, control: CGFloat = 8, card: CGFloat = 10, window: CGFloat = 14) {
        self.small = small
        self.chip = chip
        self.control = control
        self.card = card
        self.window = window
    }

    public static let standard = BUIRadii()
}
