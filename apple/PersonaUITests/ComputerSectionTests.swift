import XCTest

/// 操控电脑 on a Mac's page (docs/computer-use.md), against scripts/mock/mock_hub.py with `MOCK_COMPUTER=<state>`
/// (TEST_RUNNER_JARVIS_COMPUTER_HUB=http://127.0.0.1:<port>, TEST_RUNNER_JARVIS_COMPUTER_STATE=<state>): the section
/// says the state, and its switch turns it off and on; not installed, it installs and asks for the permissions. JARVIS_SHOTS
/// saves the screenshots there too.
final class ComputerSectionTests: XCTestCase {
    private var state: String { ProcessInfo.processInfo.environment["JARVIS_COMPUTER_STATE"] ?? "ready" }

    /// The page, scrolled to the section: its switch and its state line.
    @MainActor private func open() throws -> (XCUIApplication, toggle: XCUIElement, status: XCUIElement) {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_COMPUTER_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_COMPUTER (JARVIS_COMPUTER_HUB)")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "macbook"]
        app.launchPinned()
        let toggle = app.switches["computer-switch"].firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 30), "no 操控电脑 switch:\n\(app.debugDescription)")
        for _ in 0..<6 where !toggle.isHittable || toggle.frame.maxY > app.frame.height * 0.5 { app.swipeUp() }
        return (app, toggle, app.descendants(matching: .any)["computer-status"].firstMatch)
    }

    /// Not installed (#1183): 安装 Cua Driver shows the node's steps, then the grant waits on the Mac's dialogs, then it
    /// can be operated. MOCK_COMPUTER=missing.
    @MainActor func testInstallThenGrant() throws {
        guard state == "missing" else { throw XCTSkip("MOCK_COMPUTER=missing") }
        let (app, _, status) = try open()
        let install = app.buttons["computer-install"].firstMatch
        XCTAssertTrue(install.waitForExistence(timeout: 10) && status.label.contains("还没有装"), "\(status.label)")
        shot(app, "computer-missing")
        install.tap()
        XCTAssertTrue(waitFor(status, label: "下载"), "the node's steps: \(status.label)")
        XCTAssertTrue(app.buttons["computer-cancel"].exists, "an install can be stopped")
        shot(app, "computer-installing")
        XCTAssertTrue(waitFor(status, label: "在它的屏幕上点「允许」", timeout: 30), "then the grant: \(status.label)")
        shot(app, "computer-granting")
        XCTAssertTrue(waitFor(status, label: "现在可以操控", timeout: 30), "allowed on the Mac: \(status.label)")
        shot(app, "computer-granted")
    }

    /// 看画面 (#1184): the hub's live screen page in a web view, which reaches the Mac's Screen Sharing through the port
    /// relay and asks for its sign-in. Needs the mock to serve the built page (MOCK_WEB=web/dist) and to relay to a real
    /// Screen Sharing on this Mac's 5900 (TEST_RUNNER_JARVIS_COMPUTER_SCREEN=1).
    @MainActor func testTheLiveScreenAsksForTheMacsSignIn() throws {
        guard state == "ready", ProcessInfo.processInfo.environment["JARVIS_COMPUTER_SCREEN"] == "1" else {
            throw XCTSkip("MOCK_COMPUTER=ready with MOCK_WEB and a Screen Sharing on 5900 (JARVIS_COMPUTER_SCREEN=1)")
        }
        let (app, _, _) = try open()
        let look = app.buttons["computer-screen"].firstMatch
        XCTAssertTrue(look.waitForExistence(timeout: 10), "no 看画面:\n\(app.debugDescription)")
        look.tap()
        let signIn = app.webViews.staticTexts["登录这台 Mac 的屏幕共享"].firstMatch
        XCTAssertTrue(signIn.waitForExistence(timeout: 30), "the Mac's Screen Sharing did not ask to sign in:\n\(app.debugDescription)")
        XCTAssertTrue(app.webViews.textFields["账号"].exists && app.webViews.secureTextFields["密码"].exists)
        shot(app, "computer-screen-signin")
        app.buttons["完成"].firstMatch.tap()
        XCTAssertTrue(look.waitForExistence(timeout: 10), "back on the machine page")
    }

    /// In the chat, under the assistant's 看电脑 / 操控电脑: 看画面 for the Mac it operated (MOCK_COMPUTER=ready has a
    /// conversation where it did 6 × 7 on MacBook).
    @MainActor func testTheChatOffersTheLiveScreen() throws {
        guard state == "ready", let hub = ProcessInfo.processInfo.environment["JARVIS_COMPUTER_HUB"] else {
            throw XCTSkip("MOCK_COMPUTER=ready (JARVIS_COMPUTER_HUB)")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-0000000000c1", "-openWork", "YES",
                               "-showToolDetails", "YES", "-showRunsOpen", "YES"] // BeautifulUI folds the two into a run of chips
        app.launchPinned()
        let look = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "看电脑")).firstMatch
        XCTAssertTrue(look.waitForExistence(timeout: 30), "no 看电脑 row:\n\(app.debugDescription)")
        let screen = app.buttons["tool-screen"].firstMatch
        XCTAssertTrue(screen.waitForExistence(timeout: 10), "no 看画面 under the tool:\n\(app.debugDescription)")
        for _ in 0..<4 where !screen.isHittable { screen.frame.minY < app.frame.midY ? app.swipeDown() : app.swipeUp() }
        shot(app, "computer-chat-tool")
    }

    @MainActor func testAnInstallCanBeCancelled() throws {
        guard state == "missing" else { throw XCTSkip("MOCK_COMPUTER=missing") }
        let (app, _, status) = try open()
        let install = app.buttons["computer-install"].firstMatch
        XCTAssertTrue(install.waitForExistence(timeout: 10))
        install.tap()
        let cancel = app.buttons["computer-cancel"].firstMatch
        XCTAssertTrue(cancel.waitForExistence(timeout: 10))
        cancel.tap()
        XCTAssertTrue(install.waitForExistence(timeout: 10) && waitFor(status, label: "还没有装"), "back to 安装: \(status.label)")
    }

    @MainActor func testTheSwitchAndTheState() throws {
        guard state != "missing" else { throw XCTSkip("MOCK_COMPUTER=missing is the install's") }
        let (app, toggle, status) = try open()
        if state == "off" {
            XCTAssertEqual(toggle.value as? String, "0")
            XCTAssertFalse(status.exists, "nothing to say until it is on")
            shot(app, "computer-off")
            return
        }
        XCTAssertEqual(toggle.value as? String, "1")
        let says = state == "ready" ? "现在可以操控 · Cua Driver 0.38.0" : state == "locked" ? "锁屏" : "屏幕录制权限"
        XCTAssertTrue(status.waitForExistence(timeout: 10) && status.label.contains(says), "\(status.label)")
        if state == "permissions" { XCTAssertTrue(app.buttons["computer-grant"].exists, "去授权 in place of a command to run") }
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

    private func waitFor(_ e: XCUIElement, label: String, timeout: TimeInterval = 10) -> Bool {
        let deadline = Date.now.addingTimeInterval(timeout)
        while Date.now < deadline { if e.exists, e.label.contains(label) { return true }; usleep(200_000) }
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
