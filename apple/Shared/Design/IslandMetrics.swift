import SwiftUI

/// The Dynamic Island's sizes (docs/live-activity.md): the Fleet activity's pieces and the picture of it in 设置 › 通知 ›
/// 灵动岛. A table of its own because the widget extension builds this file but not `Theme`. The island's type has fixed
/// sizes: the system gives its regions in points, whatever the text size.
nonisolated enum IslandMetrics {
    // MARK: What the system gives (measured on the iOS 27 simulators, docs/live-activity.md「和其他 App 共用灵动岛」)

    /// A compact side limited in width (`isDynamicIslandLimitedInWidth`: another app's activity beside ours on iPhone 18
    /// Pro / Pro Max, landscape, iPhone Duo), and the minimal island in landscape: a 28 pt square, drawn as a circle.
    static let limited: CGFloat = 28
    /// What the glyph may take of it: the widest a line of the glyph's height can be inside the 28 pt circle.
    static let limitedContent: CGFloat = 24
    /// The minimal island (another app's activity beside ours on the other iPhones): a circle that grows into an oval
    /// with what it shows, up to this wide, of which this much shows (the rest is the system's margins; wider is cut).
    static let minimal: CGFloat = 45
    static let minimalContent: CGFloat = 35
    /// A compact side when the activity is alone: no width is offered, the island grows with what it shows, up to about
    /// this much on iPhone 17 / 17 Pro (more on iPhone 18 Pro, whose camera is narrower); wider is cut.
    static let compactContent: CGFloat = 94

    // MARK: Pieces

    /// A compact side's symbol and number.
    static let symbol: CGFloat = 13
    static let number: CGFloat = 15
    /// The minimal island's (and a limited side's) symbol and number.
    static let smallSymbol: CGFloat = 10
    static let smallNumber: CGFloat = 12
    /// 做完未看's dot, smaller than the other symbols.
    static let dot: CGFloat = 8
    static let smallDot: CGFloat = 6
    /// The assistant's star.
    static let star: CGFloat = 15
    static let smallStar: CGFloat = 12
    /// The timer's symbol and its digits, and the most the digits take (`Text(timerInterval:)` takes all it is given).
    static let timerSymbol: CGFloat = 11
    static let timerNumber: CGFloat = 13
    static let timerWidth: CGFloat = 40
    /// Between a symbol and its number.
    static let gap: CGFloat = 3
    static let smallGap: CGFloat = 1
    /// A compact side's inset from the island's rounded end.
    static let edge: CGFloat = 2
    /// The persona on the compact island's left side (docs/live-activity.md「形象」), and the gap to its count.
    static let face: CGFloat = 22
    static let faceGap: CGFloat = 4
    /// The ring around a count that has no room for its symbol.
    static let ring: CGFloat = 1.5

    // MARK: The settings page's picture of the island

    /// The pill, as wide as the island with both sides showing.
    static let previewWidth: CGFloat = 230
    static let previewHeight: CGFloat = 36
    /// Where the camera sits.
    static let previewCamera = CGSize(width: 70, height: 22)
    /// The sides' inset from the pill's ends.
    static let previewInset: CGFloat = 14
    /// The island when sharing: the pill holds only the camera and our glyph, another app's activity is a circle.
    static let previewSharedWidth: CGFloat = 140
    static let previewGap: CGFloat = 6
    /// The other app's activity in the picture: a gray stand-in for whatever it shows.
    static let previewOther: CGFloat = 14

    static func numberFont(small: Bool) -> Font {
        .system(size: small ? smallNumber : number, weight: .bold, design: .rounded)
    }

    static func symbolFont(small: Bool) -> Font {
        .system(size: small ? smallSymbol : symbol, weight: .bold)
    }
}
