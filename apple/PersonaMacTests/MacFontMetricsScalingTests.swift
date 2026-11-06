import Foundation
import Testing
@testable import Persona

/// The no-traits `UIFontMetrics.scaledValue(for:)` on the Mac follows the app's Small / Standard / Large setting (#3612): the
/// code/file viewer makes its font with it (`CodeMetrics.font`), and that variant used to return the value unchanged,
/// so the viewer kept the same size at every setting. Small / Standard / Large are a point a step around the body (12 / 13 / 14).
@MainActor @Suite(.serialized)
struct MacFontMetricsScalingTests {
    private func near(_ a: CGFloat, _ b: CGFloat) -> Bool { abs(a - b) < 0.01 }

    private func scaledValue(_ setting: String) -> CGFloat {
        let previous = UserDefaults.standard.string(forKey: "ui.textSize")
        defer { UserDefaults.standard.set(previous, forKey: "ui.textSize") }
        UserDefaults.standard.set(setting, forKey: "ui.textSize")
        return UIFontMetrics(forTextStyle: .footnote).scaledValue(for: 12.5)
    }

    @Test func standardLeavesTheViewerSize() {
        #expect(scaledValue("standard") == 12.5)
    }

    @Test func smallStepsTheViewerSizeDown() {
        let value = scaledValue("small")
        #expect(value < 12.5)
        #expect(near(value, 12.5 * 12 / 13))
    }

    @Test func largeStepsTheViewerSizeUp() {
        let value = scaledValue("large")
        #expect(value > 12.5)
        #expect(near(value, 12.5 * 14 / 13))
    }
}
