import CoreGraphics
import Testing
@testable import PersonaKit

struct ZoomMathTests {
    @Test func fitsWideAndTallImages() {
        let bounds = CGSize(width: 400, height: 800)
        #expect(ZoomMath.fittedSize(image: CGSize(width: 2000, height: 1000), in: bounds) == CGSize(width: 400, height: 200))
        #expect(ZoomMath.fittedSize(image: CGSize(width: 500, height: 2000), in: bounds) == CGSize(width: 200, height: 800))
        #expect(ZoomMath.fittedSize(image: .zero, in: bounds) == .zero)
    }

    @Test func maxScaleIsClamped() {
        // A small screenshot still zooms to 3x; a huge photo stops at 12x.
        #expect(ZoomMath.maxScale(image: CGSize(width: 300, height: 300), fitted: CGSize(width: 400, height: 400), screenScale: 3) == 3)
        #expect(ZoomMath.maxScale(image: CGSize(width: 40000, height: 30000), fitted: CGSize(width: 400, height: 300), screenScale: 3) == 12)
    }

    @Test func centersSmallContent() {
        let inset = ZoomMath.centeringInset(content: CGSize(width: 400, height: 200), bounds: CGSize(width: 400, height: 800))
        #expect(inset == CGSize(width: 0, height: 300))
        #expect(ZoomMath.centeringInset(content: CGSize(width: 1000, height: 2000), bounds: CGSize(width: 400, height: 800)) == .zero)
    }
}
