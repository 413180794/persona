import XCTest

/// Live-view data wiring before the unified component renderer is connected. A local mock serves the shared progress
/// snapshots; the real app replaces its fallback in place and ignores a stale WebSocket update.
/// TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<port> xcodebuild test … -only-testing:PersonaUITests/AssistantLiveViewTests
final class AssistantLiveViewTests: XCTestCase {
    @MainActor private var app: XCUIApplication!

    override func setUpWithError() throws { continueAfterFailure = false }

    private func mock() throws -> URL {
        guard let raw = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"],
              let url = URL(string: raw), url.scheme == "http", url.host == "127.0.0.1" else {
            throw XCTSkip("needs the dedicated loopback assistant_views_hub.py fixture")
        }
        return url
    }

    private func post(_ path: String, at hub: URL) async throws {
        var request = URLRequest(url: hub.appendingPathComponent(path))
        request.httpMethod = "POST"
        let (_, response) = try await URLSession.shared.data(for: request)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
    }

    private func waitForSocket(at hub: URL) async throws {
        for _ in 0..<50 {
            let (data, _) = try await URLSession.shared.data(from: hub.appendingPathComponent("mock/views/status"))
            if let response = try JSONSerialization.jsonObject(with: data) as? [String: Int], (response["connected"] ?? 0) > 0 { return }
            try await Task.sleep(for: .milliseconds(200))
        }
        throw NSError(domain: "AssistantLiveViewTests", code: 1, userInfo: [NSLocalizedDescriptionKey: "the local app never completed its WebSocket handshake"])
    }

    @MainActor private func shot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = "assistant live view \(name) \(UITestStyle.name)"
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor func testUpdateStaysAtItsFeedPositionAndAnOldVersionIsIgnored() async throws {
        let hub = try mock()
        try await post("mock/views/reset", at: hub)
        app = XCUIApplication()
        let paired = "persona://pair?url=" + hub.absoluteString.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", paired, "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001", "-app.tab", "assistant"]
        app.launchPinned()
        defer { app.terminate() }
        // Unit-test hosts can leave the simulator's notification alert up. While it is shown the app stays inactive.
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let allow = springboard.buttons.matching(NSPredicate(format: "label IN %@", ["Allow", "允许"])).firstMatch
        if allow.waitForExistence(timeout: 3) { allow.tap() }
        let fallback = app.staticTexts["assistant-live-view-fallback"]
        XCTAssertTrue(fallback.waitForExistence(timeout: 30), "the live card fallback was not shown")
        XCTAssertTrue(fallback.label.contains("发布进行中"))
        try await waitForSocket(at: hub)
        shot("initial")
        try await post("mock/views/update", at: hub)
        let completed = app.staticTexts.matching(NSPredicate(format: "identifier == %@ AND label CONTAINS %@", "assistant-live-view-fallback", "发布完成")).firstMatch
        XCTAssertTrue(completed.waitForExistence(timeout: 10), "the live snapshot did not update")
        XCTAssertEqual(app.staticTexts.matching(identifier: "assistant-live-view-fallback").count, 1, "an update inserted another feed line")
        shot("updated")
        try await post("mock/views/stale", at: hub)
        // Give the old update a chance to arrive; the latest version must remain visible.
        try await Task.sleep(for: .seconds(1))
        XCTAssertTrue(completed.exists)
        XCTAssertTrue(fallback.label.contains("发布完成"))
    }
}
