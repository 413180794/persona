import XCTest

/// 设置 › 外观 › App 图标 (docs/ios.md「App 图标」): picking an icon really changes the home screen one, through the
/// system's alert, and the page says which is on. Runs in the demo mode (no hub). Screenshots go to JARVIS_ICON_SHOTS
/// when set: the page after the switch and the home screen with the new icon.
final class AppIconTests: XCTestCase {
    override func setUpWithError() throws { continueAfterFailure = false }

    @MainActor private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-tab", "settings", "-openAppearance", "icon"]
        app.launchPinned()
        return app
    }

    /// The system's 「你已更改“…”的图标」 alert, from SpringBoard.
    @MainActor private func acceptSystemAlert() {
        let board = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let ok = board.alerts.buttons.firstMatch
        if ok.waitForExistence(timeout: 5) { ok.tap() }
    }

    /// A notification permission prompt left over from launch, out of the way.
    @MainActor private func dismissPermissionPrompt() {
        let board = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let alert = board.alerts.firstMatch
        if alert.waitForExistence(timeout: 3) { alert.buttons.element(boundBy: 1).tap() }
    }

    @MainActor private func shot(_ name: String, _ image: XCUIScreenshot) throws {
        guard let dir = ProcessInfo.processInfo.environment["JARVIS_ICON_SHOTS"] else { return }
        try image.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "\(name).png"))
    }

    @MainActor func testSwitchesTheHomeScreenIconAndBack() throws {
        let app = launch()
        dismissPermissionPrompt()
        let aura = app.buttons["app-icon-aura"]
        XCTAssertTrue(aura.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["app-icon-standard"].isSelected)

        aura.tap()
        acceptSystemAlert()
        XCTAssertTrue(aura.wait(for: \.isSelected, toEqual: true, timeout: 5))
        XCTAssertFalse(app.buttons["app-icon-standard"].isSelected)
        try shot("icon-page", app.screenshot())

        XCUIDevice.shared.press(.home)
        sleep(2)
        try shot("icon-home", XCUIScreen.main.screenshot())

        // The choice is the system's: a fresh launch reads it back.
        app.terminate()
        let again = launch()
        XCTAssertTrue(again.buttons["app-icon-aura"].wait(for: \.isSelected, toEqual: true, timeout: 10))

        // Back to the default, for the tests after.
        again.buttons["app-icon-standard"].tap()
        acceptSystemAlert()
        XCTAssertTrue(again.buttons["app-icon-standard"].wait(for: \.isSelected, toEqual: true, timeout: 5))
    }
}
