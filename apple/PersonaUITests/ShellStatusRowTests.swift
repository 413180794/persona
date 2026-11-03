import XCTest

/// The shell's status row (docs/shell.md「状态行」): 等待网络 / 连接中… / 更新中…, in one row under the status bar that
/// takes its room. Floating, the link's pill lay over Agents' filter chips and search field (2026-11-02). Here: nothing
/// it shows meets a control (the filter chips, the search field, the composer, the bar's buttons); the page makes way as
/// one piece (every control and row moves by the row's room, the rows keep their order) and comes back; and the row
/// waits for a finger to let go, unless what it shows is urgent. The toasts float instead (`ShellToastTests`, #1053).
final class ShellStatusRowTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = true
    }

    @MainActor private func launch(_ page: String, _ extra: [String]) {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "NO", "-probes", "YES", "-tab", page, "-dev.openChat", "", "-dev.folded", ""]
            + (page == "assistant" ? ["-newAssistant", "YES"] : []) + extra
        app.launchPinned()
        XCTAssertTrue(app.descendants(matching: .any)["probe.shell"].waitForExistence(timeout: 15), "no shell")
    }

    @MainActor private func attachScreen(_ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }

    /// Waits until the row shows `status` (none / link).
    @MainActor @discardableResult private func expectRow(_ status: String, within: TimeInterval, _ what: String) -> Bool {
        let start = Date()
        while app.shellValue("status") != status, Date().timeIntervalSince(start) < within { usleep(100_000) }
        let ok = app.shellValue("status") == status
        if !ok { attachScreen(what); XCTFail("the row is not \(status) within \(within) s: \(what) \(app.shellTrace)") }
        return ok
    }

    /// The controls a finger uses at the top of `page`, by name.
    @MainActor private func controls(_ page: String) -> [(String, XCUIElement)] {
        let any = app.descendants(matching: .any)
        var c: [(String, XCUIElement)] = [("face", app.buttons[page == "dev" ? "work-sidebar-button" : "assistant-sidebar-button"].firstMatch),
                                          ("助手", app.buttons["shell-assistant"].firstMatch), ("Agents", app.buttons["shell-agents"].firstMatch)]
        if page == "dev" {
            c += [("新建会话", app.buttons["agents-new"].firstMatch), ("全部", app.buttons["agents.filter.all"].firstMatch),
                  ("search", app.searchFields.firstMatch)]
        } else {
            c += [("新对话", app.buttons["assistant-new"].firstMatch), ("composer", any["shell-composer-field"].firstMatch)]
        }
        return c
    }

    @MainActor private func frames(_ page: String) -> [String: CGRect] {
        Dictionary(uniqueKeysWithValues: controls(page).compactMap { name, e in e.exists && !e.frame.isEmpty ? (name, e.frame) : nil })
    }

    /// The Agents rows on screen, top to bottom: their titles and where they are.
    /// The first four (reading every row's place takes seconds, and the link's state comes and goes meanwhile).
    @MainActor private func rows() -> [(String, CGFloat)] {
        let screen = app.windows.firstMatch.frame
        let all = app.descendants(matching: .any).matching(identifier: "agents.row")
        return (0..<4).map { all.element(boundBy: $0) }
            .filter { $0.exists && screen.intersects($0.frame) }
            .map { (Self.stable($0.label), $0.frame.minY) }
            .sorted { $0.1 < $1.1 }
    }

    /// A row's label without its relative time: 「19 分钟前」 turns 「20 分钟前」 between two reads a minute apart.
    private static func stable(_ label: String) -> String {
        label.components(separatedBy: "、").filter { $0.range(of: #"^(\d+ ?(秒|分钟|小时|天)前|刚刚)$"#, options: .regularExpression) == nil }
            .joined(separator: "、")
    }

    /// The link's states come and go (`-showConnectionCycle`): while one shows, it meets no control, and the page is
    /// lower by the row's room as one piece; once it goes, everything is back where it was.
    @MainActor func testTheLinkTakesItsRoomInEveryState() {
        for page in ["dev", "assistant"] {
            for phase in ["waiting", "connecting", "updating"] {
                let what = "\(phase) on \(page)"
                // Fine for 15 s, then the state for 6 s, again and again: the page is measured while it is fine (once
                // the search is pulled out; reading every row's place takes a few seconds), then the next time the state
                // comes. A measure the state came in the middle of is taken again.
                launch(page, ["-showConnectionCycle", "\(phase):15:6"])
                if page == "dev" { app.revealAgentsSearch() }
                var before: [String: CGRect] = [:], rowsBefore: [(String, CGFloat)] = []
                for _ in 0..<3 {
                    guard expectRow("none", within: 10, "\(what), fine first") else { break }
                    sleep(1)
                    let opens = app.shellValue("opened")
                    before = frames(page)
                    rowsBefore = page == "dev" ? rows() : []
                    if app.shellValue("status") == "none", app.shellValue("opened") == opens { break } // measured while fine
                }
                guard expectRow("link", within: 45, what) else { app.terminate(); continue }
                let opened = Int(app.shellValue("opened") ?? "0") ?? 0, foldedBefore = Int(app.shellValue("folded") ?? "0") ?? 0
                sleep(1) // the room's animation
                let room = app.shellRoom
                XCTAssertGreaterThan(room, 10, "the row takes no room (\(what))")
                let pill = app.descendants(matching: .any)["shell-connection"].firstMatch
                XCTAssertTrue(pill.exists, "no link pill (\(what))")
                let during = frames(page)
                for (name, f) in during {
                    XCTAssertFalse(f.intersects(pill.frame), "\(name) \(f) under the link's pill \(pill.frame) (\(what))")
                    // What hangs from the top moves with it; the composer stays on the keyboard's edge.
                    if let b = before[name], name != "composer" {
                        XCTAssertEqual(f.minY - b.minY, room, accuracy: 1, "\(name) moved \(f.minY - b.minY), not the row's \(room) (\(what))")
                    }
                }
                if page == "dev" {
                    let rowsDuring = rows()
                    XCTAssertEqual(rowsDuring.map(\.0), rowsBefore.map(\.0).prefix(rowsDuring.count).map { $0 },
                                   "the rows on screen changed or reordered as the row opened (\(what))")
                    for (b, d) in zip(rowsBefore, rowsDuring) where b.0 == d.0 {
                        XCTAssertEqual(d.1 - b.1, room, accuracy: 1, "row \(b.0) moved \(d.1 - b.1), not \(room) (\(what))")
                    }
                }
                attachScreen(what)
                guard expectRow("none", within: 8, "\(what), back") else { app.terminate(); continue }
                sleep(1)
                for (name, f) in frames(page) {
                    if let b = before[name] { XCTAssertEqual(f.minY, b.minY, accuracy: 1, "\(name) not back after \(what)") }
                }
                XCTAssertEqual(app.shellValue("opened"), "\(opened)", "the row opened again (\(what))")
                XCTAssertEqual(app.shellValue("folded"), "\(foldedBefore + 1)", "the row folded more than once (\(what))")
                app.terminate()
            }
        }
    }

    /// While a finger holds the screen the row does not open for 连接中… (everything under the finger would move); it
    /// opens once the finger lets go. 等待网络 opens at once.
    @MainActor func testTheRowWaitsForTheFingerUnlessUrgent() {
        for (phase, urgent) in [("connecting", false), ("waiting", true)] {
            // Fine 10 s, the state 4 s, again: once the first one is over, a press from then until 1.5 s into the next
            // one (14 s on), letting go well within the row's longest wait (3 s).
            launch("assistant", ["-showConnectionCycle", "\(phase):10:4"])
            guard expectRow("link", within: 16, "\(phase) the first time"), expectRow("none", within: 6, "\(phase) over") else {
                app.terminate(); continue
            }
            app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.45)).press(forDuration: 11.5)
            expectRow("link", within: 6, "\(phase) after the press")
            let trace = app.shellTrace
            let events = trace.components(separatedBy: " | ")
            guard let rowAt = events.lastIndex(where: { $0.hasSuffix("row link") || $0.hasSuffix("row link late") }) else {
                XCTFail("no row event: \(trace)"); app.terminate(); continue
            }
            let fingers = events[..<rowAt].last { $0.hasSuffix("fingers busy") || $0.hasSuffix("fingers idle") }
            XCTAssertFalse(events[rowAt].hasSuffix("late"), "the row opened on the longest wait, not when the finger let go: \(trace)")
            if urgent {
                XCTAssertTrue(fingers?.hasSuffix("busy") == true, "等待网络 waited for the finger: \(trace)")
            } else {
                XCTAssertTrue(fingers?.hasSuffix("idle") == true, "连接中… opened under the finger: \(trace)")
            }
            app.terminate()
        }
    }
}
