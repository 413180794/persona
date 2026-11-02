import XCTest

/// 设置 › 连接器 › 信息 against scripts/mock/mock_hub.py: the Mac's conversations and one of them, read-only (JARVIS_MOCK_HUB);
/// without Full Disk Access on that Mac, the steps to give it (JARVIS_MOCK_HUB_DENIED, a mock with MOCK_IMESSAGE=denied).
final class MessagesConnectorTests: XCTestCase {
    override func setUpWithError() throws { continueAfterFailure = false }

    @MainActor private func launch(_ hub: String, _ extra: [String]) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
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
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py") }
        // From 设置: the hub's connectors list 信息 under 连接器.
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

    @MainActor func testWithoutPermissionItSaysHow() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB_DENIED"] else { throw XCTSkip("needs a mock with MOCK_IMESSAGE=denied") }
        let app = launch(hub, ["-openConnectorMessages", "YES"])
        XCTAssertTrue(app.staticTexts["没有完全磁盘访问权限"].waitForExistence(timeout: 10), "\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["点「远程登录」右边的 ⓘ，打开「允许远程用户完全访问磁盘」"].exists)
        XCTAssertFalse(app.descendants(matching: .any)["messages-chats"].exists)
        shot(app, "denied")
    }
}
