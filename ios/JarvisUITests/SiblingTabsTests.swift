import XCTest

/// The chat header's tabs of the space (`SiblingTabs`): the tabs are laid out in a strip with their state, the current
/// one marked; ⋯ lists every tab with its state and the space's page; picking from either switches the chat.
/// Against scripts/mock_hub.py (JARVIS_MOCK_HUB): weather has tests (需要你), widget (工作中) and review (已看).
final class SiblingTabsTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006",
                               "-holdChromeOpen", "YES"] // the header stays open: the test taps in it
        app.launch()
    }

    @MainActor private func shot(_ name: String) {
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/sibling-\(name).png"))
    }

    @MainActor private func tab(_ label: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", label + "，")).firstMatch
    }

    @MainActor func testTabsStripAndList() throws {
        // The header folds itself after a moment: open it.
        let slim = app.buttons.matching(NSPredicate(format: "label ENDSWITH %@", "显示标题和标签")).firstMatch
        if slim.waitForExistence(timeout: 15), slim.isHittable { slim.tap() }
        // The strip: its tabs sit too deep in the view tree for XCUITest to list (one `Other` stands for them), so it
        // is found by its frame and tapped by position.
        let strip = app.scrollViews.allElementsBoundByIndex.first { $0.frame.height < 60 && $0.frame.minY < 200 }
        XCTAssertNotNil(strip, "no tab strip in the header:\n\(app.debugDescription)")
        shot("strip")

        // ⋯ lists them all with their states, the current one selected, and the space's page.
        let all = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "同一空间的")).firstMatch
        XCTAssertTrue(all.exists, "no ⋯")
        // The header folds itself after 3 idle seconds: open it again right before each try.
        for _ in 0..<3 where !app.buttons["空间 weather 的所有 Agent"].exists {
            if slim.exists, slim.isHittable { slim.tap() }
            usleep(600_000) // the unfold animation
            all.tap()
            _ = app.buttons["空间 weather 的所有 Agent"].waitForExistence(timeout: 2)
        }
        XCTAssertTrue(app.buttons["空间 weather 的所有 Agent"].waitForExistence(timeout: 3), "the list did not open:\n\(app.debugDescription)")
        let review = tab("review"), tests = tab("tests"), widget = tab("widget")
        XCTAssertTrue(review.exists && review.label.contains("已看") && review.isSelected, "review: \(review.label)")
        XCTAssertTrue(tests.exists && tests.label.contains("需要你"), "no tests with its state")
        XCTAssertTrue(widget.exists && widget.label.contains("工作中"), "no widget with its state")
        shot("list")
        // Picking from the list switches and closes it.
        widget.tap()
        XCTAssertTrue(app.buttons["空间 weather 的所有 Agent"].waitForNonExistence(timeout: 3), "the list stayed")
        let widgetTitle = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "给天气 App")).firstMatch
        XCTAssertTrue(widgetTitle.waitForExistence(timeout: 10), "did not switch to widget")

        // A tap on the strip's second tab (tests; widget, now current, is first) switches too.
        // The header folded again after the switch: open it (the strip is only tappable while it is open).
        if slim.exists, slim.isHittable { slim.tap() }
        usleep(600_000)
        guard let strip = app.scrollViews.allElementsBoundByIndex.first(where: { $0.frame.height < 60 && $0.frame.minY < 200 }),
              strip.isHittable else {
            return XCTFail("no tappable strip after switching:\n\(app.debugDescription)")
        }
        // 100 pt in: the second tab (widget is about 64 pt wide). The strip's width varies (the loop / background
        // chips beside it take some), so not a fraction of it.
        strip.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0.5)).withOffset(CGVector(dx: 100, dy: 0)).tap()
        let switched = NSPredicate { _, _ in !widgetTitle.exists }
        XCTAssertEqual(XCTWaiter.wait(for: [expectation(for: switched, evaluatedWith: nil)], timeout: 10), .completed,
                       "a tap on the strip did not switch")
        shot("switched")
    }
}
