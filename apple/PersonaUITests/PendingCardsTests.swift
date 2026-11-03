import XCTest

/// An agent's pending cards above the composer (#926). One is laid out as it always was. Three (a plan, a question and
/// a permission: more than half the page) scroll on their own under 「3 个待处理」, the permission first and in sight,
/// while the chat's header and back button stay on screen: they were pushed off the top, out of reach.
///   PORT=18797 MOCK_AGENTS=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:PersonaUITests/PendingCardsTests
/// Both layouts (TEST_RUNNER_JARVIS_LAYOUT=shell) and both styles (TEST_RUNNER_JARVIS_UI_STYLE=beautiful).
final class PendingCardsTests: XCTestCase {
    private var app: XCUIApplication!
    /// The mock's agent with three cards waiting, and one with none.
    private static let photos = "studio/a1f3c2d4-0000-4000-8000-000000000005"
    private static let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_AGENTS=1 (JARVIS_MOCK_HUB)")
        }
    }

    private func launch(chat: String, _ extra: [String] = []) {
        let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"]!
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "chats", "-dev.openChat", chat, "-tab", "dev",
                               "-openAgentsFresh", "YES", "-openChatsFresh", "YES"] + extra
        app.launchPinned()
    }

    private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "cards \(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    /// The chat's back button, on screen and in reach.
    private func assertBackInReach(file: StaticString = #filePath, line: UInt = #line) {
        let backs = app.buttons.matching(NSPredicate(format: "label == %@", "返回")).allElementsBoundByIndex
        XCTAssertTrue(backs.contains { $0.isHittable && $0.frame.minY >= 0 }, "the back button is on screen", file: file, line: line)
    }

    func testOneCardAsBefore() {
        launch(chat: Self.widget, ["-showSamplePermission", "YES"])
        XCTAssertTrue(app.buttons["work-head"].firstMatch.waitForExistence(timeout: 15), "the chat")
        sleep(2)
        XCTAssertFalse(app.staticTexts["pending-cards-count"].exists, "one card fits: no count over it")
        // Laid out whole: nothing of the card outside its area (it was cut off at the bottom once).
        let cards = app.scrollViews["pending-cards"]
        XCTAssertTrue(cards.exists, "the cards' area")
        let area = cards.frame.insetBy(dx: -1, dy: -1)
        let outside = cards.descendants(matching: .any).allElementsBoundByIndex.filter { !$0.frame.isEmpty && !area.contains($0.frame) }
        XCTAssertTrue(outside.isEmpty, "cut off: \(outside.map { $0.label }) outside \(cards.frame)")
        assertBackInReach()
        shot("one")
    }

    func testThreeCardsScrollUnderTheHeader() {
        launch(chat: Self.photos)
        let count = app.staticTexts["pending-cards-count"]
        XCTAssertTrue(count.waitForExistence(timeout: 15), "three cards: a count over them")
        sleep(2)
        XCTAssertEqual(count.label, "3 个待处理")
        assertBackInReach()
        // The permission, the newest and most urgent, right under the count and on screen.
        let permission = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "unittest")).firstMatch
        XCTAssertTrue(permission.exists, "the permission card")
        let screen = app.windows.firstMatch.frame
        XCTAssertGreaterThan(permission.frame.minY, count.frame.maxY, "the permission is under the count")
        XCTAssertLessThan(permission.frame.maxY, screen.maxY, "the permission is on screen")
        // At most half the page: the conversation keeps the rest.
        let cards = app.scrollViews["pending-cards"]
        XCTAssertTrue(cards.exists, "the cards' scroll area")
        XCTAssertLessThanOrEqual(cards.frame.height, screen.height / 2, "the cards take at most half the page")
        shot("three")
    }
}
