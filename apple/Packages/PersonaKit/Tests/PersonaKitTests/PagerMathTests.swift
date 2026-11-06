import CoreGraphics
import Testing
@testable import PersonaKit

struct PagerMathTests {
    @Test func nearestPageRoundsAndClamps() {
        #expect(PagerMath.nearestPage(offset: 0, width: 400, count: 5) == 0)
        #expect(PagerMath.nearestPage(offset: 199, width: 400, count: 5) == 0)
        #expect(PagerMath.nearestPage(offset: 201, width: 400, count: 5) == 1)
        // Stuck 80% of the way to page 3, as the bug left it: back onto page 3.
        #expect(PagerMath.nearestPage(offset: 400 * 2.8, width: 400, count: 5) == 3)
        // Bounced past either end.
        #expect(PagerMath.nearestPage(offset: -150, width: 400, count: 5) == 0)
        #expect(PagerMath.nearestPage(offset: 400 * 4 + 300, width: 400, count: 5) == 4)
        #expect(PagerMath.nearestPage(offset: 300, width: 0, count: 5) == 0)
        #expect(PagerMath.nearestPage(offset: 300, width: 400, count: 0) == 0)
    }

    @Test func alignedOnlyOnAPageBoundary() {
        #expect(PagerMath.isAligned(offset: 800, width: 400, count: 5))
        #expect(PagerMath.isAligned(offset: 800.3, width: 400, count: 5))
        #expect(!PagerMath.isAligned(offset: 803.7, width: 400, count: 5))
        #expect(!PagerMath.isAligned(offset: 720, width: 400, count: 5))
        // Past the last page is not aligned: it has to come back.
        #expect(!PagerMath.isAligned(offset: 1700, width: 400, count: 5))
        #expect(PagerMath.offset(page: 3, width: 400, count: 5) == 1200)
        #expect(PagerMath.offset(page: 9, width: 400, count: 5) == 1600)
    }

    @Test func livePagesCoverTheScreenAndOneEachSide() {
        #expect(PagerMath.livePages(offset: 0, width: 400, count: 5) == 0...1)
        #expect(PagerMath.livePages(offset: 800, width: 400, count: 5) == 1...3)
        #expect(PagerMath.livePages(offset: 900, width: 400, count: 5) == 1...4)
        #expect(PagerMath.livePages(offset: 1600, width: 400, count: 5) == 3...4)
        #expect(PagerMath.livePages(offset: 0, width: 400, count: 1) == 0...0)
    }
}
