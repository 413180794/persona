import XCTest

/// A plugin's connector the app has no wording of its own for (#1426), against scripts/mock/mock_hub.py started with
/// MOCK_CONNECTOR_PLUGIN=wechat-connector (TEST_RUNNER_JARVIS_MOCK_HUB_wechat-connector): 设置 › 连接器 › 微信 signs in with its
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

    @MainActor func testwechat-connectorSaysItsOwnWords() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB_wechat-connector"] else { throw XCTSkip("needs a mock with MOCK_CONNECTOR_PLUGIN=wechat-connector") }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", ""]
        app.launchPinned()
        let any = app.descendants(matching: .any)
        let telegram = NSPredicate(format: "label CONTAINS %@", "Telegram")

        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "微信")).firstMatch
        for _ in 0..<8 where !row.isHittable { app.swipeUp() }
        XCTAssertTrue(row.isHittable, "no 微信 row in 设置:\n\(app.debugDescription)")
        row.tap()

        let signIn = any["connector.signIn"]
        XCTAssertTrue(signIn.waitForExistence(timeout: 15), "no 登录 微信:\n\(app.debugDescription)")
        XCTAssertFalse(app.staticTexts.containing(telegram).firstMatch.exists, "says Telegram:\n\(app.debugDescription)")
        shot(app, "signed-out")
        signIn.tap()

        let url = any["connector.field.base_url"]
        XCTAssertTrue(url.waitForExistence(timeout: 10), "no address field:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["应用密钥加密存在 hub 上，只显示账号名；退出登录时一起删掉。"].exists, "not its own sign-in footer:\n\(app.debugDescription)")
        XCTAssertFalse(app.staticTexts.containing(telegram).firstMatch.exists, "says Telegram while signing in")
        // A long placeholder puts the field on its own line under the label: tap that line, at its right end. The lower
        // field first: the keyboard it brings up would cover it if the one above were filled in first.
        func type(_ f: XCUIElement, _ text: String) {
            f.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.8)).tap()
            f.typeText(text)
        }
        type(any["connector.field.app_key"], "example-key")
        type(url, "https://wechat-connector.example.com")
        XCTAssertEqual(url.value as? String, "https://wechat-connector.example.com")
        shot(app, "app-key")
        any["connector.next"].tap()

        XCTAssertTrue(app.staticTexts["wxid_example_a"].waitForExistence(timeout: 15), "not signed in:\n\(app.debugDescription)")
        let logout = any["connector.logout"]
        logout.tap()
        XCTAssertTrue(app.staticTexts["退出 微信 登录？"].waitForExistence(timeout: 5), "no confirmation:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "授权不会自动撤销")).firstMatch.exists,
                      "the confirmation does not say the authorization stays:\n\(app.debugDescription)")
        XCTAssertFalse(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "设备会话也会结束")).firstMatch.exists, "says the session ends")
        shot(app, "logout-confirm")
        let confirm = app.buttons.matching(NSPredicate(format: "label == %@ AND identifier != %@", "退出登录", "connector.logout")).firstMatch
        confirm.tap()
        XCTAssertTrue(signIn.waitForExistence(timeout: 15), "still signed in:\n\(app.debugDescription)")
    }
}
