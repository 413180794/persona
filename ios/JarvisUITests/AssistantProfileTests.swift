import XCTest

/// Jarvis's profile (its avatar on the Jarvis tab) and its schedule: the routines list and a routine's page, 立即运行
/// for a routine and for a follow-up. Against scripts/mock_hub.py (JARVIS_MOCK_HUB), which records what was run at
/// /mock/schedule.
final class AssistantProfileTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-newAssistant", "YES"]
        app.launch()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/jarvis-profile-\(name).png"))
    }

    /// What the mock hub was asked to run.
    private func ran() throws -> [String] {
        let data = try Data(contentsOf: URL(string: hub + "/mock/schedule")!)
        let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        return obj?["ran"] as? [String] ?? []
    }

    @MainActor private func openProfile(_ app: XCUIApplication) {
        let avatar = app.buttons["jarvis-profile"]
        XCTAssertTrue(avatar.waitForExistence(timeout: 15), "no avatar on the Jarvis tab:\n\(app.debugDescription)")
        avatar.tap()
        XCTAssertTrue(app.staticTexts["你的私人助手 · 运行在 Server"].waitForExistence(timeout: 5)
                      || app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "你的私人助手")).firstMatch.waitForExistence(timeout: 5),
                      "the profile did not open:\n\(app.debugDescription)")
    }

    @MainActor func testProfileRoutineAndRunNow() throws {
        let app = launch()
        openProfile(app)
        XCTAssertTrue(app.staticTexts["接下来"].exists || app.staticTexts["接下来".uppercased()].exists, "no 接下来 section")
        for row in ["定时任务", "已安排的跟进", "运行记录", "关于我", "记忆"] {
            let b = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", row)).firstMatch
            for _ in 0..<4 where !(b.exists && b.isHittable) { app.swipeUp() }
            XCTAssertTrue(b.exists, "no \(row) row:\n\(app.debugDescription)")
        }
        shot(app, "profile")
        for _ in 0..<4 { app.swipeDown() }

        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "定时任务")).firstMatch.tap()
        let morning = app.descendants(matching: .any)["routine-morning"]
        XCTAssertTrue(morning.waitForExistence(timeout: 5), "no 早报 routine:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["每个工作日 上午 9:00"].exists, "the rule is not in words")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "上次")).firstMatch.exists, "no last run")
        shot(app, "routines")

        morning.tap()
        let runNow = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "立即运行")).firstMatch
        XCTAssertTrue(runNow.waitForExistence(timeout: 5), "the routine page did not open")
        let history = app.staticTexts["历史记录"]
        for _ in 0..<4 where !(history.exists && history.isHittable) { app.swipeUp() }
        XCTAssertTrue(history.exists, "no history on the routine page:\n\(app.debugDescription)")
        XCTAssertTrue(runNow.isHittable, "立即运行 scrolled away")
        shot(app, "routine")
        runNow.tap()
        let deadline = Date().addingTimeInterval(10)
        while try !ran().contains("morning"), Date() < deadline { usleep(200_000) }
        XCTAssertTrue(try ran().contains("morning"), "立即运行 did not reach the hub")
        // It opens the conversation it started: the profile is gone.
        XCTAssertTrue(app.buttons["jarvis-profile"].waitForExistence(timeout: 5), "not back on the conversation")
        XCTAssertFalse(app.staticTexts["历史记录"].exists, "still on the routine page")
    }

    @MainActor func testFollowupRunNow() throws {
        let app = launch()
        openProfile(app)
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "定时任务")).firstMatch.tap()
        let ci = app.descendants(matching: .any)["followup-fu_ci"].firstMatch
        XCTAssertTrue(app.staticTexts["接下来"].waitForExistence(timeout: 5), "the schedule page did not open")
        for _ in 0..<5 where !(ci.exists && ci.isHittable) { app.swipeUp() }
        XCTAssertTrue(ci.exists && ci.isHittable, "no follow-up row on the schedule page:\n\(app.debugDescription)")
        ci.press(forDuration: 1.0)
        let run = app.buttons["立即运行"].firstMatch
        XCTAssertTrue(run.waitForExistence(timeout: 5), "no 立即运行 in the menu:\n\(app.debugDescription)")
        run.tap()
        let deadline = Date().addingTimeInterval(10)
        while try !ran().contains("fu_ci"), Date() < deadline { usleep(200_000) }
        XCTAssertTrue(try ran().contains("fu_ci"), "the follow-up was not run")
    }
}
