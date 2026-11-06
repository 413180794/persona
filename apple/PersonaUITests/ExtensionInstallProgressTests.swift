import XCTest

/// Extension Market › an extension installed and removed step by step (#3307): the steps and the one going on, the step it
/// failed at with why, Retry from there, the failure still there after leaving the page (and on the list's row), and a
/// removal that failed done again. Against scripts/mock/mock_hub.py with
/// `MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=fail`: `TEST_RUNNER_PERSONA_EXTENSIONS_OPS_HUB=http://127.0.0.1:<port>` (restart the mock
/// before each run: it fails each thing once). `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots, named by
/// `TEST_RUNNER_PERSONA_EXTENSIONS_APPEARANCE` (light / dark) and the style.
final class ExtensionInstallProgressTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }

    @MainActor private func launch(hub key: String = "PERSONA_EXTENSIONS_OPS_HUB") throws -> XCUIApplication {
        guard let hub = env[key] else {
            throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 and MOCK_EXTENSION_OPS=fail (\(key))")
        }
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings"]
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = env["PERSONA_EXTENSIONS_APPEARANCE"] ?? "light"
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "ext-op-\(name)-\(UITestStyle.name)-\(look).png"))
    }

    @MainActor private func scrollTo(_ el: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<8 where !(el.exists && el.isHittable) { app.swipeUp() }
    }

    @MainActor private func value(_ el: XCUIElement) -> String { el.value as? String ?? "" }

    /// A step's row, scrolled to (the list draws only the rows on screen).
    @MainActor private func step(_ id: String, in app: XCUIApplication) -> XCUIElement {
        let row = app.descendants(matching: .any)["extension-operation-step-\(id)"]
        for _ in 0..<6 where !row.exists { app.swipeUp() }
        return row
    }

    @MainActor private func waitFor(_ el: XCUIElement, value want: String, timeout: TimeInterval) -> Bool {
        let end = Date.now.addingTimeInterval(timeout)
        while Date.now < end {
            if el.exists, value(el) == want { return true }
            usleep(150_000)
        }
        return false
    }

    @MainActor func testInstallStepsFailureAndRetry() throws {
        let app = try launch()
        let entry = app.buttons["settings-plugins"]
        XCTAssertTrue(entry.waitForExistence(timeout: 15) || app.staticTexts["设置"].exists, "no settings:\n\(app.debugDescription)")
        scrollTo(entry, in: app)
        entry.tap()
        let market = app.buttons["plugins-market"]
        XCTAssertTrue(market.waitForExistence(timeout: 15), "no 扩展市场:\n\(app.debugDescription)")
        market.tap()
        let calendar = app.buttons["market-plugin-example-calendar"]
        XCTAssertTrue(calendar.waitForExistence(timeout: 15), "no catalog:\n\(app.debugDescription)")
        calendar.tap()

        let install = app.buttons["market-install"]
        XCTAssertTrue(install.waitForExistence(timeout: 10), "no 安装:\n\(app.debugDescription)")
        install.tap()

        // The hub downloads and checks in the background: the page lists its steps, the download first, with its bar.
        let operation = app.descendants(matching: .any)["extension-operation"]
        XCTAssertTrue(operation.waitForExistence(timeout: 10), "no progress on the page:\n\(app.debugDescription)")
        let download = app.descendants(matching: .any)["extension-operation-step-download"]
        XCTAssertTrue(download.waitForExistence(timeout: 5), "no steps:\n\(app.debugDescription)")
        XCTAssertTrue(app.descendants(matching: .any)["extension-operation-step-verify"].exists, "the steps to come are listed too")
        XCTAssertTrue(waitFor(download, value: "running", timeout: 5) || value(download) == "done")
        shot(app, "running")

        // Checked: the preview opens by itself, the operation waits for the user's yes.
        let confirm = app.buttons["market-confirm"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 20), "no preview once it was checked:\n\(app.debugDescription)")
        shot(app, "consent")
        confirm.tap()
        XCTAssertTrue(confirm.waitForNonExistence(timeout: 5), "the preview sheet stays up while it installs")

        // It stops at Start: the step, why, Retry.
        let retry = app.buttons["extension-operation-retry"]
        XCTAssertTrue(retry.waitForExistence(timeout: 20), "no failure:\n\(app.debugDescription)")
        XCTAssertEqual(value(step("start", in: app)), "failed")
        for _ in 0..<6 where !retry.isHittable { app.swipeDown() }
        let error = app.descendants(matching: .any)["extension-operation-error"]
        XCTAssertTrue(error.exists && error.label.contains("启动") && error.label.contains("exit status 1"), "no step or reason: \(error.label)")
        XCTAssertFalse(app.buttons["market-uninstall"].exists, "not installed yet")
        shot(app, "failed")

        // Left and opened again: the failure is still there, and the list's row says so.
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(calendar.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["安装失败"].waitForExistence(timeout: 5) || calendar.label.contains("安装失败"),
                      "the row does not say it failed:\n\(app.debugDescription)")
        calendar.tap()
        XCTAssertTrue(retry.waitForExistence(timeout: 10), "the failure is gone after coming back:\n\(app.debugDescription)")

        // Retry: a new operation, the package from the hub's cache and the permissions already agreed to, so it finishes.
        retry.tap()
        XCTAssertTrue(waitFor(operation, value: "running", timeout: 5) || app.buttons["market-uninstall"].exists, "retry did not start")
        let remove = app.buttons["market-uninstall"]
        XCTAssertTrue(remove.waitForExistence(timeout: 20), "not installed after 重试:\n\(app.debugDescription)")
        XCTAssertTrue(operation.waitForNonExistence(timeout: 5), "the card stays after it went through")
        shot(app, "installed")

        // A removal that fails: where and why, and Retry removes it.
        scrollTo(remove, in: app)
        remove.tap()
        let removeConfirm = app.buttons["market-uninstall-confirm"].firstMatch
        XCTAssertTrue(removeConfirm.waitForExistence(timeout: 5))
        removeConfirm.tap()
        XCTAssertTrue(retry.waitForExistence(timeout: 20), "the removal did not fail:\n\(app.debugDescription)")
        XCTAssertEqual(value(step("remove", in: app)), "failed")
        for _ in 0..<6 where !retry.isHittable { app.swipeDown() }
        shot(app, "uninstall-failed")
        retry.tap()
        XCTAssertTrue(install.waitForExistence(timeout: 20), "not removed after 重试:\n\(app.debugDescription)")
    }

    /// A hub from before operations (MOCK_EXTENSIONS=1 MOCK_EXTENSION_INSTALL_DELAY=4,
    /// `TEST_RUNNER_PERSONA_EXTENSIONS_OLD_HUB`): no steps, but the page says it is installing and for how long, then it is.
    @MainActor func testAnOlderHubSaysWhatItIsDoing() throws {
        let app = try launch(hub: "PERSONA_EXTENSIONS_OLD_HUB")
        let entry = app.buttons["settings-plugins"]
        XCTAssertTrue(entry.waitForExistence(timeout: 15) || app.staticTexts["设置"].exists, "no settings:\n\(app.debugDescription)")
        scrollTo(entry, in: app)
        entry.tap()
        let market = app.buttons["plugins-market"]
        XCTAssertTrue(market.waitForExistence(timeout: 15))
        market.tap()
        let calendar = app.buttons["market-plugin-example-calendar"]
        XCTAssertTrue(calendar.waitForExistence(timeout: 15))
        calendar.tap()
        let install = app.buttons["market-install"]
        XCTAssertTrue(install.waitForExistence(timeout: 10))
        install.tap()
        let confirm = app.buttons["market-confirm"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 10))
        confirm.tap()
        XCTAssertTrue(app.descendants(matching: .any)["extension-operation-waiting"].waitForExistence(timeout: 5),
                      "no word on what the hub is doing:\n\(app.debugDescription)")
        XCTAssertFalse(app.descendants(matching: .any)["extension-operation-step-download"].exists, "an older hub has no steps")
        shot(app, "older-hub")
        let remove = app.buttons["market-uninstall"]
        XCTAssertTrue(remove.waitForExistence(timeout: 20), "not installed:\n\(app.debugDescription)")
        XCTAssertFalse(app.descendants(matching: .any)["extension-operation"].exists, "the card stays after it went through")
        // Removed again, so the next run finds it as before.
        scrollTo(remove, in: app)
        remove.tap()
        let removeConfirm = app.buttons["market-uninstall-confirm"].firstMatch
        XCTAssertTrue(removeConfirm.waitForExistence(timeout: 5))
        removeConfirm.tap()
        XCTAssertTrue(install.waitForExistence(timeout: 20))
    }
}
