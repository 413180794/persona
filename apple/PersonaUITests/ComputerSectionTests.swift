import XCTest

/// 操控电脑 on a Mac's page (docs/computer-use.md), against scripts/mock/mock_hub.py with `MOCK_COMPUTER=<state>`
/// (TEST_RUNNER_JARVIS_COMPUTER_HUB=http://127.0.0.1:<port>, TEST_RUNNER_JARVIS_COMPUTER_STATE=<state>): the section
/// says the state, and its switch turns it off and on. JARVIS_SHOTS saves the screenshots there too.
final class ComputerSectionTests: XCTestCase {
    @MainActor func testTheSwitchAndTheState() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_COMPUTER_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_COMPUTER (JARVIS_COMPUTER_HUB)")
        }
        let state = ProcessInfo.processInfo.environment["JARVIS_COMPUTER_STATE"] ?? "ready"
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "macbook"]
        app.launchPinned()

        let toggle = app.switches["computer-switch"].firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 30), "no 操控电脑 switch:\n\(app.debugDescription)")
        for _ in 0..<6 where !toggle.isHittable || toggle.frame.maxY > app.frame.height * 0.6 { app.swipeUp() }
        let status = app.descendants(matching: .any)["computer-status"].firstMatch
        if state == "off" {
            XCTAssertEqual(toggle.value as? String, "0")
            XCTAssertFalse(status.exists, "nothing to say until it is on")
            shot(app, "computer-off")
            return
        }
        XCTAssertEqual(toggle.value as? String, "1")
        let says = state == "ready" ? "现在可以操控 · Cua Driver 0.38.0" : state == "locked" ? "锁屏" : "屏幕录制权限"
        XCTAssertTrue(status.waitForExistence(timeout: 10) && status.label.contains(says), "\(status.label)")
        shot(app, "computer-\(state)")

        toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(toggle, value: "0"), "turned off")
        XCTAssertTrue(waitGone(status), "its state goes with it")
        toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(toggle, value: "1") && status.waitForExistence(timeout: 10), "on again")
    }

    private func waitFor(_ e: XCUIElement, value: String) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if e.value as? String == value { return true }; usleep(200_000) }
        return false
    }

    private func waitGone(_ e: XCUIElement) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if !e.exists { return true }; usleep(200_000) }
        return false
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let look = ProcessInfo.processInfo.environment["JARVIS_LOOK"].map { "-" + $0 } ?? ""
        let full = "\(name)-\(UITestStyle.name)\(look)"
        let png = app.screenshot()
        let attachment = XCTAttachment(screenshot: png)
        attachment.name = full
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(full + ".png"))
        }
    }
}
