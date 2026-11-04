import XCTest

/// 设置 › 扩展 (docs/extensions.md): the list, a plugin's page with its form drawn from its schema, a secret filled in
/// (and never shown again), a setting the hub refuses shown beside its field. Against scripts/mock/mock_hub.py with
/// MOCK_PLUGINS=1: `TEST_RUNNER_JARVIS_PLUGINS_HUB=http://127.0.0.1:<端口>`; `TEST_RUNNER_JARVIS_SHOTS=<目录>` keeps the
/// screenshots, named by `TEST_RUNNER_JARVIS_PLUGINS_APPEARANCE` (light / dark: set with `simctl ui … appearance`).
final class ExtensionSettingsTests: XCTestCase {
    @MainActor func testSetUpAnExtension() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["JARVIS_PLUGINS_HUB"] else { throw XCTSkip("needs mock_hub.py with MOCK_PLUGINS=1 (JARVIS_PLUGINS_HUB)") }
        let look = env["JARVIS_PLUGINS_APPEARANCE"] ?? "light"
        func shot(_ app: XCUIApplication, _ name: String) {
            guard let dir = env["JARVIS_SHOTS"] else { return }
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "plugins-\(name)-\(look).png"))
        }
        // The system's notification prompt, on a fresh simulator: answered so it covers nothing.
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings"]
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }

        // 设置's row: how many, and how many need the user.
        let entry = app.buttons["settings-plugins"]
        XCTAssertTrue(entry.waitForExistence(timeout: 15) || app.staticTexts["设置"].exists, "no settings")
        for _ in 0..<8 where !(entry.exists && entry.isHittable) { app.swipeUp() }
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "6 个 · 2 个要处理")).firstMatch.exists,
                      "the row's summary:\n\(app.debugDescription)")
        shot(app, "home")
        entry.tap()

        let notes = app.buttons["plugin-example-notes"]
        XCTAssertTrue(notes.waitForExistence(timeout: 15), "no plugin list:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["plugin-telegram"].exists && app.buttons["plugin-voice"].exists)
        shot(app, "list")
        notes.tap()

        XCTAssertTrue(app.staticTexts["还缺：密钥「API Key」"].waitForExistence(timeout: 10), "no 还缺:\n\(app.debugDescription)")
        shot(app, "detail")
        // A setting the hub refuses: said beside the field, the draft kept.
        let url = app.textFields.matching(NSPredicate(format: "placeholderValue CONTAINS %@", "notes.example.com")).firstMatch
        XCTAssertTrue(url.waitForExistence(timeout: 5), "no 服务地址 field")
        url.tap()
        url.typeText("nope")
        app.buttons["plugin-save"].tap()
        XCTAssertTrue(app.staticTexts["要是一个网址"].waitForExistence(timeout: 5), "the refused field says nothing:\n\(app.debugDescription)")
        shot(app, "refused")
        url.tap()
        for _ in 0..<4 { url.typeText(XCUIKeyboardKey.delete.rawValue) }
        url.typeText("https://notes.example.org\n") // return: the keyboard goes, nothing is covered
        app.buttons["plugin-save"].tap()
        XCTAssertTrue(app.staticTexts["要是一个网址"].waitForNonExistence(timeout: 5))

        // The secret: typed in hidden, saved, then only 已设置.
        let secret = app.buttons["plugin-secret-api_key"]
        for _ in 0..<6 where !(secret.exists && secret.isHittable) { app.swipeUp() }
        secret.tap()
        let value = app.secureTextFields["plugin-secret-value"]
        XCTAssertTrue(value.waitForExistence(timeout: 5))
        value.tap()
        value.typeText("s3cret-value")
        shot(app, "secret")
        app.buttons["plugin-secret-save"].tap()
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "已设置")).firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "s3cret")).firstMatch.exists, "a secret is never shown")
        PasswordPrompt.assertNone(app, "a plugin's secret")
        for _ in 0..<6 { app.swipeDown() }
        XCTAssertTrue(app.staticTexts["正在运行"].waitForExistence(timeout: 5),
                      "set up, it runs:\n\(app.debugDescription)")
        shot(app, "running")
    }
}
