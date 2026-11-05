import XCTest

/// The home screen icon is the 形象's (docs/persona.md「App 图标」, #3203): choosing 面罩 in 设置 › 外观 › 形象 really
/// changes the home screen icon, through the system's alert; a launch on another 形象 (the old App 图标 setting left
/// the icon apart from it) brings the icon back to that 形象's. Against scripts/mock/mock_hub.py (JARVIS_MOCK_HUB).
/// The icon on now is the value of the page's note (`persona-icon-note`). Screenshots go to JARVIS_ICON_SHOTS when set:
/// the page and the home screen after each switch.
final class AppIconTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
    }

    /// On 设置 › 外观 › 形象 with `persona` chosen as the app starts.
    @MainActor private func launch(_ persona: String) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openAppearance", "persona", "-openPersona", persona]
        app.launchPinned()
        return app
    }

    @MainActor private func shot(_ name: String, _ image: XCUIScreenshot) throws {
        guard let dir = ProcessInfo.processInfo.environment["JARVIS_ICON_SHOTS"] else { return }
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
        // The line under 形象 says which icon is on, or "unavailable" where the system cannot switch icons
        // (`supportsAlternateIcons`): iOS 26.0.1 and 26.3.1 simulators cannot (docs/lessons/ios.md), a device can.
        XCTAssertTrue(app.buttons["persona-jarvis"].waitForExistence(timeout: 20), "no 形象 page:\n\(app.debugDescription)")
        let note = app.descendants(matching: .any)["persona-icon-note"]
        XCTAssertTrue(note.waitForExistence(timeout: 10), "no line on the icon under 形象:\n\(app.debugDescription)")
        if note.value as? String == "unavailable" {
            throw XCTSkip("this simulator cannot switch app icons (supportsAlternateIcons is false)")
        }
        acceptIconAlert() // a simulator left on another icon goes back to 团子's first
        waitIcon(app, "standard", "团子 should be on the default icon")

        let visor = app.buttons["persona-jarvis"]
        XCTAssertTrue(visor.waitForExistence(timeout: 10), "面罩 is not offered:\n\(app.debugDescription)")
        visor.tap()
        acceptIconAlert()
        waitIcon(app, "visor", "choosing 面罩 did not change the icon")
        try shot("look-visor-page", app.screenshot())
        try home("look-visor-home")

        // The old setting's case: the icon is apart from the 形象 (here 面罩's icon with 团子 chosen). The first time
        // the app is in front the 形象 wins.
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
