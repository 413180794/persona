import CoreGraphics
import Testing
@testable import Persona

/// Late remote images give the mail web view its real height only after `didFinish`, so remeasurements keep arriving.
/// Accepting them (and ignoring rounding jitter) is pure logic in `FittedHeight`, tested here without a WKWebView:
/// the document keeps its 20pt slack, and anything within 1pt of the current height is ignored.
struct MacMailFittedHeightTests {
    @Test func firstMeasurementFromZeroIsApplied() {
        #expect(FittedHeight.shouldUpdate(from: 0, to: 100))
        #expect(FittedHeight.fittedHeight(measured: 100, current: 0) == 120)
    }

    @Test func growthWhenARemoteImageLoadsIsApplied() {
        // Text only: document 100pt -> frame 120pt. The image loads and the document grows to 400pt.
        #expect(FittedHeight.shouldUpdate(from: 120, to: 400))
        #expect(FittedHeight.fittedHeight(measured: 400, current: 120) == 420)
    }

    @Test func changeBelowThresholdIsIgnoredEvenWithPadding() {
        // 100.5 + 20 = 120.5, only half a point past the current 120: sub-pixel layout noise.
        #expect(FittedHeight.fittedHeight(measured: 100.5, current: 120) == nil)
        #expect(!FittedHeight.shouldUpdate(from: 120, to: 100.5))
    }

    @Test func shrinkBelowThresholdIsIgnored() {
        // 101 + 20 = 121, exactly 1pt away; the guard is strictly greater than.
        #expect(FittedHeight.fittedHeight(measured: 101, current: 120) == nil)
        #expect(!FittedHeight.shouldUpdate(from: 120, to: 101))
    }

    @Test func shrinkPastThresholdIsApplied() {
        // A genuinely shorter page (new mail) must be allowed to shrink.
        #expect(FittedHeight.fittedHeight(measured: 60, current: 120) == 80)
    }

    @Test func equalHeightIsIgnored() {
        #expect(FittedHeight.fittedHeight(measured: 100, current: 120) == nil)
        #expect(!FittedHeight.shouldUpdate(from: 120, to: 100))
    }
}
