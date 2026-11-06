import XCTest

/// The task dock above the assistant's composer (#1094): All stays at the leading edge while the pills scroll sideways
/// beside it, so a dock wider than the screen never pushes it out of reach. Against scripts/mock/mock_hub.py with
/// MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 MOCK_TASKS_DOCK=many (JARVIS_MOCK_HUB).
final class TaskDockTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_DOCK=many (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor func testBoardStaysFirstWhilePillsScroll() throws {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair]
        app.launchPinned()

        let board = app.buttons["task-dock.board"]
        XCTAssertTrue(board.waitForExistence(timeout: 20), "no 全部 in the dock:\n\(app.debugDescription)")
        let pills = app.buttons.matching(identifier: "task-dock.pill")
        let deadline = Date.now.addingTimeInterval(10)
        while pills.count < 6 && Date.now < deadline { usleep(200_000) }
        XCTAssertGreaterThanOrEqual(pills.count, 6, "want six or more pills:\n\(app.debugDescription)")

        sleep(2) // the conversation above settles (cards arrive, the question card rises)
        let first = pills.element(boundBy: 0)
        let before = (board: board.frame, pill: first.frame)
        XCTAssertLessThan(before.board.maxX, before.pill.minX, "全部 is not before the first pill")
        let window = app.windows.firstMatch.frame
        XCTAssertGreaterThan(pills.element(boundBy: pills.count - 1).frame.maxX, window.maxX, "the dock fits the screen: nothing to scroll")

        // Swipe the pills: they move sideways, All does not (the row as a whole may still move up or down with the
        // conversation above it).
        first.swipeLeft()
        sleep(1)
        XCTAssertEqual(board.frame.minX, before.board.minX, accuracy: 0.5, "全部 moved with the pills")
        XCTAssertEqual(board.frame.width, before.board.width, accuracy: 0.5, "全部 changed its width")
        XCTAssertTrue(board.isHittable, "全部 is out of reach after a scroll")
        let moved = pills.element(boundBy: 0).frame.minX
        XCTAssertLessThan(moved, before.pill.minX - 20, "the pills did not scroll")
    }
}
