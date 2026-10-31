import XCTest

/// 设置 › 外观 › 形象 (docs/persona.md), against scripts/mock_hub.py started with MOCK_PERSONAS=demo (JARVIS_MOCK_HUB): the
/// hub's persona is offered next to the built-in Jarvis; choosing it downloads it, and the Jarvis tab's top bar takes
/// its name and face; choosing Jarvis again brings the name back.
final class PersonaPickerTests: XCTestCase {
    private var hub: String!
    /// mock_hub's main conversation: its name pill shows the persona's name (a side conversation's shows its title).
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB) with MOCK_PERSONAS=demo")
        }
        self.hub = hub
    }

    /// Whatever happened, the simulator is left on the built-in: other tests read "Jarvis" in the bar.
    override func tearDown() {
        if let hub {
            // `-openPersona jarvis` takes effect as the app starts.
            let app = XCUIApplication()
            let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
            app.launchArguments = ["-pairURL", pair, "-openPersona", "jarvis"]
            app.launchJarvis()
            _ = app.tabBars.firstMatch.waitForExistence(timeout: 10)
            app.terminate()
        }
        super.tearDown()
    }

    /// On the Jarvis tab's main conversation. `-openPersona jarvis`: from the built-in, whatever the last run chose.
    @MainActor private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openAssistant", main, "-openPersona", "jarvis"]
        app.launchJarvis()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/jarvis-persona-\(name).png"))
    }

    @MainActor private func waitSelected(_ e: XCUIElement, _ why: String) {
        let selected = expectation(for: NSPredicate(format: "selected == true"), evaluatedWith: e)
        XCTAssertEqual(XCTWaiter.wait(for: [selected], timeout: 20), .completed, why)
    }

    @MainActor private func topBarName(_ app: XCUIApplication) -> String {
        app.tabBars.buttons["Jarvis"].tap()
        let face = app.buttons["jarvis-avatar"]
        XCTAssertTrue(face.waitForExistence(timeout: 15), "no face in the top bar:\n\(app.debugDescription)")
        return face.label
    }

    /// 设置 › 外观: the settings tab remembers its page, so the second time it is already open.
    @MainActor private func openAppearance(_ app: XCUIApplication) {
        app.tabBars.buttons["设置"].tap()
        if app.buttons["persona-jarvis"].waitForExistence(timeout: 2) { return }
        let row = app.buttons["settings-appearance"]
        XCTAssertTrue(row.waitForExistence(timeout: 10), "no 外观 row:\n\(app.debugDescription)")
        row.tap()
    }

    @MainActor func testChoosingAPersonaChangesJarvis() throws {
        let app = launch()
        XCTAssertTrue(topBarName(app).hasPrefix("Jarvis"), "the bar should start on the built-in")
        openAppearance(app)
        let builtin = app.buttons["persona-jarvis"]
        let demo = app.buttons["persona-demo"]
        XCTAssertTrue(demo.waitForExistence(timeout: 20), "the hub's persona is not offered:\n\(app.debugDescription)")
        XCTAssertTrue(builtin.isSelected, "the built-in should be the one on screen")
        XCTAssertTrue(app.descendants(matching: .any)["persona-states"].exists, "no state preview")
        shot(app, "settings-jarvis")

        demo.tap()
        waitSelected(app.buttons["persona-demo"], "choosing the hub's persona did not take")
        // The whole app was rebuilt with the new colors and is still on 外观.
        XCTAssertTrue(app.navigationBars["外观"].exists, "the rebuild left 设置 › 外观:\n\(app.debugDescription)")
        shot(app, "settings-demo")
        XCTAssertTrue(topBarName(app).hasPrefix("小薄荷"), "the top bar does not use the persona's name")
        shot(app, "topbar-demo")

        openAppearance(app)
        let back = app.buttons["persona-jarvis"]
        XCTAssertTrue(back.waitForExistence(timeout: 10))
        back.tap()
        waitSelected(app.buttons["persona-jarvis"], "choosing Jarvis again did not take")
        XCTAssertTrue(topBarName(app).hasPrefix("Jarvis"), "the top bar did not go back to Jarvis")
    }
}
