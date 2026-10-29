import XCTest

/// Typing straight into the terminal against scripts/mock_hub.py (TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<port>):
/// a tap brings the keyboard up on the terminal, keys go out in order (batched), the key bar's modifiers apply to them.
final class TerminalTypingTests: XCTestCase {
    private let widget = "a1f3c2d4-0000-4000-8000-000000000001"

    @MainActor func testKeyboardTypesIntoTheTerminal() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/" + widget, "-openPane", "terminal",
                               "-terminalInputMode", "direct", "-terminalTipsSeen", "YES"]
        app.launch()

        func sent() -> [(keys: [String], text: String)] {
            guard let data = try? Data(contentsOf: URL(string: hub + "/mock/keys")!),
                  let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let list = obj["keys"] as? [[String: Any]] else { return [] }
            return list.map { ($0["keys"] as? [String] ?? [], $0["text"] as? String ?? "") }
        }
        let before = sent().count // the mock keeps what earlier runs sent
        let screen = app.descendants(matching: .any)["terminal-screen"]
        XCTAssertTrue(screen.waitForExistence(timeout: 15), "no terminal:\n\(app.debugDescription)")
        sleep(1)
        screen.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no keyboard on the terminal")
        XCTAssertTrue(app.buttons["收起键盘"].exists)

        app.typeText("ls -la\n")
        // ctrl armed on the key bar, then c on the keyboard: ctrl+c.
        app.buttons["Control"].tap()
        app.typeText("c")

        var got: [(keys: [String], text: String)] = []
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline {
            got = Array(sent().dropFirst(before))
            if got.flatMap(\.keys).contains("ctrl+c") { break }
            usleep(300_000)
        }
        // However the keys were batched, they arrive in the order typed.
        var stream: [String] = []
        for r in got { stream.append(contentsOf: r.text.map { String($0) } + r.keys) }
        XCTAssertEqual(stream, "ls -la".map { String($0) } + ["enter", "ctrl+c"], "requests: \(got)")
        XCTAssertLessThan(got.count, 9, "one request per key: \(got)")

        app.buttons["收起键盘"].tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 5), "the keyboard stayed up")
    }
}
