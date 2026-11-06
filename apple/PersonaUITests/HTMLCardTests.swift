import XCTest

/// The assistant's html cards (#2025): nothing runs before Run; a page's `persona.reply` is shown in a native dialog
/// first, and a card sends one reply at most. Needs no hub (`-showSampleHTMLCard YES` shows sample cards).
///   xcodebuild test … -only-testing:PersonaUITests/HTMLCardTests
final class HTMLCardTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws { continueAfterFailure = false }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "html card \(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor func testOpenRequiresConfirmationAndDeclinesMuteThePage() {
        app = XCUIApplication()
        app.launchArguments = ["-showSampleHTMLCard", "open"]
        app.launchPinned()
        let choose = app.webViews.buttons["打开链接"].firstMatch
        XCTAssertTrue(choose.waitForExistence(timeout: 20))
        choose.tap()
        let alert = app.alerts["卡片要打开链接"]
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        XCTAssertTrue(alert.staticTexts["https://example.com/card"].exists)
        shot("open asking")
        alert.buttons["取消"].tap()
        choose.tap()
        XCTAssertFalse(app.alerts.firstMatch.waitForExistence(timeout: 2), "a declined page kept asking")
        XCTAssertFalse(app.staticTexts["html-card-opened"].exists)

        // The second card is the running one; restart that same card, not the first waiting card.
        app.buttons["html-card-stop"].firstMatch.tap()
        app.buttons.matching(identifier: "html-card-run").element(boundBy: 1).tap()
        XCTAssertTrue(choose.waitForExistence(timeout: 5))
        choose.tap()
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        alert.buttons["打开"].tap()
        XCTAssertTrue(app.staticTexts["html-card-opened"].waitForExistence(timeout: 5))
        choose.tap()
        XCTAssertTrue(alert.waitForExistence(timeout: 5), "a repeat open must still ask")
        alert.buttons["取消"].tap()
        choose.tap()
        XCTAssertFalse(app.alerts.firstMatch.waitForExistence(timeout: 2), "a declined page kept asking")
        shot("open completed")
    }

    @MainActor func testRunsOnTapAndAsksBeforeReplying() {
        app = XCUIApplication()
        app.launchArguments = ["-showSampleHTMLCard", "YES"]
        app.launchPinned()
        let run = app.buttons["html-card-run"]
        XCTAssertTrue(run.waitForExistence(timeout: 20), "the waiting card is not there")
        // One page runs (the second card runs by itself), the first waits for Run.
        XCTAssertTrue(app.descendants(matching: .any)["html-card-page"].waitForExistence(timeout: 10))
        XCTAssertEqual(app.descendants(matching: .any).matching(identifier: "html-card-page").count, 1)
        shot("first")

        let choose = app.webViews.buttons["选 B"].firstMatch
        XCTAssertTrue(choose.waitForExistence(timeout: 10), "the page's button is not there")
        choose.tap()
        let alert = app.alerts["卡片要替你回复"]
        XCTAssertTrue(alert.waitForExistence(timeout: 5), "no dialog before the reply")
        XCTAssertTrue(alert.staticTexts["选 B"].exists, "the dialog does not show what is sent")
        shot("asking")
        alert.buttons["发送"].tap()
        XCTAssertTrue(app.staticTexts["html-card-replied"].waitForExistence(timeout: 5), "the reply was not sent")

        // Sent once: the page asking again shows nothing.
        choose.tap()
        XCTAssertFalse(app.alerts.firstMatch.waitForExistence(timeout: 2), "a card sent a second reply")

        run.tap()
        XCTAssertTrue(app.buttons["html-card-run"].waitForNonExistence(timeout: 5), "运行 did not start the page")
        XCTAssertEqual(app.descendants(matching: .any).matching(identifier: "html-card-page").count, 2)
        // Stop takes the page away again.
        app.buttons["html-card-stop"].firstMatch.tap()
        XCTAssertTrue(app.buttons["html-card-run"].waitForExistence(timeout: 5))
        shot("stopped")
    }
}
