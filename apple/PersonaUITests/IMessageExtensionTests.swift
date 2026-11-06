import XCTest

/// Settings › Extensions › Messages (iMessage) (#1830 step 6) against scripts/mock/mock_hub.py with MOCK_EXTENSIONS=1
/// (TEST_RUNNER_PERSONA_EXTENSIONS_HUB): a switch per Mac, MacBook on; turning on another is refused with the one on named,
/// and nothing changes until that one is off. PERSONA_SHOTS keeps the screenshots, named by PERSONA_EXTENSIONS_APPEARANCE.
final class IMessageExtensionTests: XCTestCase {
    @MainActor func testOneMacOnly() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["PERSONA_EXTENSIONS_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 (PERSONA_EXTENSIONS_HUB)")
        }
        continueAfterFailure = false
        let look = env["PERSONA_EXTENSIONS_APPEARANCE"] ?? "light"
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openExtension", "imessage"]
        app.launchPinned()
        func row(_ name: String) -> XCUIElement { app.switches.matching(NSPredicate(format: "label BEGINSWITH %@", name + "、")).firstMatch }
        let macbook = row("MacBook"), studio = row("Mac Studio")
        XCTAssertTrue(macbook.waitForExistence(timeout: 30), "no switch per Mac:\n\(app.debugDescription)")
        XCTAssertEqual(macbook.value as? String, "1")
        XCTAssertEqual(studio.value as? String, "0")
        XCTAssertFalse(row("服务器").exists, "a Linux machine offered for Messages")
        shot(app, "imessage-ext-\(look)")
        studio.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        let refused = app.staticTexts.containing(NSPredicate(format: "label CONTAINS '只能读一台 Mac，先关掉「MacBook」'")).firstMatch
        XCTAssertTrue(refused.waitForExistence(timeout: 10), "a second Mac was not refused:\n\(app.debugDescription)")
        XCTAssertTrue(waitFor(studio, value: "0"), "the refused Mac shows on")
        XCTAssertEqual(macbook.value as? String, "1")
        shot(app, "imessage-ext-refused-\(look)")
        // MacBook off, then Mac Studio on: allowed.
        macbook.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(macbook, value: "0"), "MacBook off")
        studio.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(studio, value: "1"), "Mac Studio on once MacBook is off")
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: name + ".png"))
    }

    private func waitFor(_ e: XCUIElement, value: String, timeout: TimeInterval = 10) -> Bool {
        let deadline = Date.now.addingTimeInterval(timeout)
        while Date.now < deadline { if e.value as? String == value { return true }; usleep(200_000) }
        return false
    }
}
