import XCTest

/// Settings › Connectors › Messages against scripts/mock/mock_hub.py: the Mac's conversations and one of them, read-only (PERSONA_MOCK_HUB);
/// without Full Disk Access on that Mac, the steps to give it (PERSONA_MOCK_HUB_DENIED, a mock with MOCK_IMESSAGE=denied).
final class MessagesConnectorTests: XCTestCase {
    override func setUpWithError() throws { continueAfterFailure = false }

    @MainActor private func launch(_ hub: String, _ extra: [String]) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", ""] + extra
        app.launchPinned()
        return app
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor func testConversationsAndAThread() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py") }
        // From Settings: the hub's connectors list Messages under Connectors.
        let app = launch(hub, [])
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "信息")).firstMatch
        for _ in 0..<8 where !row.isHittable { app.swipeUp() }
        XCTAssertTrue(row.isHittable, "no 信息 row in 设置:\n\(app.debugDescription)")
        shot(app, "settings")
        row.tap()
        let chats = app.descendants(matching: .any)["messages-chats"]
        XCTAssertTrue(chats.waitForExistence(timeout: 10), "no 会话 on the 信息 page:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["MacBook 上的信息"].exists)
        chats.tap()
        let zhang = app.descendants(matching: .any)["messages-chat-imessage-0a1b2c3d4e5f"]
        XCTAssertTrue(zhang.waitForExistence(timeout: 10), "no conversations:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["周末徒步"].exists)
        shot(app, "chats")
        zhang.tap()
        XCTAssertTrue(app.staticTexts["改一下：三点半"].waitForExistence(timeout: 10), "no messages:\n\(app.debugDescription)")
        for text in ["已编辑", "张三撤回了一条消息", "这条消息的格式读不出来", "👍 好的，周五见 🎉"] {
            XCTAssertTrue(app.staticTexts[text].exists, "missing \(text)")
        }
        shot(app, "thread")
    }

    /// Hand to Persona: on to begin with; off lets the hub's copy be deleted, after a confirmation.
    @MainActor func testTheSwitchAndDelete() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py") }
        let app = launch(hub, ["-openConnectorMessages", "YES"])
        let toggle = app.switches["messages-ai"]
        XCTAssertTrue(toggle.waitForExistence(timeout: 10), "no 交给 Persona:\n\(app.debugDescription)")
        let delete = app.buttons["messages-delete"]
        XCTAssertFalse(delete.isEnabled, "delete while on")
        shot(app, "switch-on")
        toggle.switches.firstMatch.tap()
        XCTAssertTrue(app.staticTexts["现在不交给 Persona：不同步、不进收件箱，助手读不到。hub 上之前存下的还在，可以删除。"].waitForExistence(timeout: 10))
        XCTAssertTrue(delete.isEnabled)
        delete.tap()
        app.buttons["删除"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["已删除 6 个会话、9 个人、1 件收件箱条目"].waitForExistence(timeout: 10), "\(app.debugDescription)")
        shot(app, "switch-off-deleted")
        toggle.switches.firstMatch.tap() // on again, for the other tests on this mock
        XCTAssertFalse(delete.isEnabled)
    }

    @MainActor func testWithoutPermissionItSaysHow() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB_DENIED"] else { throw XCTSkip("needs a mock with MOCK_IMESSAGE=denied") }
        let app = launch(hub, ["-openConnectorMessages", "YES"])
        XCTAssertTrue(app.staticTexts["没有完全磁盘访问权限"].waitForExistence(timeout: 10), "\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["点「远程登录」右边的 ⓘ，打开「允许远程用户完全访问磁盘」"].exists)
        XCTAssertFalse(app.descendants(matching: .any)["messages-chats"].exists)
        shot(app, "denied")
    }
}
