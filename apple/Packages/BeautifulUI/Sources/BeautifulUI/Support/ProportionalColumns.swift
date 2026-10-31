import SwiftUI

/// A table row whose cells take fixed fractions of the width (CSS `table-fixed` with
/// `<col>` percentages). Cells are vertically centred; the row is as tall as its tallest cell.
public struct BUIColumns: Layout {
    public var fractions: [CGFloat]

    public init(_ fractions: [CGFloat]) {
        self.fractions = fractions
    }

    private func widths(_ total: CGFloat, count: Int) -> [CGFloat] {
        (0..<count).map { i in total * (i < fractions.count ? fractions[i] : 0) }
    }

    public func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? subviews.map { $0.sizeThatFits(.unspecified).width }.reduce(0, +)
        let ws = widths(width, count: subviews.count)
        let height = zip(subviews, ws).map { $0.sizeThatFits(ProposedViewSize(width: $1, height: nil)).height }.max() ?? 0
        return CGSize(width: width, height: height)
    }

    public func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX
        for (subview, w) in zip(subviews, widths(bounds.width, count: subviews.count)) {
            subview.place(at: CGPoint(x: x, y: bounds.midY), anchor: .leading, proposal: ProposedViewSize(width: w, height: bounds.height))
            x += w
        }
    }
}

/// The site's tag pill: `h-5.5 rounded-full bg-inset px-2 text-[11.5px] font-medium shadow-hairline`
/// with a 6 pt colour dot.
public struct BUITagPill: View {
    var label: String
    var dot: Color?
    var fill: Color?
    @Environment(\.buiTheme) private var theme

    public init(_ label: String, dot: Color? = nil, fill: Color? = nil) {
        self.label = label
        self.dot = dot
        self.fill = fill
    }

    public var body: some View {
        HStack(spacing: 6) {
            if let dot {
                Circle().fill(dot).frame(width: 6, height: 6)
            }
            Text(label)
                .buiFont(BUITextStyle(size: 11.5, weight: .medium))
                .foregroundStyle(theme.colors.ink2)
                .lineLimit(1)
        }
        .padding(.horizontal, 8)
        .frame(height: 22)
        .background(fill ?? theme.colors.inset, in: Capsule())
        .overlay(Capsule().strokeBorder(theme.colors.line, lineWidth: 1))
        .fixedSize()
    }
}

/// `.records-tag` / `.filter-status-*`: one base hue mixed into the surface for the fill and
/// ring and into ink for the label — light mode 18–20 % / 32–34 %, dark mode 34 %.
public struct BUIHueTag: View {
    var label: String
    var base: Color
    var lightFill: Double
    @Environment(\.buiTheme) private var theme
    @Environment(\.colorScheme) private var scheme

    public init(_ label: String, base: Color, lightFill: Double = 0.2) {
        self.label = label
        self.base = base
        self.lightFill = lightFill
    }

    public var body: some View {
        let dark = scheme == .dark
        let fill = base.mix(with: theme.colors.surface, by: 1 - (dark ? 0.34 : lightFill))
        let ring = base.mix(with: theme.colors.surface, by: dark ? 0.66 : 0.66)
        let ink = dark ? base.mix(with: .white, by: 0.14) : base.mix(with: theme.colors.ink, by: 0.08)
        Text(label)
            .buiFont(BUITextStyle(size: 13, weight: .medium, lineHeight: 1.19))
            .foregroundStyle(ink)
            .lineLimit(1)
            .padding(.horizontal, 7)
            .frame(height: 23)
            .background(fill, in: .rect(cornerRadius: 8, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(ring, lineWidth: 1))
            .fixedSize()
    }
}
