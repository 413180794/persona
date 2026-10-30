import SwiftUI
import Testing
@testable import BeautifulUI

@Suite struct FlowchartTests {
    @Test func rowsStackWithPaddingAndGap() {
        #expect(Flowchart.tops(heights: [92, 134]) == [CGFloat(24), CGFloat(24 + 92 + 64)])
    }

    @Test func connectorHandleLengthIsClamped() {
        // 0.55 × 40 = 22 clamps up to 24; 0.55 × 400 = 220 clamps down to 84.
        let short = Flowchart.connector(from: .zero, to: CGPoint(x: 0, y: 40)).boundingRect
        #expect(short.maxY <= 40.01)
        let long = Flowchart.connector(from: .zero, to: CGPoint(x: 100, y: 400)).boundingRect
        #expect(abs(long.width - 100) < 0.01)
    }
}
