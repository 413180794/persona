import XCTest

/// An extension that installs command-line programs and signs in (#4507): the install sheet lists the programs under
/// Install command-line programs, the steps have Install command-line tools with a line per program (the machine's own one says so), the install ends on
/// the extension's page with the sign-in open (a QR code and a link to open it on this phone), it signs in by itself
/// when the hub says done, Sign Out signs out, and About lists the programs. An older hub (no `extension_auth`) shows no
/// Sign In section, only its warning and the extension's own sign-in page.
///
/// Against scripts/mock/mock_hub.py, a fresh one per run:
/// `MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=1 MOCK_EXTENSION_AUTH=1` → `TEST_RUNNER_PERSONA_EXT_AUTH_HUB=http://127.0.0.1:<port>`;
/// `MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=1 MOCK_EXTENSION_AUTH=installed MOCK_EXTENSION_AUTH_OLD=1` →
/// `TEST_RUNNER_PERSONA_EXT_AUTH_OLD_HUB=…`. `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots, named by the style and
/// `TEST_RUNNER_PERSONA_SHOTS_APPEARANCE` (light / dark).
final class ExtensionSignInTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }

    @MainActor private func launch(_ key: String, _ extra: [String] = []) throws -> XCUIApplication {
        guard let hub = env[key] else { throw XCTSkip("needs mock_hub.py with MOCK_EXTENSION_AUTH (\(key))") }
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings"] + extra
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = env["PERSONA_SHOTS_APPEARANCE"] ?? "light"
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "ext-auth-\(name)-\(UITestStyle.name)-\(look).png"))
    }

    @MainActor private func any(_ app: XCUIApplication, _ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    @MainActor private func scrollTo(_ el: XCUIElement, in app: XCUIApplication, up: Bool = true) {
        for _ in 0..<8 where !(el.exists && el.isHittable) { up ? app.swipeUp() : app.swipeDown() }
    }

    @MainActor private func waitFor(_ el: XCUIElement, value want: String, timeout: TimeInterval) -> Bool {
        let end = Date.now.addingTimeInterval(timeout)
        while Date.now < end {
            if el.exists, (el.value as? String) == want { return true }
            usleep(150_000)
        }
        return false
    }

    @MainActor func testInstallToolsAndSignIn() throws {
        let app = try launch("PERSONA_EXT_AUTH_HUB")
        let entry = app.buttons["settings-plugins"]
        XCTAssertTrue(entry.waitForExistence(timeout: 15) || app.staticTexts["设置"].exists, "no settings:\n\(app.debugDescription)")
        scrollTo(entry, in: app)
        entry.tap()
        let market = app.buttons["plugins-market"]
        XCTAssertTrue(market.waitForExistence(timeout: 15), "no 扩展市场:\n\(app.debugDescription)")
        market.tap()
        let item = app.buttons["market-plugin-example-lookup"]
        XCTAssertTrue(item.waitForExistence(timeout: 15), "no catalog:\n\(app.debugDescription)")
        scrollTo(item, in: app)
        item.tap()
        let install = app.buttons["market-install"]
        XCTAssertTrue(install.waitForExistence(timeout: 10), "no 安装:\n\(app.debugDescription)")
        install.tap()

        // The sheet: the programs, where they come from and where they go.
        let confirm = app.buttons["market-confirm"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 30), "no preview:\n\(app.debugDescription)")
        let tools = any(app, "permission-tools")
        scrollTo(tools, in: app)
        XCTAssertTrue(tools.exists, "no 安装命令行程序:\n\(app.debugDescription)")
        XCTAssertTrue(tools.label.contains("安装命令行程序") && tools.label.contains("example-cli 2.3.4 · npm：registry.example.com")
                      && tools.label.contains("nodejs.org") && tools.label.contains("不用管理员权限"), "the line: \(tools.label)")
        shot(app, "sheet")
        scrollTo(confirm, in: app)
        confirm.tap()

        // The steps: Install command-line tools, a line per program.
        let step = any(app, "extension-operation-step-tools")
        XCTAssertTrue(step.waitForExistence(timeout: 10), "no 安装命令行 step:\n\(app.debugDescription)")
        let end = Date.now.addingTimeInterval(15)
        while Date.now < end, !(step.exists && step.label.contains("用这台机器上的")) { usleep(150_000) }
        XCTAssertTrue(step.label.contains("安装命令行") && step.label.contains("node") && step.label.contains("用这台机器上的")
                      && step.label.contains("example-cli"), "the step: \(step.label)")
        // The programs under the step (the page's foot): one swipe while it still installs; it may be done already.
        if step.exists { app.swipeUp() }
        shot(app, "tools")

        // Installed, not signed in: the extension's page with the sign-in open.
        // Its Sign In section is at the top (the page may have opened scrolled to where the steps were).
        let qr = any(app, "extension-auth-qr")
        let opened = Date.now.addingTimeInterval(30)
        while Date.now < opened, !qr.exists {
            if app.buttons["plugin-enabled"].exists || any(app, "plugin-tool-node").exists { app.swipeDown() }
            usleep(500_000)
        }
        XCTAssertTrue(qr.exists, "the sign-in did not open after the install:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["extension-auth-open"].exists, "no link to open on this phone")
        let status = any(app, "extension-auth-status")
        XCTAssertTrue(waitFor(status, value: "pending", timeout: 5) || (status.value as? String) == "signed_in", "not 等待确认")
        shot(app, "qr")
        XCTAssertFalse(app.buttons["plugin-page-login"].exists, "the extension's own sign-in page is for older apps")

        // The hub says done (the third poll): signed in, the card closes.
        XCTAssertTrue(waitFor(status, value: "signed_in", timeout: 15), "not signed in:\n\(app.debugDescription)")
        XCTAssertTrue(qr.waitForNonExistence(timeout: 5))
        XCTAssertTrue(status.label.contains("me@example.com"), "no account: \(status.label)")
        XCTAssertTrue(app.buttons["extension-auth-sign-in"].label.contains("重新登录"))
        shot(app, "signed-in")

        // About lists the programs.
        let about = any(app, "plugin-tool-node")
        scrollTo(about, in: app)
        XCTAssertTrue(about.exists && about.label.contains("机器上已有"), "no programs in 关于:\n\(app.debugDescription)")
        XCTAssertTrue(any(app, "plugin-tool-example-cli").exists)
        shot(app, "about")

        // Sign Out.
        let logout = app.buttons["extension-auth-logout"]
        scrollTo(logout, in: app, up: false)
        logout.tap()
        let yes = app.buttons["extension-auth-logout-confirm"].firstMatch
        XCTAssertTrue(yes.waitForExistence(timeout: 5))
        yes.tap()
        XCTAssertTrue(waitFor(status, value: "signed_out", timeout: 10), "still signed in")
        XCTAssertFalse(app.buttons["extension-auth-logout"].exists)
        shot(app, "signed-out")
    }

    @MainActor func testAnOlderHubKeepsTheExtensionsOwnPage() throws {
        let app = try launch("PERSONA_EXT_AUTH_OLD_HUB", ["-openExtension", "example-lookup"])
        let page = app.buttons["plugin-page-login"]
        XCTAssertTrue(page.waitForExistence(timeout: 20), "no sign-in page of its own:\n\(app.debugDescription)")
        XCTAssertFalse(any(app, "extension-auth-status").exists, "an older hub has no 登录 section")
        let warning = app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "还没登录")).firstMatch
        scrollTo(warning, in: app)
        XCTAssertTrue(warning.exists, "no warning:\n\(app.debugDescription)")
        shot(app, "old-hub")
    }
}
