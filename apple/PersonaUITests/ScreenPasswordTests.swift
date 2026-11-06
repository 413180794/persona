import XCTest

/// VNC remembers the Mac's Screen Sharing sign-in on this phone (#1322), against scripts/mock/mock_hub.py with
/// MOCK_COMPUTER=ready MOCK_WEB=web/dist MOCK_SCREEN=fake (a Screen Sharing of the mock's own that takes me / mock-pass;
/// TEST_RUNNER_PERSONA_SCREEN_HUB, TEST_RUNNER_PERSONA_FAKE_SCREEN=1): signed in once with 「在这台手机上记住」, the
/// screen comes up by itself next time; after the Mac's password changed the form says so and the new one replaces
/// it; Forget Screen Sharing Password asks again. PERSONA_SHOTS keeps the screenshots.
final class ScreenPasswordTests: XCTestCase {
    private var hub: String? { ProcessInfo.processInfo.environment["PERSONA_SCREEN_HUB"] }

    @MainActor func testTheSignInIsRemembered() throws {
        guard let hub, ProcessInfo.processInfo.environment["PERSONA_FAKE_SCREEN"] == "1" else {
            throw XCTSkip("needs the mock's own Screen Sharing: MOCK_SCREEN=fake (PERSONA_FAKE_SCREEN=1)")
        }
        continueAfterFailure = false
        try mock(hub, "/mock/screen-password?p=mock-pass")
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=" + UITestToken.device
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "macbook", "-openScreenFresh", "YES"]
        app.launchPinned()
        let look = app.buttons["computer-screen"].firstMatch
        XCTAssertTrue(look.waitForExistence(timeout: 30), "no VNC:\n\(app.debugDescription)")
        for _ in 0..<6 where !look.isHittable { app.swipeUp() }
        XCTAssertFalse(app.buttons["screen-forget"].exists, "nothing remembered yet")

        // The first time: the form, Remember ticked.
        look.tap()
        let signIn = app.staticTexts["登录这台 Mac 的屏幕共享"].firstMatch
        XCTAssertTrue(signIn.waitForExistence(timeout: 30), "the form:\n\(app.debugDescription)")
        let remember = app.switches["vnc-remember"].firstMatch
        XCTAssertTrue(remember.exists && remember.label.contains("在这台手机上记住") && remember.value as? String == "1", "记住, on:\n\(app.debugDescription)")
        shot(app, "screen-password-form")
        type("mock-pass", in: app)
        XCTAssertTrue(live(app), "signed in")
        close(app)
        let forget = app.buttons["screen-forget"].firstMatch
        XCTAssertTrue(forget.waitForExistence(timeout: 10), "remembered: 忘记屏幕共享密码 next to VNC")
        shot(app, "screen-password-forget")

        // Next time it signs in by itself, into the small window.
        letGo()
        look.tap()
        XCTAssertTrue(openedByItself(app), "the small window (or the page), signed in by itself:\n\(app.debugDescription)")
        XCTAssertFalse(signIn.exists)
        XCTAssertTrue(waitSignedIn(hub, count: 2), "it signed in with the kept one")

