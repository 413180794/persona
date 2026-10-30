import SwiftUI

/// The small line icons the original draws inline as 24×24 SVGs, kept as their SVG path data
/// so weight and proportions match instead of the nearest SF Symbol.
public struct BUIGlyph: Sendable, Hashable {
    /// SVG path data in a 24×24 box.
    public var d: String
    /// Filled glyphs ignore the line width.
    public var filled: Bool

    public init(_ d: String, filled: Bool = false) {
        self.d = d
        self.filled = filled
    }

    /// `<circle>` as path data.
    public static func circle(_ cx: Double, _ cy: Double, _ r: Double) -> String {
        "M\(cx - r) \(cy)a\(r) \(r) 0 1 0 \(2 * r) 0a\(r) \(r) 0 1 0 \(-2 * r) 0"
    }

    /// `<rect>` as path data.
    public static func rect(_ x: Double, _ y: Double, _ w: Double, _ h: Double, rx: Double = 0) -> String {
        guard rx > 0 else { return "M\(x) \(y)h\(w)v\(h)h\(-w)z" }
        return "M\(x + rx) \(y)h\(w - 2 * rx)a\(rx) \(rx) 0 0 1 \(rx) \(rx)v\(h - 2 * rx)a\(rx) \(rx) 0 0 1 \(-rx) \(rx)"
            + "h\(-(w - 2 * rx))a\(rx) \(rx) 0 0 1 \(-rx) \(-rx)v\(-(h - 2 * rx))a\(rx) \(rx) 0 0 1 \(rx) \(-rx)z"
    }
}

public extension BUIGlyph {
    static let sparkle = BUIGlyph("M12 2l2.4 7.2L22 12l-7.6 2.8L12 22l-2.4-7.2L2 12l7.6-2.8z", filled: true)
    static let chevronDown = BUIGlyph("M6 9l6 6 6-6")
    static let chevronRight = BUIGlyph("M9 6l6 6-6 6")
    static let check = BUIGlyph("M20 6L9 17l-5-5")
    static let magnifier = BUIGlyph(circle(11, 11, 7) + "M21 21l-4.3-4.3")
    static let globe = BUIGlyph(circle(12, 12, 9) + "M3.5 12h17M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18")
    static let close = BUIGlyph("M18 6L6 18M6 6l12 12")
    static let plus = BUIGlyph("M12 5v14M5 12h14")
    static let arrowUp = BUIGlyph("M12 19V5M5 12l7-7 7 7")
    static let arrowRight = BUIGlyph("M5 12h14M13 6l6 6-6 6")
    static let copy = BUIGlyph(rect(9, 9, 12, 12, rx: 2.5) + "M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1")
    static let retry = BUIGlyph("M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6")
    static let thumbUp = BUIGlyph("M7 10v12M15 5.88L14 10h5.83a2 2 0 0 1 1.92 2.56l-2.33 8A2 2 0 0 1 17.5 22H4a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2h2.76a2 2 0 0 0 1.79-1.11L12 2a3.13 3.13 0 0 1 3 3.88z")
    static let thumbDown = BUIGlyph("M17 14V2M9 18.12L10 14H4.17a2 2 0 0 1-1.92-2.56l2.33-8A2 2 0 0 1 6.5 2H20a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-2.76a2 2 0 0 0-1.79 1.11L12 22a3.13 3.13 0 0 1-3-3.88z")
    /// Corner-down-left, used on follow-up prompts.
    static let reply = BUIGlyph("M9 10l-5 5 5 5M20 4v7a4 4 0 0 1-4 4H4")
}

/// Draws a `BUIGlyph` (or any 24-box SVG path data) at `size` points with the original's
/// stroke width.
public struct BUIIcon: View {
    var glyph: BUIGlyph
    var size: CGFloat
    var lineWidth: CGFloat

    /// `lineWidth` is in the SVG's 24-unit space, as written in the original markup.
    public init(_ glyph: BUIGlyph, size: CGFloat = 14, lineWidth: CGFloat = 2) {
        self.glyph = glyph
        self.size = size
        self.lineWidth = lineWidth
    }

    public var body: some View {
        let unit = size / 24
        let path = SVGPath.path(glyph.d).applying(CGAffineTransform(scaleX: unit, y: unit))
        Group {
            if glyph.filled {
                path.fill()
            } else {
                path.stroke(style: StrokeStyle(lineWidth: lineWidth * unit, lineCap: .round, lineJoin: .round))
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}
