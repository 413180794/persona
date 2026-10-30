import Testing
@testable import BeautifulUI

@Suite struct CurveTests {
    @Test func endpointsAreFixed() {
        for curve in [BUICurve.ease, .easeInOut, .outStrong, .inOutStrong, .link] {
            #expect(curve(0) == 0)
            #expect(curve(1) == 1)
        }
    }

    @Test func linearIsIdentity() {
        for t in stride(from: 0.0, through: 1.0, by: 0.1) {
            #expect(abs(BUICurve.linear(t) - t) < 1e-4)
        }
    }

    @Test func easeInOutIsSymmetric() {
        #expect(abs(BUICurve.easeInOut(0.5) - 0.5) < 1e-4)
        #expect(abs(BUICurve.easeInOut(0.25) + BUICurve.easeInOut(0.75) - 1) < 1e-4)
    }

    @Test func matchesReferenceSamples() {
        // Reference values from a bisection solver of the same CSS curves.
        #expect(abs(BUICurve.outStrong(0.25) - 0.77538) < 1e-4)
        #expect(abs(BUICurve.easeInOut(0.25) - 0.12916) < 1e-4)
    }
}

@Suite struct KeyframeTests {
    let pixelOn = PixelLoader.pixelOn

    @Test func holdsAtStops() {
        #expect(pixelOn.value(at: 0) == 0.15)
        #expect(pixelOn.value(at: 0.18) == 1)
        #expect(pixelOn.value(at: 0.3) == 1)
        #expect(pixelOn.value(at: 0.8) == 0.15)
    }

    @Test func restsUntilDelayEnds() {
        #expect(pixelOn.value(elapsed: 0.05, duration: 0.65, delay: 0.09, rest: 0.15) == 0.15)
    }

    @Test func repeatsEveryCycle() {
        let a = pixelOn.value(elapsed: 0.2, duration: 0.65, delay: 0, rest: 0.15)
        let b = pixelOn.value(elapsed: 0.2 + 0.65 * 3, duration: 0.65, delay: 0, rest: 0.15)
        #expect(abs(a - b) < 1e-6)
    }
}
