import XCTest

/// Against the mock hub (#1291): 设置 › 助手 shows no model. The settings row carries no launcher name and the page has
/// no 对话模型 / 模型配置 rows; the assistant's model is 模型配置 › 主对话 only.
final class AssistantSettingsModelTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let value = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py")
        }
        hub = value
    }

    @MainActor private func launch(_ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", ""] + extra
        app.launchPinned()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            let suffix = ProcessInfo.processInfo.environment["JARVIS_SHOT_SUFFIX"].map { "-" + $0 } ?? ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + suffix + ".png"))
        }
    }

    @MainActor func testSettingsRowHasNoLauncher() {
        let app = launch()
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "助手")).firstMatch
        for _ in 0..<4 where !row.isHittable { app.swipeUp() }
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        // The mock's assistant runs the launcher "claude": the row no longer names it.
        XCTAssertFalse(row.label.contains("claude"), row.label)
        shot(app, "settings-assistant-row")
    }

    @MainActor func testAssistantPageHasNoModelRows() {
        let app = launch(["-openAssistantPage", "assistant"])
        let workdir = app.staticTexts["工作目录"].firstMatch
        XCTAssertTrue(app.staticTexts["定时任务"].firstMatch.waitForExistence(timeout: 10))
        for _ in 0..<6 where !workdir.isHittable { app.swipeUp() }
        XCTAssertTrue(workdir.exists)
        XCTAssertFalse(app.buttons["model-config-link"].exists, "no 模型配置 shortcut on 设置 › 助手")
        XCTAssertFalse(app.staticTexts["对话模型"].exists, "no 对话模型 row on 设置 › 助手")
        shot(app, "assistant-page-settings")
    }
}
