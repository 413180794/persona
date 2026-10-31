import XCTest

extension XCUIElement {
    /// Taps the middle of the element, where a finger (and VoiceOver) does. For the round glass buttons of the Jarvis
    /// tab's top bar (历史对话, 新对话): on iOS 27.0 `tap()` lands about 4 pt inside the frame's top-left corner, outside
    /// the circle the button takes touches in, and nothing happens (an iPhone 17 Pro simulator, 2026-10-31).
    func tapCenter() {
        coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
    }
}
