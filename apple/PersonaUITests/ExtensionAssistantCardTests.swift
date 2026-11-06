import XCTest

/// Checks the unified renderer in a real conversation: persona-view and the old persona-card are both read-only, links still work.
/// With MOCK_EXTENSION_CARDS=1 the screenshot has both kinds of card; no new DEBUG launch switch.
final class ExtensionAssistantCardTests: XCTestCase {
    @MainActor func testCardsShareTheRendererAndStayReadOnly() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["PERSONA_EXTENSIONS_HUB"] else { throw XCTSkip("需要 MOCK_EXTENSION_CARDS=1") }
        continueAfterFailure = false
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"]
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for title in ["允许", "Allow"] where springboard.buttons[title].waitForExistence(timeout: 3) {
            springboard.buttons[title].tap()
        }
        XCTAssertTrue(app.staticTexts["今天步数"].waitForExistence(timeout: 15), app.debugDescription)
        let overview = app.staticTexts["本周概览"]
        for _ in 0..<4 where !overview.isHittable { app.swipeDown() }
        XCTAssertTrue(overview.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(app.staticTexts["周会摘要"].exists)
        XCTAssertTrue(app.descendants(matching: .any)["plugin-ui-link"].exists || app.staticTexts["查看说明"].exists,
                      "只读卡片的链接保留：\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["plugin-ui-action-must-not-run"].exists)
        XCTAssertFalse(app.buttons["不应运行"].exists)
        if let dir = env["PERSONA_SHOTS"] {
            let appearance = env["PERSONA_EXTENSIONS_APPEARANCE"] ?? "light"
            try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir)
                .appending(path: "assistant-cards-\(UITestStyle.name)-\(appearance).png"))
        }
    }
}
