import XCTest

/// Where the context was compacted, the chat draws one divider (「以上内容已压缩 · 来源 · 压缩前 N 万 token」, as the web and Android, #1762), not the two notices and
/// the idle note in a work line of their own (2026-11-04, the user's screenshot); a tap shows the notes.
///   PORT=18797 MOCK_COMPACT=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:PersonaUITests/CompactDividerTests
final class CompactDividerTests: XCTestCase {
    private var hub = ""
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_COMPACT=1 (PERSONA_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = false
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            guard alert.buttons["Allow"].exists else { return false }
            alert.buttons["Allow"].tap()
            return true
        }
    }

    private func launch(_ args: [String]) {
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openChatsFresh", "YES"] + args
        app.launchPinned()
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons["Allow"]
        if allow.waitForExistence(timeout: 6) { allow.tap() }
    }

    private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        let suffix = ProcessInfo.processInfo.environment["SHOT_SUFFIX"].map { "-" + $0 } ?? ""
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-compact-\(name)\(suffix).png"))
    }

    private var dividers: XCUIElementQuery { app.buttons.matching(identifier: "compact-divider") }

    /// The notes show only once the divider is tapped.
    private func checkNotes(_ divider: XCUIElement, _ name: String, last: Bool = false) {
        let summary = app.staticTexts["已从压缩的上下文继续"]
        XCTAssertFalse(summary.exists, "the summary notice is not a row of its own")
        shot(name)
        divider.tap()
        XCTAssertTrue(summary.waitForExistence(timeout: 3), "a tap shows the notes")
        // All of them in view, the last one too: opened as the chat's last row, they grew past the composer (2026-11-04).
        let size = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "压缩于")).firstMatch
        XCTAssertTrue(size.waitForExistence(timeout: 3), "when it was compacted, the last note")
        sleep(1)
        shot(name + "-open")
        XCTAssertTrue(size.isHittable, "the last note is in view")
        XCTAssertTrue(divider.isHittable, "the divider stays in view")
        // The chat's last row: brought all the way to the newest end, no ↓ over the notes.
        if last { let down = app.buttons["回到最新"].firstMatch; XCTAssertFalse(down.exists && down.isHittable, "no ↓ at the newest end") }
        divider.tap()
    }

    /// Persona's main conversation, compacted while idle after its last answer.
    func testPersonaIdleCompaction() {
        launch(["-app.tab", "assistant", "-tab", "assistant", "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"])
        // It opens at the start of the latest answer (finished, not seen): down to the newest, where the divider is.
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "今天的日程")).firstMatch.waitForExistence(timeout: 20))
        let down = app.buttons["回到最新"].firstMatch
        if down.waitForExistence(timeout: 3) { down.tap() } else { app.swipeUp(velocity: .slow) }
        sleep(1)
        let d = dividers.firstMatch
        if !d.waitForExistence(timeout: 8) { print("TREE " + app.debugDescription.replacingOccurrences(of: "\n", with: "\nTREE ")); shot("persona-missing") }
        XCTAssertTrue(d.exists, "the divider")
        XCTAssertTrue(d.label.hasPrefix("以上内容已压缩 · 空闲时 · 压缩前 15.1 万 token"), d.label)
        XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "后台通知")).firstMatch.exists, "no work line of notices")
        checkNotes(d, "persona", last: true)
    }

    /// An Agents chat: /compact, then a compaction in the middle of a later turn's work.
    func testAgentsManualAndMidTurn() {
        launch(["-app.tab", "chats", "-tab", "dev", "-dev.openChat", "macbook/a1f3c2d4-0000-4000-8000-000000000006"])
        let manual = dividers.matching(NSPredicate(format: "label BEGINSWITH %@", "以上内容已压缩 · 手动")).firstMatch
        XCTAssertTrue(manual.waitForExistence(timeout: 20), "the /compact divider")
        checkNotes(manual, "agents")
        // The later turn's work line, opened: its compaction is one divider among the steps.
        let work = app.buttons.matching(identifier: "work-head").allElementsBoundByIndex.last { $0.isHittable }
        XCTAssertNotNil(work, "the later turn's work line")
        if work?.label == "展开过程" { work?.tap() }
        let mid = dividers.matching(NSPredicate(format: "label BEGINSWITH %@", "以上内容已压缩 · 自动")).firstMatch
        XCTAssertTrue(mid.waitForExistence(timeout: 5), "the compaction in the work line")
        shot("agents-midturn")
    }
}
