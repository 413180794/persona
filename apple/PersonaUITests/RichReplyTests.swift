import XCTest

/// A reply with a link in Chinese prose and wide tables (scripts/mock/mock_hub.py MOCK_RICH=1, PERSONA_MOCK_HUB): the link
/// ends at the full-width bracket (it used to run on through the rest of the paragraph); a three-column table with a
/// long Chinese column fits the screen and wraps that column; a seven-column one scrolls sideways.
final class RichReplyTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_RICH=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = "rich-" + name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor func testLinksEndAtCJKAndTablesFit() throws {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let scheme = ProcessInfo.processInfo.environment["PERSONA_SHOTS_SCHEME"] ?? "light"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-ui.scheme", scheme]
        app.launchPinned()
        acceptIconAlert()
        app.terminate()
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-00000000000a", "-ui.scheme", scheme]
        app.launchPinned()
        acceptIconAlert()

        let cell = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "按小时刷新")).firstMatch
        XCTAssertTrue(cell.waitForExistence(timeout: 20), "the reply did not open:\n\(app.debugDescription)")
        // Wait until the reply has settled at its place.
        for _ in 0..<4 where !cell.isHittable { app.swipeDown() }
        sleep(1)
        shot(app, "top")

        // The link is the URL alone.
        let link = app.links.matching(NSPredicate(format: "label CONTAINS %@", "23140")).firstMatch
        XCTAssertTrue(link.exists, "no link for the URL:\n\(app.debugDescription)")
        XCTAssertFalse(link.label.contains("出来") || link.label.contains("）"), "the link took the text after it: \(link.label)")

        // The long column wraps inside the screen: the cell is fully on screen and several lines tall.
        let screen = app.windows.firstMatch.frame
        XCTAssertLessThanOrEqual(cell.frame.maxX, screen.maxX, "the long column runs off the screen: \(cell.frame)")
        XCTAssertGreaterThan(cell.frame.height, 40, "the long cell did not wrap: \(cell.frame)")
        let number = app.staticTexts["#"]
        XCTAssertTrue(number.exists && number.frame.width < 40, "the # column is not narrow: \(number.frame)")

        // The wide table is wider than the screen and says so with its fade.
        let source = app.staticTexts["数据来源"]
        for _ in 0..<4 where !app.staticTexts["城市"].isHittable { app.swipeUp() }
        sleep(1)
        shot(app, "wide")
        XCTAssertTrue(app.otherElements["table-more"].exists, "the wide table does not show it scrolls:\n\(app.debugDescription)")
        XCTAssertGreaterThan(source.frame.maxX, screen.maxX, "the wide table fits after all: \(source.frame)")
    }
}
