import XCTest

/// An agent's dev server opened from the phone (docs/operations/ports.md), against scripts/mock/mock_hub.py with MOCK_PORTS=1
/// (PERSONA_MOCK_HUB): the session lists the port and its reply links it, both as `:port` badges with the page's title;
/// a tap opens the page in the in-app sheet, its bytes through the hub's …/ports/{port}/connect (GET /mock/ports
/// lists those); the badge of a server that has stopped is gray and says so when tapped.
final class PortForwardTests: XCTestCase {
    private var hub: String!
    private let landing = "a1f3c2d4-0000-4000-8000-00000000000d"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_PORTS=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let suffix = ProcessInfo.processInfo.environment["SHOT_SUFFIX"].map { "-" + $0 } ?? ""
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-ports-\(name)\(suffix).png"))
    }

    private func json(_ path: String) throws -> [String: Any] {
        let data = try Data(contentsOf: URL(string: hub + path)!)
        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }

    /// The dev server's port, as the mock's session lists it.
    private func devPort() throws -> Int {
        let sessions = try json("/api/v1/sessions")["sessions"] as? [[String: Any]] ?? []
        let s = try XCTUnwrap(sessions.first { $0["session_id"] as? String == landing })
        return try XCTUnwrap((s["ports"] as? [[String: Any]])?.first?["port"] as? Int)
    }

    @MainActor func testOpenTheAgentsDevServer() throws {
        let port = try devPort()
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        // A fresh chat cache: each mock run serves its page on another port, under the same message id.
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/" + landing, "-openChatsFresh", "YES"]
        app.launchPinned()

        let chips = app.buttons.matching(identifier: "port-chip-\(port)")
        XCTAssertTrue(chips.firstMatch.waitForExistence(timeout: 20), "no badge for :\(port):\n\(app.debugDescription)")
        XCTAssertTrue(chips.firstMatch.label.contains("天气 · 落地页"), "the badge has no title: \(chips.firstMatch.label)")
        let gone = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'port-chip-' AND label CONTAINS '已停止'")).firstMatch
        XCTAssertTrue(gone.waitForExistence(timeout: 10), "the stopped server's badge is not gray:\n\(app.debugDescription)")
        sleep(1)
        shot(app, "chat")

        // The reply's badge (the last one: the header's comes first) opens the page in the sheet.
        chips.element(boundBy: chips.count - 1).tap()
        let headline = app.webViews.staticTexts["明天有雨"]
        XCTAssertTrue(headline.waitForExistence(timeout: 20), "the page did not load through the hub:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["MacBook :\(port)"].exists, "the sheet does not name the machine and port:\n\(app.debugDescription)")
        sleep(1)
        shot(app, "page")
        let connects = try json("/mock/ports")["connects"] as? [[String: Any]] ?? []
        XCTAssertTrue(connects.contains { $0["port"] as? Int == port && $0["machine_id"] as? String == "macbook" },
                      "the page did not come through …/ports/\(port)/connect: \(connects)")
        // Refresh asks the page again; closed and opened again, it is a new load too, not the page as it was (#1934).
        let loads = { () -> Int in
            let t = app.webViews.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '第 '")).firstMatch
            return t.exists ? Int(t.label.split(separator: " ")[1]) ?? 0 : 0
        }
        let first = loads()
        XCTAssertGreaterThan(first, 0, "the page has no load count:\n\(app.debugDescription)")
        app.buttons["link-reload"].tap()
        expectation(for: NSPredicate { _, _ in loads() > first }, evaluatedWith: NSNull())
        waitForExpectations(timeout: 10)
        let afterReload = loads()
        app.buttons["关闭"].firstMatch.tap()
        sleep(1)
        chips.element(boundBy: chips.count - 1).tap()
        XCTAssertTrue(headline.waitForExistence(timeout: 20), "the page did not open again")
        expectation(for: NSPredicate { _, _ in loads() > afterReload }, evaluatedWith: NSNull())
        waitForExpectations(timeout: 10)
        shot(app, "reopened")
        app.buttons["关闭"].firstMatch.tap()

        // The stopped one says why it cannot open.
        XCTAssertTrue(gone.waitForExistence(timeout: 5))
        gone.tap()
        let toast = app.staticTexts.matching(NSPredicate(format: "label CONTAINS '已经没有在监听'")).firstMatch
        XCTAssertTrue(toast.waitForExistence(timeout: 10), "no word on the stopped server:\n\(app.debugDescription)")
        shot(app, "gone")
    }
}
