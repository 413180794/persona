import SwiftUI

/// Lays children out in rows and wraps when a row is full, like CSS `flex-wrap`.
public struct BUIFlowLayout: Layout {
    public var spacing: CGFloat
    public var lineSpacing: CGFloat

    public init(spacing: CGFloat = 6, lineSpacing: CGFloat? = nil) {
        self.spacing = spacing
        self.lineSpacing = lineSpacing ?? spacing
    }

    public func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let frames = Self.frames(for: subviews.map { $0.sizeThatFits(.unspecified) }, width: proposal.width ?? .infinity, spacing: spacing, lineSpacing: lineSpacing)
        let width = frames.map(\.maxX).max() ?? 0
        let height = frames.map(\.maxY).max() ?? 0
        return CGSize(width: proposal.width.map { min($0, width) } ?? width, height: height)
    }

    public func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let sizes = subviews.map { $0.sizeThatFits(.unspecified) }
        let frames = Self.frames(for: sizes, width: bounds.width, spacing: spacing, lineSpacing: lineSpacing)
        for (subview, frame) in zip(subviews, frames) {
            subview.place(
                at: CGPoint(x: bounds.minX + frame.minX, y: bounds.minY + frame.minY),
                proposal: ProposedViewSize(width: min(frame.width, bounds.width), height: frame.height)
            )
        }
    }

    /// Row-major frames; items in a row are vertically centred on the row.
    static func frames(for sizes: [CGSize], width: CGFloat, spacing: CGFloat, lineSpacing: CGFloat) -> [CGRect] {
        var frames: [CGRect] = []
        var rowStart = 0
        var x: CGFloat = 0, y: CGFloat = 0, rowHeight: CGFloat = 0
        func closeRow(_ end: Int) {
            for i in rowStart..<end {
                frames[i].origin.y = y + (rowHeight - frames[i].height) / 2
            }
        }
        for (i, size) in sizes.enumerated() {
            if x > 0 && x + size.width > width {
                closeRow(i)
                y += rowHeight + lineSpacing
                x = 0
                rowHeight = 0
                rowStart = i
            }
            frames.append(CGRect(origin: CGPoint(x: x, y: y), size: CGSize(width: min(size.width, width), height: size.height)))
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
        closeRow(sizes.count)
        return frames
    }
}
