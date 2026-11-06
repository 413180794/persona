import CoreGraphics
import Testing
@testable import Persona

/// The terminal cover, the file viewer and the Git sheets open as large as their window on the Mac (they opened at
/// a form sheet's 640 pt, the file viewer at 470 × 280), never past it and never below a form sheet.
@MainActor struct MacLargeSheetTests {
    @Test func fillsTheWindowLessItsMargins() {
        #expect(MacLargeSheet.fitted(to: CGSize(width: 1280, height: 820), bars: false) == CGSize(width: 1216, height: 756))
        // A navigation stack's title bar and bottom toolbar come on top of the content's height.
        #expect(MacLargeSheet.fitted(to: CGSize(width: 1280, height: 820), bars: true) == CGSize(width: 1216, height: 640))
    }

    @Test func neverSmallerThanAFormSheet() {
        #expect(MacLargeSheet.fitted(to: CGSize(width: 500, height: 400), bars: true) == CGSize(width: 640, height: 520))
    }
}
