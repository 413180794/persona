import XCTest

/// ↓ 回到最新 and following the newest in the chat, case by case (docs/ios-state.md「回到最新」): each case records the chat's
/// state over time (`probe.chat-rows`: the arrow, following, held back, the sentinel, the gap to the newest end, a finger
/// on the list) and prints `FINDING` lines where it differs from what the user should see. It fails only on setup:
/// it is the measurement the state machine is designed from, not its acceptance yet.
///   Agents chat:  PORT=… MOCK_STREAM=1 MOCK_LONG_HISTORY=1 MOCK_AGENTS=1 python3 scripts/mock/mock_hub.py  (JARVIS_MOCK_HUB)
///   Jarvis chat:  PORT=… MOCK_ASSISTANT_RUN=1 (JARVIS_RUN_HUB); MOCK_ASSISTANT_RUN=alone for the one-screen chat
final class ScrollToBottomTests: XCTestCase {
    private var app: XCUIApplication!
    private static let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001" // streams with MOCK_STREAM, long history
    private static let photos = "studio/a1f3c2d4-0000-4000-8000-000000000005" // several pending cards with MOCK_AGENTS
    private static let main = "server/b2e4d6f8-0000-4000-8000-000000000001" // runs with MOCK_ASSISTANT_RUN

    private struct Rows: Decodable {
        var items: Int
        var lines: Int
        var visible: Int
        var newest: Bool
        var follows: Bool
        var frozen: Bool?
        var atBottom: Bool?
        var arrow: Bool?
        var gap: Int?
        var dragging: Bool?
        var held: Bool?
    }

    override func setUpWithError() throws {
        continueAfterFailure = true
    }

    private func hub(_ name: String) throws -> String {
        guard let h = ProcessInfo.processInfo.environment[name] else { throw XCTSkip("needs \(name)") }
        return h
    }

    private func launch(_ hub: String, chat: String, assistant: Bool, _ extra: [String] = []) {
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-probes", "YES", "-openChatsFresh", "YES"]
            + (assistant ? ["-app.tab", "assistant", "-tab", "assistant", "-openAssistant", chat]
                         : ["-app.tab", "chats", "-tab", "dev", "-dev.openChat", chat])
            + extra
        app.launchPinned()
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons["Allow"]
        if allow.waitForExistence(timeout: 6) { allow.tap() }
        let start = Date.now
        while Date.now.timeIntervalSince(start) < 20, (rows()?.visible ?? 0) == 0 { Thread.sleep(forTimeInterval: 0.2) }
        XCTAssertGreaterThan(rows()?.visible ?? 0, 0, "the chat shows")
        sleep(3)
    }

    private func rows() -> Rows? {
        app.probe("chat-rows", as: Rows.self, page: "assistant") ?? app.probe("chat-rows", as: Rows.self, page: "agents")
            ?? app.probe("chat-rows", as: Rows.self)
    }

    private var arrowButton: XCUIElement { app.buttons["回到最新"].firstMatch }

    /// The composer's field (a vertical TextField), waited for. Nil in the old layout's liquid glass, where it is not in
    /// the accessibility tree (#1006).
    private func composer() -> XCUIElement? {
        let f = app.textFields.firstMatch
        if f.waitForExistence(timeout: 8) { return f }
        let v = app.textViews.firstMatch
        return v.waitForExistence(timeout: 2) ? v : nil
    }

    /// The probe's state as one line, for the timeline.
    private func line(_ r: Rows) -> String {
        "arrow \((r.arrow ?? false) ? 1 : 0) follows \(r.follows ? 1 : 0) frozen \((r.frozen ?? false) ? 1 : 0) bottom \((r.atBottom ?? false) ? 1 : 0) "
            + "newest \(r.newest ? 1 : 0) gap \(r.gap ?? -1) drag \((r.dragging ?? false) ? 1 : 0) held \((r.held ?? false) ? 1 : 0) items \(r.items)"
    }

    /// Each change of the state for `seconds`, printed as `TRUTH <case> t: …`; the states seen, in order.
    @discardableResult
    private func record(_ label: String, seconds: Double) -> [(t: Double, r: Rows)] {
        var out: [(Double, Rows)] = []
        var last = ""
        let start = Date.now
        while Date.now.timeIntervalSince(start) < seconds {
            if let r = rows() {
                let l = line(r)
                if l != last {
                    let t = Date.now.timeIntervalSince(start)
                    print(String(format: "TRUTH %@ %.2f s: ", label, t) + l)
                    out.append((t, r))
                    last = l
                }
            }
            Thread.sleep(forTimeInterval: 0.05)
        }
        return out
    }

    private func finding(_ label: String, _ what: String) {
        print("FINDING \(label): \(what)")
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "finding \(label)"
        a.lifetime = .keepAlways
        add(a)
    }

