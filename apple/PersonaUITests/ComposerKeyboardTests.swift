import XCTest

/// The keyboard over an Agents chat (2026-11-04, iPhone Duo open): a button by the composer puts it away, and under
/// the side bars (`-sideBars YES` stands in for Duo's on any simulator) the slim header folds while typing so the chat
/// keeps its rows, and comes back with the keyboard down. Demo data, both layouts; both styles with
/// `TEST_RUNNER_JARVIS_UI_STYLE=beautiful`.
final class ComposerKeyboardTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let a = XCTAttachment(string: app.debugDescription)
            a.name = "tree"
            a.lifetime = .keepAlways
            add(a)
        }
        super.tearDown()
    }

    @MainActor private func openChat(oldLayout: Bool, sideBars: Bool) {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", oldLayout ? "YES" : "NO", "-app.tab", "chats", "-tab", "dev",
                               "-dev.openChat", "", "-openAgentsFresh", "YES", "-holdChromeOpen", "YES",
                               "-sideBars", sideBars ? "YES" : "NO"]
        app.launchPinned()
        let row = app.buttons.matching(identifier: "agents.row").firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "no agent in the list")
        sleep(1)
        row.tap()
        XCTAssertTrue(field.waitForExistence(timeout: 10), "the chat did not open")
        sleep(2)
    }

    @MainActor private var field: XCUIElement { app.textViews.firstMatch.exists ? app.textViews.firstMatch : app.textFields.firstMatch }
    @MainActor private var dismissButton: XCUIElement { app.buttons["收起键盘"] }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "\(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    /// The sibling tabs' labels in the header: buttons above the chat's newest message, under the status bar.
    @MainActor private func headerButtons() -> Set<String> {
        let limit = field.frame.minY - 120
        return Set(app.buttons.allElementsBoundByIndex.filter { $0.isHittable && $0.frame.maxY < limit && $0.frame.minY > 40 }.map(\.label))
    }

    @MainActor private func typeAndDismiss(_ what: String, sideBars: Bool) {
        XCTAssertFalse(dismissButton.exists, "\(what): 收起键盘 before the keyboard is up")
        let before = headerButtons()
        field.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "\(what): no keyboard")
        XCTAssertTrue(dismissButton.waitForExistence(timeout: 3), "\(what): no 收起键盘 while typing")
        XCTAssertTrue(dismissButton.isHittable, "\(what): 收起键盘 not hittable")
        XCTAssertLessThan(dismissButton.frame.maxY, app.keyboards.firstMatch.frame.minY, "\(what): 收起键盘 under the keyboard")
        sleep(1)
        shot("\(what) typing")
        if sideBars {
            let typing = headerButtons()
            XCTAssertFalse(before.subtracting(typing).isEmpty, "\(what): the header kept \(typing) while typing (was \(before))")
        }
        dismissButton.tap()
        let gone = NSPredicate(format: "exists == false")
        expectation(for: gone, evaluatedWith: app.keyboards.firstMatch)
        waitForExpectations(timeout: 5)
        XCTAssertFalse(dismissButton.waitForExistence(timeout: 1), "\(what): 收起键盘 stays after the keyboard went down")
        sleep(1)
        shot("\(what) dismissed")
        if sideBars {
            XCTAssertEqual(headerButtons(), before, "\(what): the header did not come back")
        }
    }

    @MainActor func testDismissInTheShell() {
        openChat(oldLayout: false, sideBars: false)
        typeAndDismiss("shell", sideBars: false)
    }

    @MainActor func testDismissInTheOldLayout() {
        openChat(oldLayout: true, sideBars: false)
        typeAndDismiss("old layout", sideBars: false)
    }

    @MainActor func testSideBarsHeaderFoldsWhileTyping() {
        openChat(oldLayout: true, sideBars: true)
        typeAndDismiss("side bars", sideBars: true)
    }
}
