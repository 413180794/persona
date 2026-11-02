import XCTest

/// A new Jarvis conversation opens on the welcome (`AssistantWelcome`: a greeting, what Jarvis can reach, things to ask),
/// in the shell and the old layout, from launch and from the sidebar's 新对话. The shell once showed nothing but the
/// composer there (2026-11-02, the user: 「之前设计的挺好的助手空状态，现在咋全空白了？」). Runs in the BeautifulUI
/// style too (`TEST_RUNNER_JARVIS_UI_STYLE=beautiful`).
final class AssistantWelcomeTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor private func launch(oldLayout: Bool, newConversation: Bool) {
        app = XCUIApplication()
        // `-showHomeState empty`: nothing needs the user, the case that was blank.
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", oldLayout ? "YES" : "NO", "-app.tab", "assistant",
                               "-dev.openChat", "", "-showHomeState", "empty"]
            // Otherwise the demo's main conversation (the last run may have left a new one open).
            + (newConversation ? ["-newAssistant", "YES"] : ["-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"])
        app.launchPinned()
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "\(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    /// The welcome's parts are there and on screen, below the top bar and above the composer.
    @MainActor private func assertWelcome(_ layout: String) {
        let welcome = app.descendants(matching: .any)["assistant-welcome"].firstMatch
        XCTAssertTrue(welcome.waitForExistence(timeout: 15), "\(layout): no welcome on the new conversation")
        shot("\(layout) welcome")
        let window = app.windows.firstMatch.frame
        let greeting = app.staticTexts.matching(NSPredicate(format: "label IN %@", ["早上好", "中午好", "下午好", "晚上好"])).firstMatch
        XCTAssertTrue(greeting.exists, "\(layout): no greeting")
        let reach = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "我能看你的电脑")).firstMatch
        XCTAssertTrue(reach.exists, "\(layout): no line on what Jarvis can reach")
        let ask = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "今天的安排")).firstMatch
        XCTAssertTrue(ask.exists, "\(layout): no things to ask")
        let field = app.textViews.firstMatch.exists ? app.textViews.firstMatch : app.textFields.firstMatch
        for (what, e) in [("greeting", greeting), ("reach", reach), ("first suggestion", ask)] {
            XCTAssertTrue(e.isHittable, "\(layout): the \(what) is not visible")
            let f = e.frame
            XCTAssertTrue(f.width > 20 && f.height > 10, "\(layout): the \(what) is \(f.size)")
            XCTAssertTrue(window.contains(f), "\(layout): the \(what) is off screen at \(f)")
            if field.exists { XCTAssertLessThan(f.maxY, field.frame.minY, "\(layout): the \(what) is under the composer") }
        }
    }

    @MainActor func testTheShellsNewConversation() {
        launch(oldLayout: false, newConversation: true)
        assertWelcome("shell")
    }

    @MainActor func testTheOldLayoutsNewConversation() {
        launch(oldLayout: true, newConversation: true)
        assertWelcome("old")
    }

    /// From a conversation, the sidebar's 新对话 opens the same welcome.
    @MainActor func testNewConversationFromTheSidebar() {
        launch(oldLayout: false, newConversation: false)
        let menu = app.buttons["assistant-sidebar-button"].firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 15), "no sidebar button")
        XCTAssertFalse(app.descendants(matching: .any)["assistant-welcome"].exists, "opened on the welcome, not a conversation")
        sleep(1)
        menu.tapCenter()
        // The pill's own id is under the sidebar's (`assistant-sidebar`): by its label, not the top bar's 新对话.
        let new = app.buttons.matching(NSPredicate(format: "label == %@ AND identifier != %@", "新对话", "assistant-new")).firstMatch
        XCTAssertTrue(new.waitForExistence(timeout: 5), "the sidebar did not open")
        new.tap()
        assertWelcome("sidebar")
    }
}
