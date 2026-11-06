import XCTest

/// The reply box of a connector's conversation page (Telegram's): what the user sends there goes out at once and shows
/// in the page, with nothing to allow on the phone (#1190: the hub's gate is for the AI's writes, not the user's own).
/// Against scripts/mock/mock_hub.py started with MOCK_TELEGRAM=signed_in MOCK_TELEGRAM_INBOX=1
/// (PERSONA_MOCK_HUB_TELEGRAM_THREAD), whose made-up dm takes replies into its page.
final class ConnectorThreadReplyTests: XCTestCase {
    override func setUpWithError() throws { continueAfterFailure = false }

    @MainActor func testAReplyGoesOutWithoutACard() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB_TELEGRAM_THREAD"] else {
            throw XCTSkip("needs a mock with MOCK_TELEGRAM=signed_in MOCK_TELEGRAM_INBOX=1")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-openInboxItem", "telegram:dm1"]
        app.launchPinned()
        func text(_ s: String) -> XCUIElement {
            app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
        }

        let box = app.descendants(matching: .any)["thread.box"]
        XCTAssertTrue(box.waitForExistence(timeout: 20), "the dm has no reply box:\n\(app.debugDescription)")
        let words = "好的，九点见 " + UUID().uuidString.prefix(6)
        box.tap()
        app.typeText(words)
        app.buttons["thread.send"].tap()

        // A bubble's label carries its time too (StampedText): look for the words in it.
        let shown = text(words).waitForExistence(timeout: 5)
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "telegram reply sent \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
        XCTAssertTrue(shown, "the reply is not in the page:\n\(app.debugDescription)")
        XCTAssertFalse(text("确认卡片").exists, "the page still says to allow it on the phone")
        XCTAssertFalse(app.buttons["允许"].exists, "a card asks to allow the user's own reply")
    }
}
