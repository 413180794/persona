import XCTest

/// VNC remembers the Mac's Screen Sharing sign-in on this phone (#1322), against scripts/mock/mock_hub.py with
/// MOCK_COMPUTER=ready MOCK_WEB=web/dist MOCK_SCREEN=fake (a Screen Sharing of the mock's own that takes me / mock-pass;
/// TEST_RUNNER_JARVIS_SCREEN_HUB, TEST_RUNNER_JARVIS_FAKE_SCREEN=1): signed in once with 「在这台手机上记住」, the
/// screen comes up by itself next time; after the Mac's password changed the form says so and the new one replaces
/// it; 忘记屏幕共享密码 asks again. JARVIS_SHOTS keeps the screenshots.
final class ScreenPasswordTests: XCTestCase {
    private var hub: String? { ProcessInfo.processInfo.environment["JARVIS_SCREEN_HUB"] }

    @MainActor func testTheSignInIsRemembered() throws {
        guard let hub, ProcessInfo.processInfo.environment["JARVIS_FAKE_SCREEN"] == "1" else {
            throw XCTSkip("needs the mock's own Screen Sharing: MOCK_SCREEN=fake (JARVIS_FAKE_SCREEN=1)")
        }
        continueAfterFailure = false
        try mock(hub, "/mock/screen-password?p=mock-pass")
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=" + UITestToken.device
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "macbook", "-openScreenFresh", "YES"]
        app.launchPinned()
        let look = app.buttons["computer-screen"].firstMatch
        XCTAssertTrue(look.waitForExistence(timeout: 30), "no VNC:\n\(app.debugDescription)")
        for _ in 0..<6 where !look.isHittable { app.swipeUp() }
        XCTAssertFalse(app.buttons["screen-forget"].exists, "nothing remembered yet")

