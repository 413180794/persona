import XCTest

/// Background tasks reporting back inside a chat (Item.task): a background heredoc finishing, a test run failing, a
/// Monitor's events between two commands and its end. Each is one line in the tools' look — the heredoc's notice used
/// to be the whole heredoc in a centered capsule, screens tall, its `>` still `&gt;`. The call that started a task says
/// how it went, and a notice leads back to that call. Against scripts/mock/mock_hub.py with MOCK_TASKS=1 (PERSONA_MOCK_HUB).
final class BackgroundNoticeTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-bg-\(name).png"))
    }

    @MainActor func testBackgroundNoticesAreOneLineAndLeadToTheirCall() throws {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000009"]
        app.launchPinned()

        let work = app.buttons["work-head"]
        XCTAssertTrue(work.waitForExistence(timeout: 20), "the chat did not open:\n\(app.debugDescription)")
        work.tap()
        let notes = app.buttons.matching(identifier: "task-note")
        let deadline = Date.now.addingTimeInterval(10)
        while notes.count < 5 && Date.now < deadline { usleep(200_000) }
        XCTAssertEqual(notes.count, 5, "want five task notices in the open work line:\n\(app.debugDescription)")
        shot(app, "open")

        // One line each, never the heredoc: as tall as a tool row.
        for i in 0..<notes.count {
            let n = notes.element(boundBy: i)
            XCTAssertLessThan(n.frame.height, 60, "notice \(i) is \(n.frame.height) pt tall: \(n.label)")
            XCTAssertFalse(n.label.contains("&gt;") || n.label.contains("&lt;"), "entities in \(n.label)")
        }
        let heredoc = notes.matching(NSPredicate(format: "label CONTAINS %@", "cat > brief.md")).firstMatch
        XCTAssertTrue(heredoc.exists, "the heredoc's notice is not named by its command's first line")
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "## 新功能")).firstMatch.exists,
                       "the heredoc's text is spread over the chat")
        XCTAssertTrue(notes.matching(NSPredicate(format: "label CONTAINS %@", "build: 3/5 jobs passed")).firstMatch.exists,
                      "a Monitor event does not show what happened")
        XCTAssertTrue(notes.matching(NSPredicate(format: "label CONTAINS %@", "退出码 1")).firstMatch.exists, "the failure has no exit code")

        // The calls that went to the background say how they went.
        XCTAssertTrue(app.staticTexts["后台 · 已完成"].exists, "the heredoc's call has no 后台 · 已完成:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["后台 · 失败"].exists, "the test run's call has no 后台 · 失败")
        XCTAssertTrue(app.staticTexts["后台 · 已结束 · 2 个事件"].exists, "the Monitor's call does not count its events")

        // Opening the heredoc's notice: the whole command; then back to the call that started it.
        heredoc.tap()
        let whole = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "## 新功能")).firstMatch
        XCTAssertTrue(whole.waitForExistence(timeout: 3), "the opened notice does not show the whole command")
        shot(app, "detail")
        let jump = app.buttons["跳到发起的这一步"]
        XCTAssertTrue(jump.exists, "no way back to the call")
        jump.tap()
        let call = app.staticTexts["后台 · 已完成"]
        XCTAssertTrue(call.waitForExistence(timeout: 3) && call.isHittable, "the call is not in view after the jump")
        shot(app, "jumped")
        // The rest of the card: the failed run and the Monitor's end.
        app.swipeUp()
        app.swipeUp()
        shot(app, "end")
        // A long notice that is not a task (a local command's output) is a few left-aligned lines, not a centered block.
        let context = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Context Usage")).firstMatch
        XCTAssertTrue(context.waitForExistence(timeout: 3), "no local command output:\n\(app.debugDescription)")
        XCTAssertLessThan(context.frame.height, 100, "the local command output is \(context.frame.height) pt tall")
        // It is one of the open work's steps, so it starts where they do: 28 pt in on a card, 62 pt on Liquid Glass's
        // timeline (right of the rail). A centered block would be a narrow capsule in the middle instead.
        XCTAssertLessThan(context.frame.minX - app.frame.minX, app.frame.width / 4,
                          "the local command output does not start at the left (\(context.frame.minX) pt)")
        XCTAssertGreaterThan(context.frame.width, app.frame.width / 2,
                             "the local command output is \(context.frame.width) pt wide: a centered block, not a box")
    }
}
