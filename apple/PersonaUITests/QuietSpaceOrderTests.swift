import XCTest

/// MOCK_QUIET=order puts the latest idle reply in the last tab, reproducing the quiet-space ordering regression.
final class QuietSpaceOrderTests: XCTestCase {
    private var app: XCUIApplication!
    private let newest = "刷新缓存索引"
    private let middle = "首页信息流加下拉刷新"
    private let oldest = "抓取 RSS 时跳过重复条目"

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "failure"
            shot.lifetime = .keepAlways
            add(shot)
            let tree = XCTAttachment(string: app.debugDescription)
            tree.name = "view tree"
            tree.lifetime = .keepAlways
            add(tree)
        }
        super.tearDown()
    }

    @MainActor private func row(_ title: String, compact: Bool) -> XCUIElement {
        app.buttons.matching(identifier: compact ? "agents.compactRow" : "agents.row")
            .matching(NSPredicate(format: "label CONTAINS %@", title)).firstMatch
    }

    @MainActor private func assertOrder(compact: Bool) {
        let rows = [newest, middle, oldest].map { row($0, compact: compact) }
        app.bringAboveTabBar(rows[2])
        for r in rows { XCTAssertTrue(r.waitForExistence(timeout: 5)) }
        XCTAssertLessThan(rows[0].frame.minY, rows[1].frame.minY, "the latest reply precedes the first tab")
        XCTAssertLessThan(rows[1].frame.minY, rows[2].frame.minY, "idle replies follow update time")
        XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "展开 ")).firstMatch.exists,
                       "opening a quiet space exposes its rows without another idle fold")
    }

    @MainActor private func shot(_ name: String) throws {
        let screenshot = app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let path = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let appearance = ProcessInfo.processInfo.environment["PERSONA_STYLE"] ?? "system"
            try screenshot.pngRepresentation.write(to: URL(fileURLWithPath: path)
                .appendingPathComponent("quiet-order-\(name)-\(UITestStyle.name)-\(appearance).png"))
        }
    }

    @MainActor func testExpandedQuietSpaceUsesLatestActivityInCardsAndCompactRows() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_QUIET_ORDER_HUB"] else {
            throw XCTSkip("needs the MOCK_QUIET=order hub (PERSONA_QUIET_ORDER_HUB)")
        }
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow"] where alert.buttons[label].exists {
                alert.buttons[label].tap()
                return true
            }
            return false
        }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "chats", "-tab", "dev", "-dev.openChat", "",
                               "-openAgentsFresh", "YES", "-dev.grouping", "space"]
        app.launchPinned()
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.buttons
            .matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allow.waitForExistence(timeout: 2) { allow.tap() }
        XCTAssertTrue(app.buttons["agents.filter.all"].waitForExistence(timeout: 20))

        for compact in [false, true] {
            if compact {
                let menu = app.buttons.matching(identifier: "shell-agents").allElementsBoundByIndex.first(where: \.isHittable)
                    ?? app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "分组：")).firstMatch
                menu.tap()
                let item = app.buttons["细列表"].firstMatch
                XCTAssertTrue(item.waitForExistence(timeout: 5))
                item.tap()
            }
            let header = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "news-dev")).firstMatch
            app.bringAboveTabBar(header)
            XCTAssertTrue(header.waitForExistence(timeout: 10))
            if !row(newest, compact: compact).exists { header.tap() }
            assertOrder(compact: compact)
            try shot(compact ? "compact" : "cards")
            app.bringAboveTabBar(header)
            header.tap()
            XCTAssertFalse(row(newest, compact: compact).exists)
            header.tap()
            assertOrder(compact: compact)
        }
    }
}
