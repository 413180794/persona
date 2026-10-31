import XCTest

/// 机器监控 (docs/machines.md): the dashboard opens from the Agents list's machine menu with every machine (the busy
/// one called out, the offline one marked), a card opens the machine's charts, and the range switches. Against
/// scripts/mock_hub.py (TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<port>); TEST_RUNNER_JARVIS_SHOT_DIR=<dir> also
/// saves the screens there as PNG for a look.
final class MachineMonitorTests: XCTestCase {
    private func hub() throws -> String {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        return hub
    }

    private func launch(_ hub: String, _ args: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-dev.openChat", ""] + args
        app.launchJarvis()
        return app
    }

    private func shot(_ name: String) {
        let png = XCUIScreen.main.screenshot()
        let a = XCTAttachment(screenshot: png)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOT_DIR"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }

    @MainActor func testDashboardThenMachineThenRange() throws {
        let app = launch(try hub())
        // The machine menu (top left of the Agents list) → 机器监控.
        let menu = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "分组")).firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 15), app.debugDescription)
        menu.tap()
        let item = app.buttons["机器监控"]
        XCTAssertTrue(item.waitForExistence(timeout: 5), app.debugDescription)
        item.tap()

        XCTAssertTrue(app.staticTexts["服务器"].waitForExistence(timeout: 10), "no dashboard:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["MacBook"].exists)
        XCTAssertTrue(app.staticTexts["3/4 台在线"].exists, "the offline NAS should not count")
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "CPU 高")).firstMatch.exists,
                      "the busy server is not called out")
        shot("dashboard")

        app.staticTexts["服务器"].tap()
        let range = app.segmentedControls["metric-range"]
        XCTAssertTrue(range.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.staticTexts["Ubuntu 24.04.1 LTS"].exists)
        XCTAssertTrue(range.buttons["1 小时"].isSelected)
        shot("detail-1h")

        range.buttons["24 小时"].tap()
        XCTAssertTrue(range.buttons["24 小时"].isSelected)
        let cpu = app.descendants(matching: .any)["chart-CPU"]
        XCTAssertTrue(cpu.waitForExistence(timeout: 10), app.debugDescription)
        sleep(1)
        shot("detail-24h")

        // Hold a chart: the moment's time shows in its header.
        app.swipeUp()
        sleep(1)
        let chart = app.descendants(matching: .any)["chart-下行"]
        XCTAssertTrue(chart.waitForExistence(timeout: 5), app.debugDescription)
        let at = chart.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.7))
        at.press(forDuration: 1.2, thenDragTo: chart.coordinate(withNormalizedOffset: CGVector(dx: 0.45, dy: 0.7)))
        shot("detail-24h-lower")
        app.swipeUp()
        sleep(1)
        XCTAssertTrue(app.staticTexts["AMD EPYC 9354 32-Core Processor"].waitForExistence(timeout: 5), "no system facts")
        shot("detail-system")
    }

    /// An offline machine's page: its OS still known, 离线, history up to when it went away.
    @MainActor func testOfflineMachine() throws {
        let app = launch(try hub(), ["-openMonitor", "nas", "-openMonitorRange", "6h"])
        XCTAssertTrue(app.staticTexts["Debian GNU/Linux 12 (bookworm)"].waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertTrue(app.staticTexts["离线"].exists)
        shot("offline-6h")
    }
}
