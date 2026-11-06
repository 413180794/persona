import XCTest

final class GitPullUITests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_GIT_HUB"] else {
            throw XCTSkip("需要 MOCK_FILES=1 MOCK_GIT_PULL=1 的 mock hub 和 PERSONA_GIT_HUB")
        }
        self.hub = hub
    }

    private func control(_ mode: String? = nil) async throws -> [String: Any] {
        let url = URL(string: hub + "/mock/git-pull" + (mode.map { "?mode=" + $0 } ?? ""))!
        let (data, _) = try await URLSession.shared.data(from: url)
        return try JSONSerialization.jsonObject(with: data) as! [String: Any]
    }

    @MainActor private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000001",
                               "-openPane", "git", "-openAgentsFresh", "YES", "-openChatsFresh", "YES"]
        app.launchPinned()
        XCTAssertTrue(app.buttons["git-pull"].waitForExistence(timeout: 20), app.debugDescription)
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let style = env["PERSONA_UI_STYLE"] ?? "liquid"
        let appearance = env["PERSONA_GIT_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir + "/git-\(style)-\(appearance)-\(name).png"))
    }

    @MainActor func testPullRefreshesAndCanCheckAgain() async throws {
        _ = try await control("success")
        let app = launch()
        let button = app.buttons["git-pull"]
        XCTAssertEqual(button.value as? String, "落后 28 个提交")
        try shot(app, "before")
        button.tap()
        XCTAssertTrue(app.staticTexts["已拉取最新代码"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.staticTexts["更新示例代码"].exists)
        XCTAssertEqual(button.value as? String, "落后 0 个提交")
        let state = try await control()
        XCTAssertEqual(state["pulls"] as? Int, 1)
        try shot(app, "success")
        button.tap()
        XCTAssertTrue(app.staticTexts["已经是最新代码"].waitForExistence(timeout: 10))
    }

    @MainActor func testFailureIsVisibleAndRetryable() async throws {
        _ = try await control("error")
        let app = launch()
        app.buttons["git-pull"].tap()
        let error = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "工作区有未提交的改动")).firstMatch
        XCTAssertTrue(error.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertEqual(app.buttons["git-pull"].value as? String, "落后 28 个提交")
        try shot(app, "failure")
        _ = try await control("success")
        app.buttons["git-pull"].tap()
        XCTAssertTrue(app.staticTexts["已拉取最新代码"].waitForExistence(timeout: 10))
    }
}
