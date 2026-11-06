import XCTest

/// The first message of a new conversation (#1911): it shows where it stays, at the newest end over the composer, from
/// the moment it is sent; it did show at the top of the new-conversation page first, then jumped to the bottom when
/// the chat took over. Prints `FIRST <case> <s> <y>` for each change of the bubble's top on screen (s from the send),
/// and `FIRST <case> start|reply <epoch>` for cutting a recording to it.
///   PORT=… MOCK_ASSISTANT_START_SECONDS=2 MOCK_ECHO_SENT=1 python3 scripts/mock/mock_hub.py  (PERSONA_MOCK_HUB)
final class FirstMessageTests: XCTestCase {
    private var app: XCUIApplication!
    private var hub: String!
    private static let text = "依次运行三条命令，每条只告诉我退出码和列出了几条，不要贴任何内容。"

    override func setUpWithError() throws {
        continueAfterFailure = true
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs PERSONA_MOCK_HUB") }
        hub = h
    }

    private func launch(_ extra: [String]) {
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openChatsFresh", "YES"] + extra
        app.launchPinned()
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons["Allow"]
        if allow.waitForExistence(timeout: 6) { allow.tap() }
    }

    private func mark(_ name: String, _ what: String) {
        print(String(format: "FIRST %@ %@ %.3f", name, what, Date.now.timeIntervalSince1970))
    }

    /// The bubble's place every 50 ms for `seconds`, each change printed; how far it moved in all, and in one step. Its
    /// place is its bottom's distance over the composer: the keyboard coming up takes both up together (the simulator
    /// shows it only after the send, a phone has it up already), and that is not the message moving.
    @discardableResult
    private func follow(_ name: String, seconds: Double) -> (first: Double?, moved: Double, step: Double, gone: Int) {
        let bubble = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "依次运行三条命令")).firstMatch
        let field = app.textViews.firstMatch.exists ? app.textViews.firstMatch : app.textFields.firstMatch
        let start = Date.now
        var first: Double?, last: Double?, step = 0.0, lo = Double.infinity, hi = -Double.infinity, gone = 0
        while Date.now.timeIntervalSince(start) < seconds {
            if bubble.exists, field.exists {
                let y = Double(field.frame.minY - bubble.frame.maxY)
                // Not laid out (the row between two copies of it): counted as a flicker.
                guard y.isFinite else {
                    if first != nil { gone += 1 }
                    print(String(format: "FIRST %@ %.2f gone", name, Date.now.timeIntervalSince(start)))
                    usleep(50_000)
                    continue
                }
                if first == nil { first = y }
                if y != last {
                    print(String(format: "FIRST %@ %.2f %.0f", name, Date.now.timeIntervalSince(start), y))
                    if let l = last { step = max(step, abs(y - l)) }
                    last = y
                }
                lo = min(lo, y); hi = max(hi, y)
            }
            usleep(50_000)
        }
        return (first, hi > lo ? hi - lo : 0, step, gone)
    }

    private func reply() {
        var c = URLComponents(string: hub + "/mock/say")!
        c.queryItems = [URLQueryItem(name: "preset", value: "markdown"), URLQueryItem(name: "stream", value: "1"),
                        URLQueryItem(name: "steps", value: "40"), URLQueryItem(name: "ms", value: "80")]
        _ = try? Data(contentsOf: c.url!)
    }

    /// Assistant: the new-conversation page, the first message sent, the chat taking over, a reply streaming in.
    @MainActor func testAssistantFirstMessage() throws {
        launch(["-newAssistant", "YES", "-app.tab", "assistant", "-tab", "assistant"])
        let field = app.textViews.firstMatch.exists ? app.textViews.firstMatch : app.textFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 20), "no composer")
        sleep(2)
        field.tap()
        field.typeText(Self.text)
        app.buttons["发送"].firstMatch.tap()
        mark("assistant", "start")
        let sent = follow("assistant", seconds: 6)
        print("FIRST assistant sent: moved \(Int(sent.moved)) step \(Int(sent.step))")
        // The new-conversation page's field and the chat's are not the same height: a few points between the two.
        XCTAssertLessThanOrEqual(sent.moved, 10, "the first message moved after it was sent")
        XCTAssertEqual(sent.gone, 0, "the first message went off the screen")
        mark("assistant", "reply")
        DispatchQueue.global().async { self.reply() }
        let replied = follow("assistant-reply", seconds: 6)
        print("FIRST assistant reply: moved \(Int(replied.moved)) step \(Int(replied.step))")
    }

    /// Agents: a new agent with nothing in it yet (started through the mock, as New Agent does), its first message sent
    /// from the chat's composer, then the reply.
    @MainActor func testAgentFirstMessage() throws {
        var req = URLRequest(url: URL(string: hub + "/api/v1/sessions")!)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["machine_id": "macbook", "cwd": "/home/me/code/first", "async": true, "start_seconds": 0])
        var started: Data?
        let done = expectation(description: "started")
        URLSession.shared.dataTask(with: req) { d, _, _ in started = d; done.fulfill() }.resume()
        wait(for: [done], timeout: 10)
        let session = try XCTUnwrap(((try? JSONSerialization.jsonObject(with: started ?? Data())) as? [String: Any])?["session"] as? [String: Any])
        let key = "macbook/" + (session["session_id"] as? String ?? "")
        launch(["-app.tab", "chats", "-tab", "dev", "-dev.openChat", key])
        let field = app.textViews.firstMatch.exists ? app.textViews.firstMatch : app.textFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 20), "no composer")
        sleep(8) // claude up (MOCK_START_SECONDS=6)
        field.tap()
        field.typeText(Self.text)
        app.buttons["发送"].firstMatch.tap()
        mark("agent", "start")
        let sent = follow("agent", seconds: 6)
        print("FIRST agent sent: moved \(Int(sent.moved)) step \(Int(sent.step))")
        // The sent bubble's text and the transcript's are 12 pt apart as XCUITest reports them; on the screen the message
        // stays where it is (the recordings, pixel for pixel).
        XCTAssertLessThanOrEqual(sent.moved, 12, "the first message moved after it was sent")
        XCTAssertEqual(sent.gone, 0, "the first message went off the screen")
        mark("agent", "reply")
        DispatchQueue.global().async { self.reply() }
        let replied = follow("agent-reply", seconds: 6)
        print("FIRST agent reply: moved \(Int(replied.moved)) step \(Int(replied.step))")
    }
}
