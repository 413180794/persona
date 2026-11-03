import XCTest

/// Several groups of hubs on one phone (docs/multi-hub.md), against three scripts/mock_hub.py: A alone (家里), B and C one
/// group (公司, each lists the other in its hello's mesh; C owns the MacBook). Both groups have a MacBook with the same
/// machine and session ids and the same interaction ids, so a request sent to the wrong group's hub shows. Start them
/// as docs/testing.md「多 hub」 says and pass TEST_RUNNER_JARVIS_HUB_A / _B / _C (http://127.0.0.1:<port>).
final class MultiHubTests: XCTestCase {
    private struct Hubs { var a: String; var b: String; var c: String }

    private func hubs() throws -> Hubs {
        let env = ProcessInfo.processInfo.environment
        guard let a = env["JARVIS_HUB_A"], let b = env["JARVIS_HUB_B"], let c = env["JARVIS_HUB_C"] else {
            throw XCTSkip("needs three scripts/mock_hub.py (JARVIS_HUB_A, JARVIS_HUB_B, JARVIS_HUB_C)")
        }
        return Hubs(a: a, b: b, c: c)
    }

    private func pair(_ hub: String) -> String {
        "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
    }

    private func launch(_ h: Hubs, _ args: [String]) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair(h.a), "-pairURL2", pair(h.b), "-pairURL3", pair(h.c),
                               "-openAgentsFresh", "YES", "-openChatsFresh", "YES", "-dev.openChat", ""] + args
        app.launchPinned()
        dismissNotificationPrompt()
        return app
    }

    /// A launch without -pairURL before this one (the unit tests' host app) asked for notifications: the system's
    /// alert can still be up, over the app (and the app inactive under it).
    private func dismissNotificationPrompt() {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["不允许", "Don’t Allow", "Don't Allow"] where springboard.alerts.buttons[label].waitForExistence(timeout: 1) {
            springboard.alerts.buttons[label].tap()
            return
        }
    }

    private func mock(_ hub: String, _ path: String) -> [String: Any] {
        guard let data = try? Data(contentsOf: URL(string: hub + path)!),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [:] }
        return obj
    }

    private func resolved(_ hub: String) -> [String] {
        (mock(hub, "/mock/resolved")["resolved"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? String }
    }

    private func shot(_ name: String) {
        let s = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        s.name = name
        s.lifetime = .keepAlways
        add(s)
    }

    private func text(_ app: XCUIApplication, containing s: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    /// The Agents list's ⋯ menu: show one group only ("全部 hub": every group).
    private func showOnly(_ app: XCUIApplication, _ group: String) {
        let menu = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '分组' OR label BEGINSWITH '当前'")).firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 5), "no ⋯ menu:\n\(app.debugDescription)")
        menu.tap()
        let item = app.buttons[group].firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5), "no \(group) in the menu:\n\(app.debugDescription)")
        item.tap()
    }

    /// The Agents list shows both groups' requests, each saying its group, and can show one group only; 允许 on each
    /// goes to that group's hub only (both groups have the same interaction id on the same machine and session ids).
    @MainActor func testMergedListAndAnswersGoToTheirOwnHub() throws {
        let h = try hubs()
        let app = launch(h, ["-tab", "dev"])
        let needs = app.staticTexts["需要你处理"]
        XCTAssertTrue(needs.waitForExistence(timeout: 20))
        let home = text(app, containing: "MacBook · 家里")
        XCTAssertTrue(home.waitForExistence(timeout: 10), "the first group's MacBook does not say its group:\n\(app.debugDescription)")
        shot("merged")

        // Only the second group: its request shows, the first group's do not.
        showOnly(app, "公司")
        let work = text(app, containing: "工作 MacBook · 公司")
        XCTAssertTrue(work.waitForExistence(timeout: 10), "no request from the second group:\n\(app.debugDescription)")
        XCTAssertFalse(text(app, containing: "· 家里").exists, "the hub filter left the first group's rows")
        shot("second group only")

        let beforeA = resolved(h.a), beforeWork = resolved(h.b) + resolved(h.c)
        let workCard = app.cells.containing(NSPredicate(format: "label CONTAINS %@", "工作 MacBook · 公司")).firstMatch
        XCTAssertTrue(workCard.buttons["允许"].waitForExistence(timeout: 5), "no 允许 on the second group's card")
        workCard.buttons["允许"].tap()
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline, resolved(h.b).count + resolved(h.c).count == beforeWork.count { usleep(200_000) }
        let work2 = resolved(h.b) + resolved(h.c)
        XCTAssertEqual(work2.count, beforeWork.count + 1, "the second group's hubs did not get the answer")
        XCTAssertEqual(resolved(h.a), beforeA, "the first group's hub got the second group's answer")

        // Every group again, and the first group's own (the same interaction id, at the other hub).
        showOnly(app, "全部 hub")
        let homeCard = app.cells.containing(NSPredicate(format: "label CONTAINS %@", "MacBook · 家里")).firstMatch
        XCTAssertTrue(homeCard.buttons["允许"].waitForExistence(timeout: 10), "no 允许 on the first group's card")
        homeCard.buttons["允许"].tap()
        let d2 = Date.now.addingTimeInterval(10)
        while Date.now < d2, resolved(h.a).count == beforeA.count { usleep(200_000) }
        XCTAssertEqual(resolved(h.a).count, beforeA.count + 1, "the first group's hub did not get its answer")
        XCTAssertEqual((resolved(h.b) + resolved(h.c)).count, work2.count, "the second group got the first group's answer")
        shot("answered")
    }

    /// 设置 › hub lists the two groups (B and C one group); disconnecting 公司 leaves 家里 and its agents as they were.
    @MainActor func testDisconnectingOneGroupLeavesTheOther() throws {
        let h = try hubs()
        let app = launch(h, ["-tab", "settings", "-openHubs", "YES"])
        XCTAssertTrue(app.navigationBars["hub"].waitForExistence(timeout: 20), "no hub page:\n\(app.debugDescription)")
        // Two groups: the second one has both its hubs.
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "18761")).firstMatch
            .waitForExistence(timeout: 10))
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "18762")).firstMatch.exists)
        let removes = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'hubs.remove.'"))
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline, removes.count < 2 { usleep(200_000) } // B and C merge once their hellos are in
        XCTAssertEqual(removes.count, 2, "B and C did not become one group:\n\(app.debugDescription)")
        shot("hubs")
        // The second group's 断开这一组 (the default group is first).
        removes.element(boundBy: 1).tap()
        let confirm = app.buttons["断开这一组"].firstMatch
        XCTAssertTrue(confirm.waitForExistence(timeout: 5))
        confirm.tap()
        let gone = NSPredicate(format: "count == 1")
        expectation(for: gone, evaluatedWith: removes)
        waitForExpectations(timeout: 10)
        shot("one group left")

        // Back to one group: Agents is what it was with one hub (no group names), the first hub's agents all there.
        let relaunch = XCUIApplication()
        relaunch.launchArguments = ["-pairURL", pair(h.a), "-openAgentsFresh", "YES", "-dev.openChat", "", "-tab", "dev"]
        relaunch.launchPinned()
        XCTAssertTrue(text(relaunch, containing: "补齐网络层的单元测试").waitForExistence(timeout: 20))
        XCTAssertFalse(text(relaunch, containing: "· 家里").exists, "one group but rows still name it")
        XCTAssertFalse(text(relaunch, containing: "公司").exists, "the disconnected group's rows are still there")
    }

    /// The hub the second group is on goes away: the group goes on through its other hub.
    @MainActor func testGroupMovesToItsOtherHub() throws {
        let h = try hubs()
        let app = launch(h, ["-tab", "settings", "-openHubs", "YES"])
        XCTAssertTrue(app.navigationBars["hub"].waitForExistence(timeout: 20), "no hub page:\n\(app.debugDescription)")
        func row(_ port: String) -> XCUIElement {
            app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'hubs.hub.' AND label CONTAINS %@", port)).firstMatch
        }
        let b = row("18761"), c = row("18762")
        XCTAssertTrue(b.waitForExistence(timeout: 10) && c.exists)
        let deadline = Date.now.addingTimeInterval(15)
        while Date.now < deadline, !b.label.contains("在用"), !c.label.contains("在用") { usleep(300_000) }
        let onB = b.label.contains("在用")
        XCTAssertTrue(onB || c.label.contains("在用"), "neither of the second group's hubs is in use: \(b.label) / \(c.label)")
        let (down, other) = onB ? (h.b, c) : (h.c, b)
        _ = mock(down, "/mock/down?s=40")
        let moved = NSPredicate(format: "label CONTAINS '在用'")
        expectation(for: moved, evaluatedWith: other)
        waitForExpectations(timeout: 30)
        shot("moved")
        _ = mock(down, "/mock/down?s=0")
    }
}
