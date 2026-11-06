import BeautifulUI
import SwiftUI

/// Another product's own mark (`Theme.Brand`) in a square box of `side` points (scaled with the text size): every place
/// that shows a connector's or a provider's logo draws it with this, so Lark, GitHub, Telegram, Gmail, Microsoft… look
/// the same size and sit on the same center, whatever shape their artwork has (`Theme.Brand.optical`). An iPhone app's
/// icon fills the box as an app icon, in its squircle with a hairline edge (the white ones vanish on a white card
/// otherwise). A template mark (GitHub's) takes the foreground color, as an SF Symbol does.
struct BrandMark: View {
    var asset: String
    var side: CGFloat
    /// false: `side` is already the size to draw (a BeautifulUI row sizes its tiles with its own type scale).
    var scaled = true

    init(_ asset: String, side: CGFloat, scaled: Bool = true) {
        self.asset = asset
        self.side = side
        self.scaled = scaled
    }

    var body: some View {
        Group {
            if Theme.Brand.isAppIcon(asset) {
                Image(asset).resizable().interpolation(.high).scaledToFill()
                    .clipShape(AppIconShape())
                    .overlay(AppIconShape().stroke(Theme.cardStroke, lineWidth: Theme.Size.hairline))
            } else {
                // The box stays `side` (rows line up); the artwork is drawn smaller inside it when its shape asks.
                Image(asset).resizable().scaledToFit().scaleEffect(Theme.Brand.optical(asset))
            }
        }
        .modifier(BrandBox(side: side, scaled: scaled))
        .accessibilityHidden(true) // decoration: the words beside it name the service
    }
}

/// A mark on a white tile the size and corner of BeautifulUI's `BUIIconTile`, for rows whose other marks are tiles
/// (the BeautifulUI home's Needs You): the same tile as the symbols beside it, the mark at the size a symbol has on its tile.
/// White in dark mode too, as the marks are drawn for; a template mark (GitHub's) black on it.
struct BrandTile: View {
    var asset: String
    @Environment(\.buiTheme) private var theme

    init(_ asset: String) { self.asset = asset }

    var body: some View {
        let side = Theme.Size.iconL * max(1, theme.typeScale * 0.9)
        let shape = RoundedRectangle(cornerRadius: theme.radii.chip + 1, style: .continuous)
        BrandMark(asset, side: side * Theme.Brand.onTile, scaled: false)
            .foregroundStyle(.black)
            .frame(width: side, height: side)
            .background(Color.white, in: shape)
            .overlay(shape.strokeBorder(Theme.cardStroke, lineWidth: Theme.Size.hairline))
    }
}

/// A mark as a round badge on an avatar's corner (the inbox's source): on a white disc with a thin white ring, a round
/// mark filling it, any other inside the circle (`Theme.Brand.inCircle`). The caller gives the frame.
struct BrandBadge: View {
    var asset: String

    init(_ asset: String) { self.asset = asset }

    var body: some View {
        Image(asset).resizable().scaledToFit()
            .foregroundStyle(.black)
            .padding(Theme.Size.lineMedium)
            .scaleEffect(Theme.Brand.round.contains(asset) ? 1 : Theme.Brand.inCircle)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(.white, in: .circle)
            .accessibilityHidden(true)
    }
}

private struct BrandBox: ViewModifier {
    var side: CGFloat
    var scaled: Bool

    func body(content: Content) -> some View {
        if scaled { content.scaledFrame(width: side, height: side) } else { content.frame(width: side, height: side) }
    }
}

/// An app icon's squircle at any size: `Theme.Radius.appIcon` of the side it is drawn at.
nonisolated struct AppIconShape: Shape {
    func path(in rect: CGRect) -> Path {
        RoundedRectangle(cornerRadius: Theme.Radius.appIcon(min(rect.width, rect.height)), style: .continuous).path(in: rect)
    }
}