        // The first time: the form, 记住 ticked.
        look.tap()
        let signIn = app.webViews.staticTexts["登录这台 Mac 的屏幕共享"].firstMatch
        XCTAssertTrue(signIn.waitForExistence(timeout: 30), "the form:\n\(app.debugDescription)")
        let remember = app.webViews.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "在这台手机上记住")).firstMatch
        XCTAssertTrue(remember.exists, "记住:\n\(app.debugDescription)")
        shot(app, "screen-password-form")
        type("mock-pass", in: app)
        XCTAssertTrue(live(app), "signed in")
        close(app)
        let forget = app.buttons["screen-forget"].firstMatch
        XCTAssertTrue(forget.waitForExistence(timeout: 10), "remembered: 忘记屏幕共享密码 next to VNC")
        shot(app, "screen-password-forget")

        // Next time it signs in by itself.
        look.tap()
        XCTAssertTrue(live(app), "the screen with no form")
        XCTAssertFalse(signIn.exists)
        close(app)

        // The Mac's password changed: the form says the kept one no longer works; the new one replaces it.
        try mock(hub, "/mock/screen-password?p=new-pass")
        look.tap()
        let stale = app.webViews.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "记住的密码不对了")).firstMatch
        XCTAssertTrue(stale.waitForExistence(timeout: 30), "the kept password refused:\n\(app.debugDescription)")
        shot(app, "screen-password-stale")
        type("new-pass", in: app)
        XCTAssertTrue(live(app), "the new password")
        close(app)
        look.tap()
        XCTAssertTrue(live(app), "the new one is the kept one")
        close(app)

        // Forgotten: the form again.
        XCTAssertTrue(forget.waitForExistence(timeout: 10))
        forget.tap()
        XCTAssertTrue(waitGone(forget))
        look.tap()
        XCTAssertTrue(signIn.waitForExistence(timeout: 30), "asked again")
        close(app)
    }

    /// 一键解锁 and 锁上 (#1507 a-2): signed in once with 记住, 一键解锁 turned on (confirmed); 锁上 locks the Mac, and
    /// 一键解锁 types the kept password on its lock screen: the mock's Screen Sharing gets it and Return, and unlocks.
    /// Face ID is skipped on the simulator (-showUnlockWithoutAuth).
    @MainActor func testOneTapUnlock() throws {
        guard let hub, ProcessInfo.processInfo.environment["JARVIS_FAKE_SCREEN"] == "1" else {
            throw XCTSkip("needs the mock's own Screen Sharing: MOCK_SCREEN=fake (JARVIS_FAKE_SCREEN=1)")
        }
        continueAfterFailure = false
        try mock(hub, "/mock/screen-password?p=mock-pass&clear=1")
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=" + UITestToken.device
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "macbook",
                               "-openScreenFresh", "YES", "-showUnlockWithoutAuth", "YES"]
        app.launchPinned()
        let look = app.buttons["computer-screen"].firstMatch
        XCTAssertTrue(look.waitForExistence(timeout: 30), "no VNC:\n\(app.debugDescription)")
        for _ in 0..<6 where !look.isHittable { app.swipeUp() }
        look.tap()
        XCTAssertTrue(app.webViews.staticTexts["登录这台 Mac 的屏幕共享"].firstMatch.waitForExistence(timeout: 30))
        type("mock-pass", in: app)
        XCTAssertTrue(live(app), "signed in, kept")
        close(app)

        let oneTap = app.switches["computer-one-tap"].firstMatch
        XCTAssertTrue(oneTap.waitForExistence(timeout: 10), "一键解锁, once the sign-in is kept:\n\(app.debugDescription)")
        for _ in 0..<4 where !oneTap.isHittable { app.swipeUp() }
        oneTap.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        let confirm = app.buttons["打开"].firstMatch
        XCTAssertTrue(confirm.waitForExistence(timeout: 5), "asked first:\n\(app.debugDescription)")
        confirm.tap()
        let lock = app.buttons["computer-lock"].firstMatch
        XCTAssertTrue(lock.waitForExistence(timeout: 10), "锁上 while it is on")
        shot(app, "screen-unlock-on")
        lock.tap()
        let unlock = app.buttons["computer-unlock-now"].firstMatch
        XCTAssertTrue(unlock.waitForExistence(timeout: 10), "locked: 一键解锁:\n\(app.debugDescription)")
        shot(app, "screen-unlock-locked")
        unlock.tap()
        XCTAssertTrue(live(app), "signed in by itself")
        let deadline = Date.now.addingTimeInterval(15)
        var typed = ""
        while Date.now < deadline, typed != "mock-pass\n" { typed = try self.typed(hub); usleep(300_000) }
        XCTAssertEqual(typed, "mock-pass\n", "the kept password and Return on the lock screen")
        close(app)
        XCTAssertTrue(lock.waitForExistence(timeout: 15), "unlocked: 锁上 again, not 一键解锁")
    }

    private func typed(_ hub: String) throws -> String {
        var out = ""
        let done = expectation(description: "typed")
        URLSession.shared.dataTask(with: URL(string: hub + "/mock/screen-password")!) { data, _, _ in
            out = (data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] })?["typed"] as? String ?? ""
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        return out
    }

    @MainActor private func type(_ password: String, in app: XCUIApplication) {
        let field = app.webViews.secureTextFields["密码"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText(password)
        app.webViews.buttons["连接"].firstMatch.tap()
    }

    /// The drawer says 已连接.
    @MainActor private func live(_ app: XCUIApplication) -> Bool {
        let phase = app.descendants(matching: .any)["screen-phase"].firstMatch
        let deadline = Date.now.addingTimeInterval(30)
        while Date.now < deadline { if phase.exists, phase.label.contains("已连接") { return true }; usleep(250_000) }
        return false
    }

    @MainActor private func close(_ app: XCUIApplication) {
        app.descendants(matching: .any)["screen-close"].firstMatch.tap()
        XCTAssertTrue(waitGone(app.descendants(matching: .any)["screen-drawer"].firstMatch), "the drawer closed")
    }

    private func waitGone(_ e: XCUIElement) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if !e.exists { return true }; usleep(200_000) }
        return false
    }

    private func mock(_ hub: String, _ path: String) throws {
        let done = expectation(description: path)
        URLSession.shared.dataTask(with: URL(string: hub + path)!) { _, _, _ in done.fulfill() }.resume()
        wait(for: [done], timeout: 10)
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
