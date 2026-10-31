import SwiftUI
import Testing
@testable import BeautifulUI

@Suite struct SVGPathTests {
    @Test func absoluteAndRelativeLinesAgree() {
        let a = SVGPath.parse("M6 9L12 15L18 9").boundingRect
        let b = SVGPath.parse("M6 9l6 6 6-6").boundingRect
        #expect(a == b)
        #expect(a == CGRect(x: 6, y: 9, width: 12, height: 6))
    }

    @Test func circleArcsCoverTheDisc() {
        let r = SVGPath.parse(BUIGlyph.circle(12, 12, 9)).boundingRect
        #expect(abs(r.minX - 3) < 0.01 && abs(r.maxX - 21) < 0.01)
        #expect(abs(r.minY - 3) < 0.01 && abs(r.maxY - 21) < 0.01)
    }

    @Test func roundedRectKeepsItsBounds() {
        let r = SVGPath.parse(BUIGlyph.rect(9, 9, 12, 12, rx: 2.5)).boundingRect
        #expect(abs(r.minX - 9) < 0.01 && abs(r.width - 12) < 0.01 && abs(r.height - 12) < 0.01)
    }

    @Test func packedNumbersAndFlagsParse() {
        // "-2.33 8" written as "l-2.33 8", decimals packed as ".5.5", arc flags without spaces.
        let r = SVGPath.parse("M0 0l-2.33 8M1 1l.5.5M0 10a2 2 0 012 2").boundingRect
        #expect(r.minX < -2.3 && r.maxY >= 11.9)
    }
}
