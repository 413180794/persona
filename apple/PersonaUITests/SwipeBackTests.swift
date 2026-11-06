import XCTest

/// The Agents chat hides the system navigation bar for its own header; the edge swipe back must still work, folded or
/// not. Needs a paired hub: scripts/app/ui-test.sh passes PERSONA_PAIR_URL and PERSONA_OPEN_SESSION.
final class SwipeBackTests: XCTestCase {
    @MainActor private func launch(_ extra: [String]) throws -> XCUIApplication {
        let env = ProcessInfo.processInfo.environment
        guard let pair = env["PERSONA_PAIR_URL"], let session = env["PERSONA_OPEN_SESSION"] else {
            throw XCTSkip("run through scripts/app/ui-test.sh (needs a hub)")
        }
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", session] + extra
        app.launchPinned()
        return app
    }

    @MainActor private func swipeBackFromEdge(_ app: XCUIApplication) {
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.5))
        start.press(forDuration: 0.05, thenDragTo: end)
    }

    @MainActor func testSwipeBackWithTheHeaderOpen() throws {
        try check([])
    }

    @MainActor func testSwipeBackWithTheHeaderFolded() throws {
        try check(["-showCompactChrome", "YES"])
    }

    @MainActor private func check(_ extra: [String]) throws {
        let app = try launch(extra)
        let back = app.buttons["返回"]
        XCTAssertTrue(back.waitForExistence(timeout: 15), "the chat did not open")
        sleep(5) // let the folded variant fold
        swipeBackFromEdge(app)
        // Back on the list: its search field is there and the chat's back button is gone.
        XCTAssertTrue(app.searchFields.firstMatch.waitForExistence(timeout: 5), "still in the chat after the swipe")
        XCTAssertFalse(back.isHittable)
    }

    /// Left alone, the open header folds into the slim bar within a few seconds (ChatView.chromeIdleSeconds).
    @MainActor func testHeaderFoldsWhenIdle() throws {
        let app = try launch([])
        XCTAssertTrue(app.buttons["返回"].waitForExistence(timeout: 15), "the chat did not open")
        let slim = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "显示标题和标签")).firstMatch
        XCTAssertTrue(slim.waitForExistence(timeout: 8), "the header did not fold")
        let deadline = Date.now.addingTimeInterval(4)
        while !slim.isHittable && Date.now < deadline { usleep(200_000) }
        XCTAssertTrue(slim.isHittable, "the slim bar is not showing")
    }

    /// The header watches touches (to stay open while used): its buttons must still work.
    @MainActor func testHeaderButtonsStillWork() throws {
        let app = try launch([])
        let more = app.buttons["更多"]
        XCTAssertTrue(more.waitForExistence(timeout: 15), "the chat did not open")
        more.tap()
        let terminal = app.buttons["终端"]
        XCTAssertTrue(terminal.waitForExistence(timeout: 5), "no 终端 in the ··· menu")
        terminal.tap()
        // The terminal's own bar: it came up over the page (the text field shows only in some input modes).
        XCTAssertTrue(app.descendants(matching: .any)["terminal-close"].waitForExistence(timeout: 5), "tapping 终端 did nothing")
    }
}
