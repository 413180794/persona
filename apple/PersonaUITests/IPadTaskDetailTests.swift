import XCTest

/// iPad (regular width, #2816): a task opened from the main conversation's dock, and an agent of its own, fill the
/// detail column beside the sidebar (pushed onto the conversation's stack) instead of rising as the small form sheet in
/// the middle of the screen. Their header's ‹ goes back to the conversation. Against scripts/mock/mock_hub.py with
/// MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB); skipped on a phone (the sheet, `TaskDockAgentTests`). With
/// PERSONA_SHOTS set it writes the pages there, named by PERSONA_SHOT_TAG (orientation and appearance; `portrait` in it
/// turns the iPad upright), or with PERSONA_HOLD=<seconds> holds each page for `simctl io screenshot`.
final class IPadTaskDetailTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let tag = env["PERSONA_SHOT_TAG"] ?? "ipad"
        let file = "\(dir)/task-detail-\(tag)-\(name).png"
        // XCUITest's own picture of a landscape screen comes out turned (docs/apple/ios.md「iPad」): with PERSONA_HOLD the page
        // stays up that long and a marker file names the picture for `simctl io screenshot` to take from outside.
        if let hold = env["PERSONA_HOLD"].flatMap(UInt32.init) {
            try Data(file.utf8).write(to: URL(fileURLWithPath: "\(dir)/hold-\(name)"))
            sleep(hold)
        } else {
            try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: file))
        }
    }

    @MainActor func testTaskAndAgentFillTheDetailColumn() throws {
        guard UIDevice.current.userInterfaceIdiom == .pad else { throw XCTSkip("an iPad's layout (the phone keeps the sheet)") }
        if ProcessInfo.processInfo.environment["PERSONA_SHOT_TAG"]?.contains("portrait") == true {
            XCUIDevice.shared.orientation = .portrait
        } else {
            XCUIDevice.shared.orientation = .landscapeLeft
        }
        let app = XCUIApplication()
        let url = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", url]
        app.launchPinned()

        let tasks = app.buttons.matching(identifier: "task-dock.pill")
        let agents = app.buttons.matching(identifier: "task-dock.agent")
        XCTAssertTrue(tasks.firstMatch.waitForExistence(timeout: 20), "no task in the dock:\n\(app.debugDescription)")
        let deadline = Date.now.addingTimeInterval(20)
        while agents.count == 0 && Date.now < deadline { usleep(200_000) }
        sleep(2) // the conversation settles
        try shot(app, "conversation")

        // A task: its page is the column's, with ‹ (no sheet's ✕), and the conversation under it is gone from screen.
        tasks.firstMatch.tap()
        let back = app.buttons["返回"].firstMatch
        XCTAssertTrue(back.waitForExistence(timeout: 5), "no ‹ on the task's page:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["关闭"].exists, "the task rose as a sheet")
        XCTAssertFalse(tasks.firstMatch.isHittable, "the conversation's dock still shows beside the task")
        sleep(2)
        try shot(app, "task")

        back.tap()
        XCTAssertTrue(tasks.firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(tasks.firstMatch.isHittable, "back did not come back to the conversation")
        sleep(1)

        // An agent of its own: the same page, under its own title (its chat's header, #3255).
        let agent = agents.firstMatch
        XCTAssertTrue(agent.waitForExistence(timeout: 5), app.debugDescription)
        for _ in 0..<3 where !agent.isHittable { // the dock is wider than the column: scroll it from a task
            tasks.element(boundBy: 0).swipeLeft()
            sleep(1)
        }
        agent.tap()
        XCTAssertTrue(app.buttons.matching(identifier: "chat.title").firstMatch.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(app.buttons["返回"].firstMatch.exists, "no ‹ on the agent's page")
        XCTAssertFalse(app.buttons["关闭"].exists, "the agent rose as a sheet")
        sleep(2)
        try shot(app, "agent")
        app.buttons["返回"].firstMatch.tap()
        XCTAssertTrue(tasks.firstMatch.waitForExistence(timeout: 5))
    }
}
