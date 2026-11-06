import XCTest

/// Links to this hub's own pages open them in the app (#2696), against scripts/mock/mock_hub.py with MOCK_HUB_LINKS=1
/// (`TEST_RUNNER_PERSONA_HUB_LINKS_HUB=<its address>`): an agent's reply links another agent's chat at one of its early
/// messages, a task and an outside page. The first opens that chat with the message on screen, the second the task's
/// detail, the third the web page sheet as before. `TEST_RUNNER_PERSONA_SHOTS=<dir>` writes a picture of each.
final class HubPageLinkUITests: XCTestCase {
    private var hub: String!
    private let source = "macbook/a1f3c2d4-0000-4000-8000-00000000001a"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_HUB_LINKS_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_HUB_LINKS=1 (PERSONA_HUB_LINKS_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", source, "-openAgentsFresh", "YES", "-openChatsFresh", "YES"]
        app.launchPinned()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        guard let directory = env["PERSONA_SHOTS"] else { return }
        let appearance = env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: directory + "/hub-links-\(appearance)-\(name).png"))
    }

    @MainActor private func tapLink(_ name: String, in app: XCUIApplication) {
        let link = app.links[name].firstMatch
        XCTAssertTrue(link.waitForExistence(timeout: 20), "no link \(name):\n\(app.debugDescription)")
        link.tap()
    }

    @MainActor func testAgentLinkOpensItsChatAtTheMessage() throws {
        let app = launch()
        tapLink("发布说明的草稿", in: app)
        let marked = app.staticTexts.matching(NSPredicate(format: "label CONTAINS '这一条是链接要找的消息'")).firstMatch
        XCTAssertTrue(marked.waitForExistence(timeout: 20), "the linked chat did not open:\n\(app.debugDescription)")
        let onScreen = NSPredicate { _, _ in marked.exists && marked.isHittable }
        expectation(for: onScreen, evaluatedWith: NSNull())
        waitForExpectations(timeout: 15)
        XCTAssertFalse(app.buttons["link-close"].exists, "it opened as a web page")
        sleep(1)
        try shot(app, "agent")
    }

    @MainActor func testTaskLinkOpensTheTask() throws {
        let app = launch()
        tapLink("待办：天气 App 发 1.3 版", in: app)
        let notes = app.staticTexts.matching(NSPredicate(format: "label CONTAINS '主屏幕小组件、网络层补测试'")).firstMatch
        XCTAssertTrue(notes.waitForExistence(timeout: 20), "the task's detail did not open:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["link-close"].exists, "it opened as a web page")
        sleep(1)
        try shot(app, "task")
    }

    @MainActor func testOutsideLinkStaysAWebPage() throws {
        let app = launch()
        tapLink("外部的发布指南", in: app)
        XCTAssertTrue(app.buttons["link-close"].waitForExistence(timeout: 15), "the outside page did not open in the sheet:\n\(app.debugDescription)")
    }
}
