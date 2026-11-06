import CoreGraphics
import Foundation

/// Where the image viewer's pages lie (a horizontal row of pages, each as wide as the screen), unit-tested. The pager
/// never stays between two pages: whenever it comes to rest off a page boundary it moves to `nearestPage`.
public nonisolated enum PagerMath {
    /// The page whose left edge is nearest `offset`, within 0..<count.
    public static func nearestPage(offset: CGFloat, width: CGFloat, count: Int) -> Int {
        guard width > 0, count > 0 else { return 0 }
        return clamp(Int((offset / width).rounded()), count: count)
    }

    /// The scroll offset at which `page` fills the screen.
    public static func offset(page: Int, width: CGFloat, count: Int) -> CGFloat {
        CGFloat(clamp(page, count: count)) * max(0, width)
    }

    /// Whether the pages lie exactly on the screen at `offset` (within half a point).
    public static func isAligned(offset: CGFloat, width: CGFloat, count: Int) -> Bool {
        guard width > 0, count > 0 else { return true }
        return abs(offset - self.offset(page: nearestPage(offset: offset, width: width, count: count), width: width, count: count)) < 0.5
    }

    /// The pages to keep laid out at `offset`: the ones on screen and one more on each side.
    public static func livePages(offset: CGFloat, width: CGFloat, count: Int) -> ClosedRange<Int> {
        guard width > 0, count > 0 else { return 0...0 }
        let first = Int((offset / width).rounded(.down)) - 1
        let last = Int(((offset + width) / width).rounded(.up))
        return clamp(first, count: count)...clamp(max(first, last), count: count)
    }

    private static func clamp(_ page: Int, count: Int) -> Int { min(max(0, page), max(0, count - 1)) }
}
