import SwiftUI
import Testing
import UIKit
@testable import Jarvis

/// Every style's spec against the readability floors in docs/ios.md (设计语言): white words on the accent (solid
/// buttons, selected chips) ≥ 3.5:1, the words on the user's own bubble ≥ 4.5:1, the accent as a link on the page
/// ≥ 3:1. In light and in dark.
struct StyleSpecTests {
    private static func rgb(_ c: Color, _ style: UIUserInterfaceStyle) -> (Double, Double, Double) {
        let u = UIColor(c).resolvedColor(with: UITraitCollection(userInterfaceStyle: style))
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        u.getRed(&r, green: &g, blue: &b, alpha: &a)
        return (Double(r), Double(g), Double(b))
    }

    private static func luminance(_ c: (Double, Double, Double)) -> Double {
        func lin(_ v: Double) -> Double { v <= 0.03928 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4) }
        return 0.2126 * lin(c.0) + 0.7152 * lin(c.1) + 0.0722 * lin(c.2)
    }

    static func contrast(_ a: Color, _ b: Color, _ style: UIUserInterfaceStyle) -> Double {
        let la = luminance(rgb(a, style)), lb = luminance(rgb(b, style))
        return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
    }

    private static let appearances: [(String, UIUserInterfaceStyle)] = [("light", .light), ("dark", .dark)]

    private static func below(_ floor: Double, _ pair: (StyleSpec) -> (Color, Color)) -> [String] {
        AppStyle.allCases.flatMap { style in
            appearances.compactMap { name, ui in
                let (a, b) = pair(style.spec)
                let c = contrast(a, b, ui)
                return c < floor ? "\(style.rawValue) \(name): \(String(format: "%.2f", c))" : nil
            }
        }
    }

    /// Below a floor on purpose: 飞书's dark blue is 飞书's own (#4C88FF), 3.35:1 under white.
    private static let known: Set<String> = ["lark dark"]

    @Test func whiteOnTheAccent() {
        let found = Self.below(3.5) { (.white, $0.accent) }.filter { !Self.known.contains(where: $0.hasPrefix) }
        #expect(found.isEmpty, "white on the accent under 3.5:1: \(found)")
    }

    @Test func wordsOnTheUsersBubble() {
        let found = Self.below(4.5) { ($0.userBubbleText, $0.userBubble) }
        #expect(found.isEmpty, "bubble text under 4.5:1: \(found)")
    }

    @Test func theAccentAsALink() {
        let found = Self.below(3) { ($0.accent, $0.page) }
        #expect(found.isEmpty, "the accent on the page under 3:1: \(found)")
    }

    /// Titles name the styles in the picker; two alike would be one style twice.
    @Test func everyStyleIsItsOwn() {
        #expect(Set(AppStyle.allCases.map(\.title)).count == AppStyle.allCases.count)
    }
}
