import XCTest

/// Long-press a message: the 复制 / 选择 / 引用 / 分享 bar must stay up after the finger lifts, whatever the press
/// length; a tap on the list or a scroll still closes it. Runs against scripts/mock_hub.py:
///   TEST_RUNNER_JARVIS_MOCK_URL=http://127.0.0.1:18760 xcodebuild test … -only-testing:JarvisUITests/MessageMenuTests
final class MessageMenuTests: XCTestCase {
    private let answer = "PR #42 看完了"

    @MainActor private func launch() throws -> XCUIApplication {
        guard let mock = ProcessInfo.processInfo.environment["JARVIS_MOCK_URL"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_URL)")
        }
        let url = mock.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? mock
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", "jarvis://pair?url=\(url)&token=mock", "-tab", "dev",
                               "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006"]
        app.launchJarvis()
        return app
    }

    /// The answer's bubble. Rows of the flipped chat list report flipped frames, so press by its centre in the
    /// window rather than through the element.
    @MainActor private func answerPoint(_ app: XCUIApplication) -> XCUICoordinate {
        let text = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", answer)).firstMatch
        XCTAssertTrue(text.waitForExistence(timeout: 15), "the chat did not open")
        sleep(2) // let the list settle at the newest message
        let window = app.windows.firstMatch.frame
        var f = text.frame
        if !window.contains(CGPoint(x: f.midX, y: f.midY)) { f.origin.y = window.height - f.maxY }
        return app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: f.midX, dy: f.midY))
    }

    @MainActor func testBarStaysAfterRelease() throws {
        let app = try launch()
        let point = answerPoint(app)
        let copy = app.buttons["复制"]
        for duration in [0.4, 0.8, 1.5, 2.5] {
            point.press(forDuration: duration)
            sleep(1)
            XCTAssertTrue(copy.exists, "the bar closed after a \(duration)s press")
            // A tap on the list (away from the bar) closes it.
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35)).tap()
            XCTAssertTrue(copy.waitForNonExistence(timeout: 2), "a tap did not close the bar")
        }
    }

    @MainActor func testScrollClosesBar() throws {
        let app = try launch()
        let point = answerPoint(app)
        let copy = app.buttons["复制"]
        point.press(forDuration: 0.5)
        sleep(1)
        XCTAssertTrue(copy.exists, "the bar did not open")
        let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
        from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6)))
        XCTAssertTrue(copy.waitForNonExistence(timeout: 2), "scrolling did not close the bar")
    }
}