    /// How often the arrow turned on and off in a timeline (a flash: on for less than 0.6 s).
    private func flashes(_ states: [(t: Double, r: Rows)]) -> Int {
        var n = 0
        for (i, s) in states.enumerated() where (s.r.arrow ?? false) {
            let off = states[(i + 1)...].first { !($0.r.arrow ?? false) }
            if let off, off.t - s.t < 0.6 { n += 1 }
        }
        return n
    }

    private func drag(from: CGFloat, to: CGFloat, hold: TimeInterval = 0.05, velocity: XCUIGestureVelocity = .slow) {
        let a = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: from))
        a.press(forDuration: hold, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: to)), withVelocity: velocity, thenHoldForDuration: 0)
    }

    // MARK: Cases

    /// 1. Streaming, at the newest: a finger drags the list up a little (40 pt) and lets go.
    func testA_DragUpALittleWhileStreaming() throws {
        launch(try hub("JARVIS_RUN_HUB"), chat: Self.main, assistant: true)
        record("1 before", seconds: 2)
        drag(from: 0.55, to: 0.60)
        let s = record("1 after a small drag", seconds: 10)
        let end = s.last?.r
        if end?.follows == false { finding("1", "a 40 pt drag stopped following: the stream is held back (frozen \((end?.frozen ?? false) ? 1 : 0)) until the list is exactly at the newest end again (gap \(end?.gap ?? -1))") }
        if end?.arrow == true { finding("1", "the arrow stays after a 40 pt drag (gap \(end?.gap ?? -1) pt)") }
        if flashes(s) > 0 { finding("1", "the arrow flashed \(flashes(s))×") }
    }

    /// 2. A flick down from a few screens up, gliding toward the newest end.
    func testB_FlickTowardTheNewest() throws {
        launch(try hub("JARVIS_MOCK_HUB"), chat: Self.widget, assistant: false)
        for _ in 0..<3 { drag(from: 0.3, to: 0.8, velocity: .fast); sleep(1) }
        record("2 scrolled up", seconds: 2)
        drag(from: 0.75, to: 0.25, velocity: .fast)
        let s = record("2 after the flick", seconds: 8)
        if let end = s.last?.r {
            if end.newest && !end.follows { finding("2", "the glide ended at the newest end but following did not come back") }
            if !end.newest && end.gap ?? 0 < 100 && end.arrow == true { finding("2", "the glide ended \(end.gap ?? -1) pt short of the newest end: the arrow shows, the stream is held back") }
        }
        if flashes(s) > 0 { finding("2", "the arrow flashed \(flashes(s))× during the glide") }
    }

    /// 3. A work line opened and folded at the newest end of a running turn.
    func testC_OpenAndFoldAWorkLine() throws {
        launch(try hub("JARVIS_RUN_HUB"), chat: Self.main, assistant: true)
        let work = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "正在工作")).firstMatch
        XCTAssertTrue(work.waitForExistence(timeout: 10))
        work.tap()
        let opened = record("3 opened", seconds: 5)
        if work.exists && work.isHittable { work.tap() }
        let folded = record("3 folded", seconds: 5)
        if let r = opened.last?.r, r.arrow == true { finding("3", "after opening the work line at the newest end the arrow shows (gap \(r.gap ?? -1), follows \(r.follows ? 1 : 0))") }
        if let r = folded.last?.r {
            if r.newest && !r.follows { finding("3", "folded back at the newest end, following stays off: new steps are held back with no arrow") }
            if r.arrow == true && r.newest { finding("3", "the arrow shows at the newest end") }
        }
        let flashed = flashes(opened) + flashes(folded) // two timelines, each from 0: counted apart
        if flashed > 0 { finding("3", "the arrow flashed \(flashed)×") }
    }

    /// 4. The keyboard comes up (the composer tapped) and goes down, at the newest end.
    func testD_Keyboard() throws {
        launch(try hub("JARVIS_RUN_HUB"), chat: Self.main, assistant: true)
        guard let field = composer() else { throw XCTSkip("the composer is not in the accessibility tree (#1006)") }
        field.tap()
        let up = record("4 keyboard up", seconds: 3)
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.25)).tap() // a tap on the page closes it
        let down = record("4 keyboard down", seconds: 3)
        for (name, s) in [("up", up), ("down", down)] {
            if let r = s.last?.r, !r.newest || !r.follows { finding("4", "keyboard \(name): left the newest end (gap \(r.gap ?? -1), follows \(r.follows ? 1 : 0))") }
            if flashes(s) > 0 { finding("4", "keyboard \(name): the arrow flashed") }
        }
    }

    /// 5. A message sent while reading history: back to the newest, following.
    func testE_SendWhileReadingHistory() throws {
        launch(try hub("JARVIS_MOCK_HUB"), chat: Self.widget, assistant: false)
        for _ in 0..<3 { drag(from: 0.3, to: 0.8, velocity: .fast); sleep(1) }
        guard let field = composer() else { throw XCTSkip("the composer is not in the accessibility tree (#1006)") }
        field.tap()
        field.typeText("再看一下")
        app.buttons["发送"].firstMatch.tap()
        let s = record("5 sent", seconds: 6)
        if let r = s.last?.r, !(r.newest && r.follows) { finding("5", "after sending from history the chat is not at the newest end, following (gap \(r.gap ?? -1), follows \(r.follows ? 1 : 0))") }
    }

    /// 6. Pushes insert new content while the user reads history; then ↓.
    func testF_PushesWhileReadingThenTheArrow() throws {
        launch(try hub("JARVIS_MOCK_HUB"), chat: Self.widget, assistant: false)
        for _ in 0..<2 { drag(from: 0.3, to: 0.8, velocity: .fast); sleep(1) }
        let reading = record("6 reading", seconds: 8) // MOCK_STREAM: a push every 2 s
        if reading.contains(where: { !($0.r.arrow ?? false) }) && reading.count > 1 { finding("6", "the arrow went away while reading history") }
        if reading.contains(where: { $0.r.follows }) { finding("6", "following came back on while reading history") }
        if arrowButton.exists && arrowButton.isHittable {
            arrowButton.tap()
            let s = record("6 after ↓", seconds: 4)
            if let r = s.last?.r, !(r.newest && r.follows && r.arrow != true) { finding("6", "↓ did not end at the newest end, following, arrow gone (gap \(r.gap ?? -1), follows \(r.follows ? 1 : 0), arrow \((r.arrow ?? false) ? 1 : 0))") }
        } else {
            finding("6", "no ↓ to tap while reading history")
        }
    }

    /// 7. Out of the chat and back in (the Agents list and the same chat again).
    func testG_LeaveAndComeBack() throws {
        launch(try hub("JARVIS_MOCK_HUB"), chat: Self.widget, assistant: false)
        let back = app.buttons.matching(NSPredicate(format: "label == %@", "返回")).firstMatch
        XCTAssertTrue(back.waitForExistence(timeout: 10), "返回")
        back.tap()
        sleep(2)
        let rows = app.buttons.matching(identifier: "agents.row")
        let row = (0..<min(rows.count, 12)).map { rows.element(boundBy: $0) }.first { $0.isHittable && $0.label.contains("小组件") }
            ?? (0..<min(rows.count, 6)).map { rows.element(boundBy: $0) }.first { $0.isHittable }
        guard let row else { throw XCTSkip("no agent row to open again on the list (the test's selector, not measured)") }
        row.tap()
        let s = record("7 back in", seconds: 4)
        if flashes(s) > 0 { finding("7", "the arrow flashed on entry") }
        if let r = s.last?.r, !(r.newest && r.follows) { finding("7", "back in, not at the newest end following (gap \(r.gap ?? -1))") }
    }

    /// 8. To the background and back, at the newest end of a running turn.
    func testH_BackgroundAndBack() throws {
        launch(try hub("JARVIS_RUN_HUB"), chat: Self.main, assistant: true)
        XCUIDevice.shared.press(.home)
        sleep(6)
        app.activate()
        let s = record("8 back from the background", seconds: 6)
        if flashes(s) > 0 { finding("8", "the arrow flashed on return") }
        if let r = s.last?.r, !(r.newest && r.follows) { finding("8", "back from the background, not at the newest end following (gap \(r.gap ?? -1), follows \(r.follows ? 1 : 0))") }
    }

    /// 9. A chat shorter than the screen: there is nothing to scroll, the arrow never shows.
    func testI_ShortChat() throws {
        launch(try hub("JARVIS_RUN_HUB"), chat: Self.main, assistant: true)
        drag(from: 0.4, to: 0.6)
        let s = record("9 short chat", seconds: 6)
        if s.contains(where: { $0.r.arrow ?? false }) { finding("9", "the arrow showed in a chat that fits on one screen") }
        if let r = s.last?.r, !r.follows { finding("9", "a drag in a one-screen chat stopped following: new steps are held back") }
    }

    /// 10. Several pending cards over the chat (#939): the arrow above them, following unchanged.
    func testJ_PendingCards() throws {
        launch(try hub("JARVIS_MOCK_HUB"), chat: Self.photos, assistant: false)
        let s = record("10 cards", seconds: 4)
        if let r = s.last?.r {
            if r.arrow == true { finding("10", "the arrow shows at rest with the cards up (gap \(r.gap ?? -1))") }
            if !r.follows { finding("10", "not following with the cards up") }
        }
        drag(from: 0.3, to: 0.7, velocity: .fast)
        sleep(2)
        if arrowButton.exists {
            let cards = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH %@", "pending")).allElementsBoundByIndex
            if let c = cards.first(where: { $0.frame.intersects(arrowButton.frame) }) { finding("10", "the arrow is under a pending card (\(c.identifier))") }
            arrowButton.tap()
            let after = record("10 after ↓", seconds: 3)
            if let r = after.last?.r, !(r.newest && r.follows) { finding("10", "↓ with the cards up did not reach the newest end (gap \(r.gap ?? -1))") }
        }
    }
}