        // The Mac's password changed: the form says the kept one no longer works; the new one replaces it.
        try mock(hub, "/mock/screen-password?p=new-pass")
        letGo()
        look.tap()
        let stale = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "记住的密码不对了")).firstMatch
        XCTAssertTrue(stale.waitForExistence(timeout: 30), "the kept password refused:\n\(app.debugDescription)")
        shot(app, "screen-password-stale")
        type("new-pass", in: app)
        XCTAssertTrue(live(app), "the new password")
        close(app)
        letGo()
        look.tap()
        XCTAssertTrue(waitSignedIn(hub, count: 5), "the new one is the kept one")
        XCTAssertTrue(openedByItself(app))

        // Forgotten: the form again.
        XCTAssertTrue(forget.waitForExistence(timeout: 10))
        forget.tap()
        XCTAssertTrue(waitGone(forget))
        letGo()
        look.tap()
        XCTAssertTrue(signIn.waitForExistence(timeout: 30), "asked again")
        close(app)
    }

    /// One-Tap Unlock and Lock (#1507 a-2): signed in once with Remember, One-Tap Unlock turned on (confirmed); Lock locks the Mac, and
    /// One-Tap Unlock types the kept password on its lock screen: the mock's Screen Sharing gets it and Return, and unlocks.
    /// Face ID is skipped on the simulator (-showUnlockWithoutAuth).
    @MainActor func testOneTapUnlock() throws {
        guard let hub, ProcessInfo.processInfo.environment["PERSONA_FAKE_SCREEN"] == "1" else {
            throw XCTSkip("needs the mock's own Screen Sharing: MOCK_SCREEN=fake (PERSONA_FAKE_SCREEN=1)")
        }
        continueAfterFailure = false
        try mock(hub, "/mock/screen-password?p=mock-pass&clear=1")
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=" + UITestToken.device
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "macbook",
                               "-openScreenFresh", "YES", "-showUnlockWithoutAuth", "YES"]
        app.launchPinned()
        let look = app.buttons["computer-screen"].firstMatch
        XCTAssertTrue(look.waitForExistence(timeout: 30), "no VNC:\n\(app.debugDescription)")
        for _ in 0..<6 where !look.isHittable { app.swipeUp() }
        look.tap()
        XCTAssertTrue(app.staticTexts["登录这台 Mac 的屏幕共享"].firstMatch.waitForExistence(timeout: 30))
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

    /// The mock's Screen Sharing has taken count good sign-ins.
    private func waitSignedIn(_ hub: String, count: Int) -> Bool {
        let deadline = Date.now.addingTimeInterval(20)
        while Date.now < deadline {
            var ok = 0
            let done = expectation(description: "signins")
            URLSession.shared.dataTask(with: URL(string: hub + "/mock/screen-password")!) { data, _, _ in
                let s = (data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] })?["signins"] as? [[String: Any]] ?? []
                ok = s.filter { $0["ok"] as? Bool == true }.count
                done.fulfill()
            }.resume()
            wait(for: [done], timeout: 10)
            if ok >= count { return true }
            usleep(500_000)
        }
        return false
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
        let field = app.secureTextFields["vnc-password"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText(password)
        app.buttons["vnc-connect"].firstMatch.tap()
    }

    /// The drawer says Connected.
    @MainActor private func live(_ app: XCUIApplication) -> Bool {
        let phase = app.descendants(matching: .any)["screen-phase"].firstMatch
        let deadline = Date.now.addingTimeInterval(30)
        while Date.now < deadline { if phase.exists, phase.label.contains("已连接") { return true }; usleep(250_000) }
        return false
    }

    /// Signed in with the kept one: the small window, or where there is no picture in picture (the simulators) the
    /// whole page, live, then closed again.
    @MainActor private func openedByItself(_ app: XCUIApplication) -> Bool {
        let pip = app.descendants(matching: .any)["vnc-pip-on"].firstMatch
        let page = app.descendants(matching: .any)["screen-drawer"].firstMatch
        let deadline = Date.now.addingTimeInterval(20)
        while Date.now < deadline, !pip.exists, !page.exists { usleep(250_000) }
        if pip.exists { return true }
        guard page.exists, live(app) else { return false }
        close(app)
        return true
    }

    /// The connection closes a moment after the page and the small window let it go (VNC.release): the next VNC signs
    /// in afresh.
    private func letGo() { sleep(3) }

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
        let look = ProcessInfo.processInfo.environment["PERSONA_LOOK"].map { "-" + $0 } ?? ""
        let full = "\(name)-\(UITestStyle.name)\(look)"
        let png = app.screenshot()
        let attachment = XCTAttachment(screenshot: png)
        attachment.name = full
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(full + ".png"))
        }
    }
}
