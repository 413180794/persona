import SwiftUI
import Testing
@testable import BeautifulUI

@Suite struct FlowLayoutTests {
    @Test func wrapsWhenTheRowIsFull() {
        let sizes = [CGSize(width: 100, height: 28), CGSize(width: 150, height: 28), CGSize(width: 90, height: 28)]
        let frames = BUIFlowLayout.frames(for: sizes, width: 260, spacing: 6, lineSpacing: 6)
        #expect(frames[0].origin == .zero)
        #expect(frames[1].origin == CGPoint(x: 106, y: 0))
        #expect(frames[2].origin == CGPoint(x: 0, y: 34))
    }

    @Test func centresShorterItemsInTheirRow() {
        let frames = BUIFlowLayout.frames(for: [CGSize(width: 50, height: 28), CGSize(width: 50, height: 20)], width: 300, spacing: 6, lineSpacing: 6)
        #expect(frames[1].minY == 4)
    }

    @Test func clampsAnItemWiderThanTheRow() {
        let frames = BUIFlowLayout.frames(for: [CGSize(width: 500, height: 28)], width: 300, spacing: 6, lineSpacing: 6)
        #expect(frames[0].width == 300)
    }
}
