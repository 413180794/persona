import SwiftUI
import Testing
@testable import BeautifulUI

@Suite struct LoadingStateTests {
    @Test func formatsUnderAMinute() {
        #expect(LoadingState.formatElapsed(0) == "0.0s")
        #expect(LoadingState.formatElapsed(3.66) == "3.6s")
        #expect(LoadingState.formatElapsed(59.99) == "59.9s")
    }

    @Test func formatsMinutes() {
        #expect(LoadingState.formatElapsed(60) == "1m 0.0s")
        #expect(LoadingState.formatElapsed(125.04) == "2m 5.0s")
    }

    @Test func driveIsAChevronWavefront() {
        // Middle row leads, outer rows trail by one step; columns step 90 ms to the right.
        let d = PixelLoader.Pattern.drive.delays.map { $0! }
        #expect(d == [0.09, 0.18, 0.27, 0, 0.09, 0.18, 0.09, 0.18, 0.27])
    }

    @Test func orbitSkipsTheCentre() {
        let d = PixelLoader.Pattern.orbit.delays
        #expect(d[4] == nil)
        #expect(d.compactMap { $0 }.count == 8)
        #expect(PixelLoader.opacity(.orbit, cell: 4, elapsed: 1) == 0.07)
    }

    @Test func reducedMotionShowsDimGrid() {
        // Elapsed -1 is what the view passes when Reduce Motion is on.
        for cell in 0..<9 {
            #expect(PixelLoader.opacity(.drive, cell: cell, elapsed: -1) == 0.15)
        }
    }
}
