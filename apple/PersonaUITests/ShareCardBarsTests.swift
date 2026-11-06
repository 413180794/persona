import XCTest

/// The share card's preview (long-press a message › Share, `MessageShareSheet`) has the image viewer's bars (#4714): a round close
/// button and Light / Dark in a capsule on top; Share · Save and Copy in a capsule · Share Text at the bottom, every one inside
/// the screen. Run it on the narrowest phone too (SE).
///   PORT=18832 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18832 xcodebuild test … -only-testing:PersonaUITests/ShareCardBarsTests
/// `TEST_RUNNER_PERSONA_SHOTS=<dir>` writes `share-card-<SHOTS_NAME>.png` (`TEST_RUNNER_PERSONA_SHOTS_NAME`, e.g. se-dark).
final class ShareCardBarsTests: XCTestCase {
    private var hub = ""
    private let session = "macbook/a1f3c2d4-0000-4000-8000-000000000006"

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = false
    }

    @MainActor func testBarsFitTheScreen() throws {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", session, "-showShareCard", "YES"]
        app.launchPinned()
        let save = app.buttons["保存"]
        XCTAssertTrue(save.waitForExistence(timeout: 30), "the share card did not open:\n\(app.debugDescription)")
        _ = XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "isEnabled == true"), object: save)], timeout: 10)
        XCTAssertTrue(save.isEnabled, "保存 stays disabled: the card never rendered")

        let screen = app.windows.firstMatch.frame
        let controls: [XCUIElement] = [
            app.buttons["share-card-close"], app.segmentedControls["share-card-style"],
            app.buttons["分享"], save, app.buttons["分享文字"], app.buttons["拷贝"],
        ]
        for c in controls {
            XCTAssertTrue(c.waitForExistence(timeout: 10), "missing \(c):\n\(app.debugDescription)")
            XCTAssertTrue(screen.contains(c.frame), "\(c.label) at \(c.frame) is not inside the screen \(screen)")
        }
        // The bottom row, on one line: Share · [Save Copy] · Share Text.
        XCTAssertLessThan(app.buttons["分享"].frame.maxX, save.frame.minX)
        XCTAssertLessThanOrEqual(save.frame.maxX, app.buttons["拷贝"].frame.minX)
        XCTAssertLessThan(app.buttons["拷贝"].frame.maxX, app.buttons["分享文字"].frame.minX)
        XCTAssertEqual(app.buttons["分享"].frame.midY, app.buttons["分享文字"].frame.midY, accuracy: 2)

        let env = ProcessInfo.processInfo.environment
        if let dir = env["PERSONA_SHOTS"] {
            // A fresh install asks for notifications over the page: answer it so the shot shows the bars.
            let alert = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.firstMatch
            if alert.waitForExistence(timeout: 2) { alert.buttons.element(boundBy: 0).tap() }
            let name = env["PERSONA_SHOTS_NAME"] ?? "shot"
            try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("share-card-\(name).png"))
        }

        // Light / Dark still switches the card, and the close button still closes the sheet.
        let darkSegment = app.segmentedControls["share-card-style"].buttons.element(boundBy: 1)
        darkSegment.tap()
        let selected = XCTNSPredicateExpectation(predicate: NSPredicate(format: "isSelected == true"), object: darkSegment)
        XCTAssertEqual(XCTWaiter.wait(for: [selected], timeout: 5), .completed, "深色 did not get selected")
        app.buttons["share-card-close"].tap()
        XCTAssertTrue(save.waitForNonExistence(timeout: 5), "关闭 did not close the share card")
    }
}
