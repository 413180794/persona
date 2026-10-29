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
                               "-terminalInputMode", "direct", "-terminalTipsSeen", "YES", "-terminalSize", "mirror"]
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

    /// Driving the terminal (MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1): the phone's grid goes up with the
    /// subscribe, keys go out as the terminal's bytes over the WebSocket, and the keyboard coming up gives the
    /// terminal fewer rows.
    @MainActor func testDrivingTheTerminalSendsItsBytes() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/" + widget, "-openPane", "terminal",
                               "-terminalInputMode", "direct", "-terminalTipsSeen", "YES", "-terminalSize", "phone"]
        app.launch()

        func input() -> [[String: Any]] {
            guard let data = try? Data(contentsOf: URL(string: hub + "/mock/terminal_input")!),
                  let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [] }
            return obj["input"] as? [[String: Any]] ?? []
        }
        let size = app.descendants(matching: .any)["terminal-size"]
        guard size.waitForExistence(timeout: 15), (size.label.contains("手机尺寸") || {
            sleep(2); return size.label.contains("手机尺寸")
        }()) else { throw XCTSkip("the mock does not drive terminals (MOCK_TERMINAL_CONTROL=1)") }
        let rowsBefore = Int(size.label.split(separator: "×").last ?? "") ?? 0
        let before = input().count
        let screen = app.descendants(matching: .any)["terminal-screen"]
        screen.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no keyboard on the terminal")
        sleep(1)
        app.typeText("ls\n")
        app.buttons["Control"].tap()
        app.typeText("c")

        var got: [[String: Any]] = []
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline {
            got = Array(input().dropFirst(before))
            if got.contains(where: { ($0["text"] as? String)?.contains("\u{03}") == true }) { break }
            usleep(300_000)
        }
        XCTAssertEqual(got.compactMap { $0["text"] as? String }.joined(), "ls\r\u{03}", "input: \(got)")
        let resized = got.compactMap { ($0["control"] as? [String: Int])?["rows"] }
        XCTAssertFalse(resized.isEmpty, "no new size with the keyboard up: \(got)")
        // (typeText may switch the simulator to its hardware keyboard afterwards: rows come back, more with the
        // page's header folded)
        XCTAssertLessThan(resized.min() ?? .max, rowsBefore, "the keyboard did not take rows: \(resized)")
        XCTAssertTrue(size.label.hasSuffix("×\(resized.last ?? 0)"), size.label)

        // Leaving the tab lets the terminal go (the pane goes back to the computer's size).
        app.buttons["收起键盘"].tap() // the page's header (with the tabs) is folded while the keyboard is up
        XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 5))
        sleep(1)
        app.buttons["对话"].firstMatch.tap()
        let gone = Date.now.addingTimeInterval(5)
        while Date.now < gone, !input().dropFirst(before).contains(where: { $0["type"] as? String == "terminal_unsubscribe" }) {
            usleep(200_000)
        }
        XCTAssertTrue(input().dropFirst(before).contains { $0["type"] as? String == "terminal_unsubscribe" }, "still driving after leaving")
    }

    /// The terminal tab opened once the app is connected (as on a phone: the session page first, then 终端):
    /// the view measures the phone's grid before the first frame, so the takeover starts (build 21 spun forever:
    /// the spinner stood in for the view that tells the grid the subscribe waits for).
    @MainActor func testOpeningTheTerminalWhenConnectedDrivesIt() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/" + widget,
                               "-terminalInputMode", "direct", "-terminalTipsSeen", "YES", "-terminalSize", "phone"]
        app.launch()
        let tab = app.buttons["终端"].firstMatch
        XCTAssertTrue(tab.waitForExistence(timeout: 15), "no session page:\n\(app.debugDescription)")
        sleep(3) // connected by now
        let unfold = app.buttons.matching(NSPredicate(format: "label ENDSWITH %@", "显示标题和标签")).firstMatch
        for _ in 0..<5 where !tab.isSelected { // the header may have folded away: scrolling the chat brings it back
            if tab.isHittable, tab.frame.minY > 60 { tab.tap() } else if unfold.exists, unfold.isHittable { unfold.tap() } else {
                app.swipeDown()
            }
            sleep(1)
        }
        XCTAssertTrue(tab.isSelected, "the terminal tab did not open")
        let size = app.descendants(matching: .any)["terminal-size"]
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline, !(size.exists && size.label.contains("手机尺寸")) { usleep(250_000) }
        XCTAssertTrue(size.exists && size.label.contains("手机尺寸"),
                      "the terminal did not start: \(size.exists ? size.label : "no size chip")\n\(app.debugDescription)")
    }
}
