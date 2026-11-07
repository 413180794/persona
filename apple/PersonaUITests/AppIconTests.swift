import XCTest

/// Both remaining personas use the default icon. Against mock_hub.py with MOCK_PERSONAS=builtin.
final class AppIconTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    /// On Settings › Appearance › Avatar with `persona` chosen as the app starts.
    @MainActor private func launch(_ persona: String) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openAppearance", "persona", "-openPersona", persona]
        app.launchPinned()
        return app
    }

    @MainActor private func shot(_ name: String, _ image: XCUIScreenshot) throws {
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_ICON_SHOTS"] else { return }
        try image.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "\(name).png"))
    }

    @MainActor private func waitIcon(_ app: XCUIApplication, _ icon: String, _ why: String) {
        let note = app.descendants(matching: .any)["persona-icon-note"]
        XCTAssertTrue(note.waitForExistence(timeout: 15), "no icon note:\n\(app.debugDescription)")
        let on = expectation(for: NSPredicate(format: "value == %@", icon), evaluatedWith: note)
        XCTAssertEqual(XCTWaiter.wait(for: [on], timeout: 15), .completed, "\(why): the icon is \(note.value ?? "?")")
    }

    /// The notification permission prompt left over from launch, declined; any other alert is left alone.
    @MainActor private func dismissPermissionPrompt() {
        let board = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["不允许", "Don’t Allow", "Don't Allow"] where board.alerts.buttons[label].waitForExistence(timeout: 1) {
            board.alerts.buttons[label].tap()
            return
        }
    }

    @MainActor private func home(_ name: String) throws {
        XCUIDevice.shared.press(.home)
        sleep(2)
        try shot(name, XCUIScreen.main.screenshot())
    }

    @MainActor func testTheLookDecidesTheHomeScreenIcon() throws {
        let app = launch("persona")
        dismissPermissionPrompt()
        // The line under Avatar says which icon is on, or "unavailable" where the system cannot switch icons
        // (`supportsAlternateIcons`): iOS 26.0.1 and 26.3.1 simulators cannot (docs/lessons/ios.md), a device can.
        XCTAssertTrue(app.buttons["persona-persona"].waitForExistence(timeout: 20), "no 形象 page:\n\(app.debugDescription)")
        let note = app.descendants(matching: .any)["persona-icon-note"]
        XCTAssertTrue(note.waitForExistence(timeout: 10), "no line on the icon under 形象:\n\(app.debugDescription)")
        if note.value as? String == "unavailable" {
            throw XCTSkip("this simulator cannot switch app icons (supportsAlternateIcons is false)")
        }
        acceptIconAlert() // a simulator left on another icon goes back to Mochi's first
        waitIcon(app, "standard", "团子 should be on the default icon")

        let frog = app.buttons["persona-nwverse"]
        XCTAssertTrue(frog.waitForExistence(timeout: 20), "Milk Frog is not offered")
        frog.tap()
        waitIcon(app, "standard", "Milk Frog should use the default icon")
        try shot("look-milk-frog-page", app.screenshot())
        try home("look-standard-home")

        app.terminate()
        let again = launch("persona")
        acceptIconAlert()
        waitIcon(again, "standard", "the launch on 团子 did not bring its icon back")
        try home("look-standard-home")
    }
}

extension XCTestCase {
    /// The system's 「你已更改“…”的图标」 alert, from SpringBoard; none within the wait is fine.
    @MainActor func acceptIconAlert(timeout: TimeInterval = 5) {
        let board = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let ok = board.alerts.buttons.firstMatch
        if ok.waitForExistence(timeout: timeout) { ok.tap() }
    }
}
