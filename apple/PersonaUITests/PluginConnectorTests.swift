import XCTest

/// A plugin's connector the app has no wording of its own for (#1426), against scripts/mock/mock_hub.py started with
/// MOCK_CONNECTOR_PLUGIN=examplechat (TEST_RUNNER_PERSONA_MOCK_HUB_PLUGIN_CONNECTOR): Settings › Connectors › Example Chat signs in with its
/// server's address and an app key, its footers and its sign-out confirmation say nothing of Telegram, and the
/// confirmation says the authorization stays at the service. The mock is left signed out.
final class PluginConnectorTests: XCTestCase {
    override func setUpWithError() throws { continueAfterFailure = false }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor func testPluginConnectorSaysItsOwnWords() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB_PLUGIN_CONNECTOR"] else { throw XCTSkip("needs a mock with MOCK_CONNECTOR_PLUGIN=examplechat") }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", ""]
        app.launchPinned()
        let any = app.descendants(matching: .any)
        let telegram = NSPredicate(format: "label CONTAINS %@", "Telegram")

        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "示例聊天")).firstMatch
        for _ in 0..<8 where !row.isHittable { app.swipeUp() }
        XCTAssertTrue(row.isHittable, "no 示例聊天 row in 设置:\n\(app.debugDescription)")
        row.tap()

        let signIn = any["connector.signIn"]
        XCTAssertTrue(signIn.waitForExistence(timeout: 15), "no 登录 示例聊天:\n\(app.debugDescription)")
        XCTAssertFalse(app.staticTexts.containing(telegram).firstMatch.exists, "says Telegram:\n\(app.debugDescription)")
        shot(app, "signed-out")
        signIn.tap()

        let url = any["connector.field.base_url"]
        XCTAssertTrue(url.waitForExistence(timeout: 10), "no address field:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["登录状态加密存在 hub 上，退出登录时删掉。"].exists, "not the generic sign-in footer:\n\(app.debugDescription)")
        XCTAssertFalse(app.staticTexts.containing(telegram).firstMatch.exists, "says Telegram while signing in")
        // A long placeholder puts the field on its own line under the label: tap that line, at its right end. The lower
        // field first: the keyboard it brings up would cover it if the one above were filled in first.
        func type(_ f: XCUIElement, _ text: String) {
            f.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.8)).tap()
            f.typeText(text)
        }
        type(any["connector.field.app_key"], "example-key")
        type(url, "https://chat.example.com")
        XCTAssertEqual(url.value as? String, "https://chat.example.com")
        shot(app, "app-key")
        any["connector.next"].tap()

        XCTAssertTrue(app.staticTexts["user_example_a"].waitForExistence(timeout: 15), "not signed in:\n\(app.debugDescription)")
        let logout = any["connector.logout"]
        logout.tap()
        XCTAssertTrue(app.staticTexts["退出 示例聊天 登录？"].waitForExistence(timeout: 5), "no confirmation:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "授权不会自动撤销")).firstMatch.exists,
                      "the confirmation does not say the authorization stays:\n\(app.debugDescription)")
        XCTAssertFalse(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "设备会话也会结束")).firstMatch.exists, "says the session ends")
        shot(app, "logout-confirm")
        let confirm = app.buttons.matching(NSPredicate(format: "label == %@ AND identifier != %@", "退出登录", "connector.logout")).firstMatch
        confirm.tap()
        XCTAssertTrue(signIn.waitForExistence(timeout: 15), "still signed in:\n\(app.debugDescription)")
    }
}
