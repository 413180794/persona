import XCTest

/// Sending in a Feishu chat does not wait for Feishu (docs/ios.md「飞书消息渲染」): the message is on screen at once
/// with 发送中, Feishu's own copy takes its place, and one that did not go out offers 重发. Against scripts/mock_hub.py
/// (JARVIS_MOCK_HUB), whose Feishu send takes 2 seconds and fails for a text with "#fail".
final class LarkSendTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func openChat() -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-inbox.kind", "mention", "-inbox.source", "", "-openInboxFirst", "YES"]
        app.launch()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "The new icon set")).firstMatch.waitForExistence(timeout: 20),
                      "the 设计评审 chat did not open")
        return app
    }

    @MainActor private func send(_ app: XCUIApplication, _ text: String) {
        let field = app.textFields.matching(NSPredicate(format: "placeholderValue BEGINSWITH %@", "回复")).firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText(text)
        app.buttons["发送"].tap()
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }

    @MainActor func testMessageShowsAtOnceThenItsCopyTakesOver() {
        let app = openChat()
        let text = "下午三点前我看一下 \(Int.random(in: 1000...9999))"
        send(app, text)
        let sending = app.staticTexts["发送中"]
        XCTAssertTrue(sending.waitForExistence(timeout: 1), "the message is not on screen while it goes")
        XCTAssertTrue(app.staticTexts[text].exists)
        let field = app.textFields.matching(NSPredicate(format: "placeholderValue BEGINSWITH %@", "回复")).firstMatch
        XCTAssertNotEqual(field.value as? String, text, "the box is cleared at once")
        shot(app, "lark-sending")
        XCTAssertTrue(sending.waitForNonExistence(timeout: 15), "it never went out")
        sleep(3) // the copy replaces it: one message with the text, not two
        XCTAssertEqual(app.staticTexts.matching(NSPredicate(format: "label == %@", text)).count, 1)
        shot(app, "lark-sent")
    }

    @MainActor func testFailedMessageOffersRetry() {
        let app = openChat()
        send(app, "#fail 这条发不出去")
        let failed = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "没发出去")).firstMatch
        XCTAssertTrue(failed.waitForExistence(timeout: 10), "a failed send says so")
        shot(app, "lark-failed")
        failed.tap()
        XCTAssertTrue(app.buttons["重发"].waitForExistence(timeout: 3))
        XCTAssertTrue(app.buttons["放回输入框"].exists)
        app.buttons["删除"].tap()
        XCTAssertTrue(failed.waitForNonExistence(timeout: 3))
    }
}
