import XCTest

/// The Agents chat hides the system navigation bar for its own header; the edge swipe back must still work, folded or
/// not. Needs a paired hub: scripts/ui-test.sh passes JARVIS_PAIR_URL and JARVIS_OPEN_SESSION.
final class SwipeBackTests: XCTestCase {
    @MainActor private func launch(_ extra: [String]) throws -> XCUIApplication {
        let env = ProcessInfo.processInfo.environment
        guard let pair = env["JARVIS_PAIR_URL"], let session = env["JARVIS_OPEN_SESSION"] else {
            throw XCTSkip("run through scripts/ui-test.sh (needs a hub)")
        }
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", session] + extra
        app.launch()
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
}
