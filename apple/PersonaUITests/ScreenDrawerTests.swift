import XCTest

/// The live screen as a drawer (#1343): while a chat's model operates a Mac its running work says so (「正在操作
/// MacBook · 点 AC → 9 → × → 8」) and opens the screen like the terminal's drawer, over it the step, dragged down it
/// closes; the ··· menus have 看画面 whether or not the model is at it. Against scripts/mock/mock_hub.py with
/// MOCK_COMPUTER=ready MOCK_COMPUTER_OPERATING=1 MOCK_WEB=web/dist (`TEST_RUNNER_JARVIS_SCREEN_HUB`); JARVIS_SHOTS keeps
/// the screenshots, named by `TEST_RUNNER_JARVIS_LOOK`.
final class ScreenDrawerTests: XCTestCase {
    private let calc = "server/b2e4d6f8-0000-4000-8000-0000000000c1"
    private let widget = "a1f3c2d4-0000-4000-8000-000000000001"

    @MainActor private func launch(_ args: [String]) throws -> XCUIApplication {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_SCREEN_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_COMPUTER=ready MOCK_COMPUTER_OPERATING=1 (JARVIS_SCREEN_HUB)")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=" + UITestToken.device
        app.launchArguments = ["-pairURL", pair] + args
        app.launchPinned()
        return app
    }

    @MainActor func testTheAssistantsOperationOpensTheScreen() throws {
        let app = try launch(["-app.tab", "assistant", "-tab", "assistant", "-openAssistant", calc])
        let pill = app.descendants(matching: .any)["computer-activity"].firstMatch
        XCTAssertTrue(pill.waitForExistence(timeout: 30), "no running work for the Mac:\n\(app.debugDescription)")
        XCTAssertTrue(pill.label.contains("正在操作 MacBook") && pill.label.contains("点 AC → 9 → × → 8"), pill.label)
        shot(app, "screen-pill-assistant")
        pill.tap()

        let step = app.descendants(matching: .any)["screen-step"].firstMatch
        XCTAssertTrue(step.waitForExistence(timeout: 10), "no drawer:\n\(app.debugDescription)")
        XCTAssertTrue(step.label.contains("正在：点 AC → 9 → × → 8"), step.label)
        let signIn = app.webViews.staticTexts["登录这台 Mac 的屏幕共享"].firstMatch
        XCTAssertTrue(signIn.waitForExistence(timeout: 30), "the screen did not come up:\n\(app.debugDescription)")
        XCTAssertTrue(app.descendants(matching: .any)["screen-phase"].firstMatch.label.contains("要登录"))
        shot(app, "screen-drawer")

        // Dragged down, it closes (the terminal's drawer).
        let handle = app.descendants(matching: .any)["screen-close"].firstMatch
        handle.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
            .press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.98)))
        XCTAssertTrue(waitGone(step), "dragged down, still up")
    }

    @MainActor func testAnAgentsOperationShows() throws {
        let app = try launch(["-openSession", "macbook/" + widget, "-openAgentsFresh", "YES", "-openChatsFresh", "YES"])
        let pill = app.descendants(matching: .any)["computer-activity"].firstMatch
        XCTAssertTrue(pill.waitForExistence(timeout: 30), "no running work for the Mac:\n\(app.debugDescription)")
        XCTAssertTrue(pill.label.contains("正在操作 MacBook") && pill.label.contains("点 小组件"), pill.label)
        shot(app, "screen-pill-agent")
    }

    @MainActor func testTheMenusHaveTheScreen() throws {
        let app = try launch(["-app.tab", "assistant", "-tab", "assistant", "-openAssistant", calc])
        let face = app.buttons["jarvis-avatar"].firstMatch
        XCTAssertTrue(face.waitForExistence(timeout: 30))
        face.tap()
        let item = app.buttons["看画面 · MacBook"].firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5), "no 看画面 in the menu:\n\(app.debugDescription)")
        shot(app, "screen-menu")
        item.tap()
        XCTAssertTrue(app.descendants(matching: .any)["screen-drawer"].firstMatch.waitForExistence(timeout: 10), "the menu's 看画面 opens it")
    }

    private func waitGone(_ e: XCUIElement) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if !e.exists { return true }; usleep(200_000) }
        return false
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let look = ProcessInfo.processInfo.environment["JARVIS_LOOK"].map { "-" + $0 } ?? ""
        let full = "\(name)-\(UITestStyle.name)\(look)"
        let png = app.screenshot()
        let attachment = XCTAttachment(screenshot: png)
        attachment.name = full
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(full + ".png"))
        }
    }
}
