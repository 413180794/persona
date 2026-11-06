import XCTest

/// Settings › Extensions › GitHub (#3228): the connector it contributes gets a section with the connector's own Show in Inbox switch (the
/// same as on Settings › Connectors › GitHub) and a row to that page; the extension's inbox rules stay in its schema form.
/// Against scripts/mock/mock_hub.py with MOCK_EXTENSIONS=1 MOCK_GITHUB_EXTENSION=1 (`TEST_RUNNER_PERSONA_EXTENSIONS_HUB`);
/// with MOCK_GITHUB_EXTENSION=old (a hub from before the switch) the section is not there.
/// `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots, named by `TEST_RUNNER_PERSONA_EXTENSIONS_APPEARANCE`.
final class ExtensionConnectorInboxTests: XCTestCase {
    @MainActor func testTheConnectorsInboxSwitch() async throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["PERSONA_EXTENSIONS_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 MOCK_GITHUB_EXTENSION=1 (PERSONA_EXTENSIONS_HUB)")
        }
        continueAfterFailure = false
        let look = env["PERSONA_EXTENSIONS_APPEARANCE"] ?? "light"
        let hasSwitch = try await inbox(hub) != nil
        if hasSwitch { try await setInbox(hub, true) } // every run starts on, whatever the last one left

        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openPlugin", "github"]
        app.launchPinned()
        func shot(_ name: String) {
            guard let dir = env["PERSONA_SHOTS"] else { return }
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "extension-connector-\(name)-\(look).png"))
        }

        let toggle = app.switches["extension-connector-inbox-github"]
        let open = app.buttons["extension-connector-open-github"]
        guard hasSwitch else {
            // A hub from before the switch: the page is there, the section is not, and nothing says it went wrong.
            XCTAssertTrue(app.switches["plugin-enabled"].waitForExistence(timeout: 30), "no extension page:\n\(app.debugDescription)")
            sleep(2) // the connector list has been read by now
            XCTAssertFalse(toggle.exists, "a switch the hub does not have")
            XCTAssertFalse(open.exists)
            shot("old-hub")
            return
        }

        XCTAssertTrue(toggle.waitForExistence(timeout: 30), "no 进收件箱 switch:\n\(app.debugDescription)")
        XCTAssertEqual(toggle.value as? String, "1")
        XCTAssertTrue(open.exists, "no 连接器设置 row")
        XCTAssertTrue(app.staticTexts["登录、状态和更多收件箱设置"].exists)
        XCTAssertTrue(text(app, "和连接器页上的「进收件箱」是同一个开关").exists, "the footer for on:\n\(app.debugDescription)")
        shot("on")

        // Off: the hub's own switch, the footer says what that means.
        toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitUntil { toggle.value as? String == "0" }, "the switch did not turn off")
        let off = try await inboxEventually(hub, false)
        XCTAssertEqual(off, false, "the hub's connector was not switched off")
        XCTAssertTrue(text(app, "不进收件箱：不再收新的，已有的条目留着。").waitForExistence(timeout: 5))
        shot("off")

        // The extension's own inbox rules: its schema form, as before.
        let rules = app.staticTexts["不收这些仓库"]
        for _ in 0..<8 where !(rules.exists && rules.isHittable) { app.swipeUp() }
        XCTAssertTrue(rules.exists, "no inbox rules in the form:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["只收这些仓库"].exists && app.staticTexts["不收这些类型"].exists)
        XCTAssertTrue(app.staticTexts["example-org/noisy-repo"].exists, "the configured repository")
        shot("rules")

        // Connector Settings opens the connector's page, whose Show in Inbox is the same switch.
        for _ in 0..<8 where !(open.exists && open.isHittable) { app.swipeDown() }
        open.tap()
        let there = app.switches["connector.inbox"]
        for _ in 0..<6 where !there.waitForExistence(timeout: 3) { app.swipeUp() }
        XCTAssertTrue(there.exists, "the connector's page has no 进收件箱:\n\(app.debugDescription)")
        XCTAssertEqual(there.value as? String, "0", "the connector's page does not show the same switch")
        shot("connector")
        there.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitUntil { there.value as? String == "1" }, "the connector page's switch did not turn on")

        // Back on the extension's page: read again, on.
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(toggle.waitForExistence(timeout: 10))
        XCTAssertTrue(waitUntil { toggle.value as? String == "1" }, "the extension's page did not follow the connector's")
    }

    private struct List: Decodable {
        struct Connector: Decodable { var id: String; var inbox: Bool? }
        var connectors: [Connector]
    }

    private func inbox(_ hub: String) async throws -> Bool? {
        let (data, _) = try await URLSession.shared.data(from: URL(string: hub + "/api/v1/connectors")!)
        return try JSONDecoder().decode(List.self, from: data).connectors.first { $0.id == "github" }?.inbox
    }

    private func inboxEventually(_ hub: String, _ want: Bool) async throws -> Bool? {
        var got = try await inbox(hub)
        for _ in 0..<20 where got != want {
            try await Task.sleep(for: .milliseconds(250))
            got = try await inbox(hub)
        }
        return got
    }

    private func setInbox(_ hub: String, _ on: Bool) async throws {
        var req = URLRequest(url: URL(string: hub + "/api/v1/connectors/github/settings")!)
        req.httpMethod = "PUT"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["inbox": on])
        _ = try await URLSession.shared.data(for: req)
    }

    private func text(_ app: XCUIApplication, _ part: String) -> XCUIElement {
        app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", part)).firstMatch
    }

    private func waitUntil(_ ok: () -> Bool) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if ok() { return true }; usleep(200_000) }
        return false
    }
}
