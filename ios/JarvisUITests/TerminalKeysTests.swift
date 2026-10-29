import XCTest

/// The terminal's key bar against scripts/mock_hub.py (TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<port>): what each
/// way of pressing ctrl / alt / shift sends, read back from GET /mock/keys.
final class TerminalKeysTests: XCTestCase {
    private let widget = "a1f3c2d4-0000-4000-8000-000000000001"

    @MainActor func testModifiersReachTheTerminal() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/" + widget, "-openPane", "terminal"]
        app.launch()

        func sent() -> [[String: String]] {
            guard let data = try? Data(contentsOf: URL(string: hub + "/mock/keys")!),
                  let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let list = obj["keys"] as? [[String: Any]] else { return [] }
            return list.map { ["keys": ($0["keys"] as? [String] ?? []).joined(separator: " "), "text": $0["text"] as? String ?? ""] }
        }
        let before = sent().count // the mock keeps what earlier runs sent
        let ctrl = app.buttons["Control"], alt = app.buttons["Option"], shift = app.buttons["Shift"]
        XCTAssertTrue(ctrl.waitForExistence(timeout: 15), "no key bar:\n\(app.debugDescription)")
        var expected: [[String: String]] = []
        func expect(_ keys: String, text: String = "") { expected.append(["keys": keys, "text": text]) }

        // A tap arms ctrl for the next character typed in the field.
        ctrl.tap()
        let field = app.textFields.firstMatch
        field.tap()
        field.typeText("c")
        expect("ctrl+c")
        // ⌃C goes out on its own.
        app.buttons["发送 Control C"].tap()
        expect("ctrl+c")
        // A double tap locks alt: it stays for every key until tapped again.
        alt.doubleTap()
        app.buttons["tab"].tap(); expect("alt+tab")
        app.buttons["tab"].tap(); expect("alt+tab")
        alt.tap()
        app.buttons["tab"].tap(); expect("tab")
        // Held and let go without pressing anything: armed for the next key.
        ctrl.press(forDuration: 0.8)
        app.buttons["tab"].tap(); expect("ctrl+tab")
        // The return after typed text takes shift.
        field.tap()
        field.typeText("ls")
        shift.tap()
        app.buttons["发送并回车"].tap(); expect("shift+enter", text: "ls")

        let deadline = Date.now.addingTimeInterval(10)
        var got: [[String: String]] = []
        while Date.now < deadline {
            got = Array(sent().dropFirst(before))
            if got.count >= expected.count { break }
            usleep(300_000)
        }
        XCTAssertEqual(got, expected)
    }

    /// Long press selects the word under the finger and offers 拷贝.
    @MainActor func testLongPressCopies() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/" + widget, "-openPane", "terminal"]
        app.launch()
        let screen = app.descendants(matching: .any)["terminal-screen"]
        XCTAssertTrue(screen.waitForExistence(timeout: 15), "no terminal:\n\(app.debugDescription)")
        sleep(2)
        // The mock's first row is "$ swift test --filter NetworkTests": press on "swift".
        screen.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 40, dy: 14)).press(forDuration: 0.8)
        let copy = app.menuItems["拷贝"].exists ? app.menuItems["拷贝"] : app.buttons["拷贝"]
        XCTAssertTrue(copy.waitForExistence(timeout: 3), "no edit menu:\n\(app.debugDescription)")
        copy.tap()
        XCTAssertTrue(app.staticTexts["已拷贝"].waitForExistence(timeout: 3), "no copy toast") // the runner may not read the pasteboard
    }
}
