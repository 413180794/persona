import XCTest

/// The BeautifulUI style (设置 › 界面风格): its prompt bar sends what was typed (it once cleared the draft before the send
/// read it, and nothing went out), and the chat shows its components only there. Against scripts/mock_hub.py
/// (JARVIS_MOCK_HUB), which records sends at /mock/sent.
final class BeautifulStyleTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func launch(style: String) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-ui.style", style, "-tab", "dev",
                               "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006"]
        app.launch()
        return app
    }

    private func sent() -> [String] {
        struct Sent: Decodable { struct Body: Decodable { var text: String? }; var sent: [Body] }
        guard let data = try? Data(contentsOf: URL(string: hub + "/mock/sent")!),
              let s = try? JSONDecoder().decode(Sent.self, from: data) else { return [] }
        return s.sent.compactMap(\.text)
    }

    @MainActor private func sendWorks(_ app: XCUIApplication, _ words: String) {
        let field = app.textViews.firstMatch.exists ? app.textViews.firstMatch : app.textFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 20), "no composer:\n\(app.debugDescription)")
        field.tap()
        field.typeText(words)
        app.buttons["发送"].firstMatch.tap()
        let deadline = Date.now.addingTimeInterval(8)
        while !sent().contains(words), Date.now < deadline { usleep(250_000) }
        XCTAssertTrue(sent().contains(words), "「\(words)」 did not reach the hub; sent: \(sent())")
    }

    @MainActor func testPromptBarSends() {
        let app = launch(style: "beautiful")
        // The prompt bar's toolbar: @ is there only in this style.
        XCTAssertTrue(app.buttons["提及文件或会话"].waitForExistence(timeout: 20), "not the prompt bar:\n\(app.debugDescription)")
        sendWorks(app, "BeautifulUI 发送 \(Int.random(in: 1000...9999))")
    }

    @MainActor func testDefaultStyleKeepsTheOriginalComposerAndSends() {
        let app = launch(style: "glass")
        let send = app.buttons["发送"].firstMatch
        XCTAssertTrue(send.waitForExistence(timeout: 20))
        XCTAssertFalse(app.buttons["提及文件或会话"].exists, "the prompt bar showed in 液态玻璃")
        sendWorks(app, "液态玻璃 发送 \(Int.random(in: 1000...9999))")
    }
}
