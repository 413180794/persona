import XCTest

/// The chat header's tabs of the space (`SiblingTabs`): the tabs are laid out in a strip with their state, the current
/// one marked; ⋯ lists every tab with its state and the space's page; picking from either switches the chat.
/// Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB): weather has tests (Needs You), widget (Working) and review (Seen).
final class SiblingTabsTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let reset = expectation(description: "重置标签测试数据")
        URLSession.shared.dataTask(with: URL(string: hub + "/mock/reset")!) { _, _, _ in reset.fulfill() }.resume()
        wait(for: [reset], timeout: 5)
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006",
                               "-holdChromeOpen", "YES"] // the header stays open: the test taps in it
        app.launchPinned()
    }

    @MainActor private func shot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = "sibling-\(name)"
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor private func tab(_ label: String) -> XCUIElement {
        let matches = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", label + "，"))
        return matches.allElementsBoundByIndex.first(where: \.isHittable) ?? matches.firstMatch
    }

    @MainActor private func mock(_ path: String) async throws -> [String: Any] {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        let (data, _) = try await URLSession.shared.data(from: URL(string: hub + path)!)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    @MainActor private func closeMenu(_ label: String) {
        tab(label).press(forDuration: 0.8)
        let close = app.buttons["停止并关闭 Agent"]
        XCTAssertTrue(close.waitForExistence(timeout: 3))
        close.tap()
    }

    @MainActor func testWorkingCloseCanBeCancelled() async throws {
        openList()
        closeMenu("widget")
        XCTAssertTrue(app.staticTexts["停止并关闭这个 Agent？"].waitForExistence(timeout: 3))
        shot("close-confirm")
        app.buttons["取消"].tap()
        XCTAssertTrue(tab("widget").waitForExistence(timeout: 3))
        let state = try await mock("/mock/session_close")
        XCTAssertEqual((state["attempts"] as? [String])?.count, 0)
        XCTAssertTrue(tab("review").isSelected)
    }

    @MainActor func testFailedCloseKeepsConfirmationAndTab() async throws {
        _ = try await mock("/mock/session_close?fail=1")
        openList()
        closeMenu("widget")
        XCTAssertTrue(app.buttons["停止并关闭"].waitForExistence(timeout: 3))
        app.buttons["停止并关闭"].tap()
        XCTAssertTrue(app.staticTexts["agent.close.error"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["停止并关闭"].isEnabled)
        shot("close-failed")
        app.buttons["取消"].tap()
        XCTAssertTrue(tab("widget").waitForExistence(timeout: 3))
        let state = try await mock("/mock/session_close")
        XCTAssertEqual((state["closed"] as? [String])?.count, 0)
        XCTAssertEqual((state["attempts"] as? [String])?.count, 1)
        XCTAssertTrue(tab("review").isSelected)
    }

    @MainActor func testQuietSwipeFailureKeepsTheTabAndShowsAnError() async throws {
        _ = try await mock("/mock/session_close?fail=1")
        openList()
        tab("review").swipeLeft()
        let close = app.buttons["停止并关闭 review"]
        XCTAssertTrue(close.waitForExistence(timeout: 3))
        close.tap()
        XCTAssertTrue(app.staticTexts["agent.tabs.close.error"].waitForExistence(timeout: 5))
        XCTAssertTrue(tab("review").isSelected)
        XCTAssertTrue(app.buttons["空间 weather 的所有 Agent"].exists)
        let state = try await mock("/mock/session_close")
        XCTAssertEqual((state["closed"] as? [String])?.count, 0)
        shot("close-quiet-failed")
    }

    @MainActor func testClosingCurrentQuietTabSelectsItsNeighbour() async throws {
        openList()
        tab("review").swipeLeft()
        let close = app.buttons["停止并关闭 review"]
        XCTAssertTrue(close.waitForExistence(timeout: 3))
        shot("close-swipe")
        close.tap()
        XCTAssertTrue(app.buttons["空间 weather 的所有 Agent"].waitForNonExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["停止并关闭这个 Agent？"].exists)
        openList()
        XCTAssertFalse(tab("review").exists)
        XCTAssertTrue(tab("release").isSelected)
        shot("close-selected")
        let state = try await mock("/mock/session_close")
        XCTAssertEqual(state["closed"] as? [String], ["macbook/a1f3c2d4-0000-4000-8000-000000000006"])
    }

    @MainActor func testClosingAnotherQuietTabKeepsTheCurrentTabAndList() async throws {
        openList()
        tab("widget").tap()
        openList()
        closeMenu("review")
        XCTAssertTrue(tab("review").waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["空间 weather 的所有 Agent"].exists)
        XCTAssertTrue(tab("widget").isSelected)
    }

    @MainActor func testClosingTheLastTabReturnsToTheList() async throws {
        openList()
        for label in ["widget", "tests"] {
            closeMenu(label)
            XCTAssertTrue(app.buttons["停止并关闭"].waitForExistence(timeout: 3))
            app.buttons["停止并关闭"].tap()
            XCTAssertTrue(app.staticTexts["停止并关闭这个 Agent？"].waitForNonExistence(timeout: 5))
            XCTAssertTrue(tab(label).waitForNonExistence(timeout: 5))
        }
        closeMenu("release")
        XCTAssertTrue(tab("release").waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["1 个标签页"].exists)
        closeMenu("review")
        XCTAssertTrue(app.buttons["空间 weather 的所有 Agent"].waitForNonExistence(timeout: 5))
        let all = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "同一空间的")).firstMatch
        XCTAssertTrue(all.waitForNonExistence(timeout: 5))
        let filter = app.buttons["agents.filter.all"]
        XCTAssertTrue(filter.waitForExistence(timeout: 5))
        XCTAssertTrue(filter.isHittable)
        let state = try await mock("/mock/session_close")
        XCTAssertEqual((state["closed"] as? [String])?.count, 4)
        shot("close-last")
    }

    /// Opens ⋯'s list (the header folds itself after a few idle seconds, so it is unfolded before each try).
    @MainActor private func openList() {
        let slim = app.buttons.matching(NSPredicate(format: "label ENDSWITH %@", "显示标题和标签")).firstMatch
        let all = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "同一空间的")).firstMatch
        _ = all.waitForExistence(timeout: 15)
        for _ in 0..<3 where !app.buttons["空间 weather 的所有 Agent"].exists {
            if slim.exists, slim.isHittable { slim.tap() }
            if all.exists, all.isHittable { all.tap() }
            _ = app.buttons["空间 weather 的所有 Agent"].waitForExistence(timeout: 2)
        }
        XCTAssertTrue(app.buttons["空间 weather 的所有 Agent"].waitForExistence(timeout: 3), "the list did not open:\n\(app.debugDescription)")
    }

    /// ⋯'s list starts another tab in the same space: the form is preset to it, and the chat switches to the new tab.
    @MainActor func testNewTabFromTheList() throws {
        openList()
        let new = app.buttons["在这个空间开新标签"]
        XCTAssertTrue(new.exists, "no 新标签 in the list")
        new.tap()
        XCTAssertTrue(app.staticTexts["加入空间"].waitForExistence(timeout: 5), "the form did not open:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["weather"].exists, "the form is not preset to the space")
        // The form's first message, not the chat's composer underneath.
        let first = NSPredicate(format: "placeholderValue BEGINSWITH %@", "想让 Claude")
        let field = app.textViews.matching(first).firstMatch.exists ? app.textViews.matching(first).firstMatch : app.textFields.matching(first).firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 3), "no first-message field:\n\(app.debugDescription)")
        field.tap()
        field.typeText("给小组件加深色模式")
        shot("new-form")
        app.buttons["开始对话"].tap()
        XCTAssertTrue(app.buttons["空间 weather 的所有 Agent"].waitForNonExistence(timeout: 5), "the list stayed")
        let title = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "给小组件加深色模式")).firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 10), "did not switch to the new tab:\n\(app.debugDescription)")
        shot("new-tab")
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
