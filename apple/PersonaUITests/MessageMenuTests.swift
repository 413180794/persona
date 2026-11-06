import XCTest

/// Long-press a message: the 复制 / 选择文本 / 引用 / 分享 bar must stay up after the finger lifts, whatever the press
/// length; a tap on the list or a scroll still closes it. Runs against scripts/mock/mock_hub.py:
///   TEST_RUNNER_JARVIS_MOCK_URL=http://127.0.0.1:18760 xcodebuild test … -only-testing:PersonaUITests/MessageMenuTests
final class MessageMenuTests: XCTestCase {
    private let answer = "PR #42 看完了"
    private let question = "看一下 PR #42"

    @MainActor private func launch() throws -> XCUIApplication {
        guard let mock = ProcessInfo.processInfo.environment["JARVIS_MOCK_URL"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (JARVIS_MOCK_URL)")
        }
        let url = mock.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? mock
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", "jarvis://pair?url=\(url)&token=mock", "-tab", "dev",
                               "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006"]
        app.launchPinned()
        return app
    }

    /// The answer's bubble. Rows of the flipped chat list report flipped frames, so press by its centre in the
    /// window rather than through the element.
    @MainActor private func answerPoint(_ app: XCUIApplication) -> XCUICoordinate { point(of: answer, in: app) }

    @MainActor private func point(of message: String, in app: XCUIApplication) -> XCUICoordinate {
        let text = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", message)).firstMatch
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

    /// The bar opens over the press, also on a short message of your own at the right edge (#501): the press is
    /// reported in the row's coordinates, where the bar is placed, not in the bubble's.
    @MainActor func testBarOpensAtThePress() throws {
        let app = try launch()
        let point = point(of: question, in: app)
        point.press(forDuration: 0.5)
        let copy = app.buttons["复制"]
        XCTAssertTrue(copy.waitForExistence(timeout: 2), "the bar did not open")
        // Sideways only: the flipped list reports flipped heights, not flipped widths.
        let bar = ["复制", "选择文本", "引用", "分享", "编辑"].map { app.buttons[$0] }.filter(\.exists)
            .map(\.frame).reduce(copy.frame) { $0.union($1) }
        let x = point.screenPoint.x
        XCTAssertTrue(bar.minX <= x && x <= bar.maxX, "the bar (\(bar.minX)–\(bar.maxX)) is not over the press at x \(x)")
    }

    /// 分支到新聊天 (#2137): ⋯ at the end of a reply's bar opens a menu that stays (the list's tap-to-close must not take
    /// it away), and its item opens the new conversation. `TEST_RUNNER_JARVIS_SHOTS=<dir>` also saves the two screens.
    @MainActor func testForkFromAReply() throws {
        let app = try launch()
        answerPoint(app).press(forDuration: 0.5)
        let more = app.buttons["更多"]
        XCTAssertTrue(more.waitForExistence(timeout: 3), "the reply's bar has no ⋯")
        shot("fork-ios-bar")
        more.tap()
        let fork = app.buttons["分支到新聊天"]
        XCTAssertTrue(fork.waitForExistence(timeout: 3), "⋯ did not open its menu")
        sleep(1)
        XCTAssertTrue(fork.exists, "the menu closed by itself")
        shot("fork-ios-menu")
        fork.tap()
        let title = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "分支 · ")).firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 10), "the new conversation did not open")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", answer)).firstMatch.waitForExistence(timeout: 10),
                      "the new conversation does not have the reply it was branched from")
        shot("fork-ios-new")
    }

    @MainActor private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] else { return }
        let mode = XCUIDevice.shared.appearance == .dark ? "dark" : "light"
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "\(name)-\(mode).png"))
    }
}
