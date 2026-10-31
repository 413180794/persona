import XCTest

/// What the user is in the middle of survives the hub's live updates (2026-10-31, the user: the Agents ⋯ menu jumped
/// while choosing a machine, a screenshot being looked at in a chat's work closed itself). The mock hub pushes a busy
/// hub's updates on demand (GET /mock/churn: sessions with new lines and a moving order, the machine list, a new step
/// in each open conversation); the app counts view bodies and main-thread time per push (`-perfCounts YES`).
///   PORT=18797 MOCK_AGENTS=1 MOCK_CHURN=1 python3 scripts/mock_hub.py &
///   TEST_RUNNER_JARVIS_CHURN_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:JarvisUITests/LiveUpdateTests
final class LiveUpdateTests: XCTestCase {
    private var hub = ""
    private var app: XCUIApplication!
    private static let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001"
    private static let rounds = 12

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let h = ProcessInfo.processInfo.environment["JARVIS_CHURN_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py with MOCK_AGENTS=1 MOCK_CHURN=1 (JARVIS_CHURN_HUB)")
        }
        hub = h
        _ = mock("reset")
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let a = XCTAttachment(screenshot: app.screenshot())
            a.name = "failure"
            a.lifetime = .keepAlways
            add(a)
        }
        super.tearDown()
    }

    /// `chat`: opened as the chat left open last time. A cold start restores it only when the tab saved with it is
    /// Agents (`app.tab`), so that is given too: otherwise the tab the last run ended on (another test's) decided it.
    private func launch(chat: String = "") {
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "chats", "-dev.openChat", chat, "-tab", "dev", "-openAgentsFresh", "YES", "-openChatsFresh", "YES", "-perfCounts", "YES"]
        app.launch()
    }

    @discardableResult
    private func mock(_ what: String) -> [String: Any] {
        let done = expectation(description: what)
        var out: [String: Any] = [:]
        URLSession.shared.dataTask(with: URL(string: hub + "/mock/" + what)!) { data, _, _ in
            out = (data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any]) ?? [:]
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        return out
    }

    /// Pushes `rounds` rounds of updates, 400 ms apart, and waits for the last. `steps`: new steps per round in each
    /// open conversation.
    private func churn(steps: Int = 1, final: Bool = false) {
        mock("churn?n=\(Self.rounds)&ms=400&steps=\(steps)&final=\(final ? 1 : 0)")
        let deadline = Date.now.addingTimeInterval(30)
        while (mock("churned")["rounds"] as? Int ?? 0) < Self.rounds + (final ? 1 : 0), Date.now < deadline { usleep(300_000) }
        sleep(1) // the last round applied
    }

    private struct Counts {
        var bodies: [String: Int]
        var events: Int
        var passes: Int
        var ms: Double
        var max: Double
    }

    private func counts() -> Counts {
        let probe = app.staticTexts["perf.counts"]
        XCTAssertTrue(probe.waitForExistence(timeout: 5), "perf.counts (launched with -perfCounts YES)")
        let d = (try? JSONSerialization.jsonObject(with: Data(probe.label.utf8))) as? [String: Any] ?? [:]
        return Counts(bodies: d["bodies"] as? [String: Int] ?? [:], events: d["events"] as? Int ?? 0,
                      passes: d["passes"] as? Int ?? 0, ms: d["ms"] as? Double ?? 0, max: d["max"] as? Double ?? 0)
    }

    /// Bodies and main-thread time the churn cost, per round of pushes; attached to the test and printed.
    private func report(_ name: String, from a: Counts, to b: Counts) {
        let n = Double(Self.rounds)
        var lines = [String(format: "%@: per round (%d pushes in %d main-thread passes overall) %.1f ms on the main thread, slowest pass %.1f ms",
                            name, b.events - a.events, b.passes - a.passes, (b.ms - a.ms) / n, b.max)]
        for (k, v) in b.bodies.sorted(by: { $0.key < $1.key }) where v - (a.bodies[k] ?? 0) > 0 {
            lines.append(String(format: "  %@ %.1f", k, Double(v - (a.bodies[k] ?? 0)) / n))
        }
        let text = lines.joined(separator: "\n")
        print("PERF " + text.replacingOccurrences(of: "\n", with: "\nPERF "))
        let att = XCTAttachment(string: text)
        att.name = "perf " + name
        att.lifetime = .keepAlways
        add(att)
    }

    // The ⋯ menu over the Agents list stays open where it was while updates come in: it was rebuilt on every push
    // (the machine list, the sections it folds) and jumped, or scrolled itself, under the finger choosing a machine.
    func testListMenuHoldsStillThroughUpdates() {
        launch()
        let menuButton = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "分组：")).firstMatch
        XCTAssertTrue(menuButton.waitForExistence(timeout: 15))
        sleep(2)
        menuButton.tap()
        let machines = app.buttons["全部机器"]
        XCTAssertTrue(machines.waitForExistence(timeout: 5), "the menu opened")
        // Scrolled down to the machines, as when choosing one.
        machines.swipeUp(velocity: .slow)
        sleep(1)
        let last = app.buttons["训练机"]
        XCTAssertTrue(last.exists, "the last machine")
        let lastY = last.frame.minY
        let machinesY = machines.frame.minY
        let before = counts()
        churn()
        XCTAssertTrue(last.exists, "the menu is still open")
        XCTAssertEqual(last.frame.minY, lastY, accuracy: 1, "the menu did not scroll or jump")
        XCTAssertEqual(machines.frame.minY, machinesY, accuracy: 1, "the menu did not scroll or jump")
        let after = counts()
        report("Agents list, ⋯ menu open", from: before, to: after)
        XCTAssertLessThanOrEqual((after.bodies["ListMenu.content"] ?? 0) - (before.bodies["ListMenu.content"] ?? 0), 1,
                                 "the open menu's content is not rebuilt for each push")
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.95)).tap() // close it
    }

    // A row's long-press menu stays open through updates of that very agent (it is working: a new line every push).
    func testRowMenuHoldsStillThroughUpdates() {
        launch()
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "给天气 App 加主屏幕小组件")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        sleep(2)
        row.press(forDuration: 1.2)
        let rename = app.buttons["重命名…"]
        XCTAssertTrue(rename.waitForExistence(timeout: 5), "the long-press menu opened")
        let y = rename.frame.minY
        let before = counts()
        churn()
        XCTAssertTrue(rename.exists, "the long-press menu is still open")
        XCTAssertEqual(rename.frame.minY, y, accuracy: 1, "the long-press menu did not move")
        let after = counts()
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.08)).tap()
        report("Agents list, long-press menu open", from: before, to: after)
        XCTAssertEqual(after.bodies["RowMenu.content"] ?? 0, before.bodies["RowMenu.content"] ?? 0,
                       "the open long-press menu's content was not rebuilt by the pushes")
    }

    // Scrolled into the middle of the list, what is on screen stays where it is while agents above it change order.
    func testScrolledListHoldsStillThroughUpdates() {
        launch()
        XCTAssertTrue(app.buttons["agents.filter.all"].waitForExistence(timeout: 15))
        sleep(2)
        let list = app.collectionViews.firstMatch
        list.swipeUp(velocity: .slow)
        sleep(2)
        // A row in the middle of the screen, and where it is.
        let rows = app.buttons.matching(identifier: "agents.row")
        let mid = app.frame.midY
        let row = (0..<rows.count).map { rows.element(boundBy: $0) }
            .filter { $0.isHittable }.min { abs($0.frame.midY - mid) < abs($1.frame.midY - mid) }
        guard let row else { return XCTFail("no row on screen") }
        let label = row.label
        let y = row.frame.minY
        let before = counts()
        churn()
        let same = app.buttons.matching(NSPredicate(format: "label == %@", label)).firstMatch
        XCTAssertTrue(same.exists, "the row is still there")
        XCTAssertEqual(same.frame.minY, y, accuracy: 2, "the row under the reader stayed put")
        report("Agents list, scrolled to the middle", from: before, to: counts())
    }

    // A screenshot in a working agent's steps, open full screen, stays open while the agent goes on (new steps every
    // push, a word between them) and finishes: it was presented by its row, which the list rebuilds as the turn goes.
    func testImageViewerStaysOpenThroughUpdates() {
        launch(chat: Self.widget)
        let head = app.buttons["work-head"].firstMatch
        XCTAssertTrue(head.waitForExistence(timeout: 15))
        if head.label == "展开过程" { head.tap() }
        // The screenshot is the result of a tool call: open that step.
        let step = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "截一张小组件的图")).firstMatch
        XCTAssertTrue(step.waitForExistence(timeout: 5), "the screenshot's step")
        step.tap()
        let image = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "图片，点按查看大图")).firstMatch
        XCTAssertTrue(image.waitForExistence(timeout: 5), "the screenshot in the work")
        image.tap()
        let save = app.buttons["保存"]
        XCTAssertTrue(save.waitForExistence(timeout: 5), "the viewer opened")
        let before = counts()
        // Many steps a round (a busy turn moves the list far past the screenshot's row), then the turn ends: its
        // last reply comes and the work line is laid out again.
        churn(steps: 6, final: true)
        XCTAssertTrue(save.exists, "the viewer is still open")
        app.buttons["关闭"].tap()
        sleep(1)
        report("Chat, image viewer open", from: before, to: counts())
    }

    // The open work card of a working agent stays open while steps and words come in.
    func testOpenWorkStaysOpenThroughUpdates() {
        launch(chat: Self.widget)
        let head = app.buttons["work-head"].firstMatch
        XCTAssertTrue(head.waitForExistence(timeout: 15))
        if head.label == "展开过程" { head.tap() }
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label == %@", "收起过程")).firstMatch.waitForExistence(timeout: 5))
        let before = counts()
        churn()
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label == %@", "收起过程")).firstMatch.exists, "the work card is still open")
        report("Chat, work card open", from: before, to: counts())
    }

    // Typing into the search field: the text and the keyboard stay while the list behind it changes.
    func testSearchKeepsWhatIsTypedThroughUpdates() {
        launch()
        let field = app.searchFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 15))
        field.tap()
        field.typeText("platform")
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5))
        let before = counts()
        churn()
        XCTAssertEqual(field.value as? String, "platform", "what was typed is still there")
        XCTAssertTrue(app.keyboards.firstMatch.exists, "the keyboard is still up")
        report("Agents list, searching", from: before, to: counts())
    }

    // A row swiped open (关闭 showing) stays open while its agent keeps working.
    func testSwipedRowStaysOpenThroughUpdates() {
        launch()
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "给天气 App 加主屏幕小组件")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        sleep(2)
        row.swipeLeft()
        let close = app.buttons["关闭"]
        XCTAssertTrue(close.waitForExistence(timeout: 5), "the row's swipe actions")
        churn()
        XCTAssertTrue(close.exists, "the swipe actions are still open")
    }
}
